package main

import (
	"bytes"
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	agentruntime "teak/internal/agent/runtime"
	"teak/internal/codemap"
	"teak/internal/config"
	"teak/internal/dap"
	"teak/internal/git"
	"teak/internal/lsp"
	"teak/internal/procmon"
	"teak/internal/search"
	"teak/internal/session"
	"teak/internal/text"
	"teak/internal/toolpath"
)

const (
	headlessMaxBufferBytes      = 4 << 20
	headlessMaxContextEntries   = 2_000
	headlessMaxContextDepth     = 4
	headlessMaxContextDirs      = 512
	headlessMaxCommandOutput    = 1 << 20
	headlessMaxCommandArgsBytes = 64 << 10
	headlessCommandTimeout      = 2 * time.Minute
	headlessSchemaVersion       = 1
)

const headlessUsageText = `Usage:
	teak headless context [--json] [--depth <0-4>] [--root <directory>]
	teak headless project list|stat [--json] [--depth <0-4>] [--root <directory>] [path]
	teak headless project mkdir|rename|copy|remove --confirm [--json] [--root <directory>] <path> [destination]
	teak headless buffer read [--json] [--root <directory>] <path>
	teak headless buffer write [--json] [--root <directory>] --expected-sha256 <sha256> <path>
	teak headless search [--semantic] [--index] [--json] [--root <directory>] [--regex] [--case-sensitive] <query>
	teak headless exec --confirm [--json] [--root <directory>] -- <command> [args...]
	teak headless codemap context|callers|callees|find [--json] [--root <directory>] <symbol>
	teak headless codemap impact [--json] [--depth <0-10>] [--root <directory>] <symbol>
	teak headless codemap symbols [--json] [--root <directory>] <path>
	teak headless codemap symbol-at --line <0-based> [--json] [--root <directory>] <path>
	teak headless tools status [--json] [--root <directory>]
	teak headless health [--json] [--root <directory>]
	teak headless health dashboard [--limit <1-256>] [--json] [--root <directory>]
	teak headless health record --confirm [--json] [--root <directory>]
	teak headless health history [--limit <1-256>] [--json] [--root <directory>]
	teak headless serve --listen <loopback-address> --token <token> [--json] [--root <directory>] [--workspace <name>=<directory> ...]
	teak headless mcp [--root <directory>]
	teak headless hitspec validate [--json] [--root <directory>] <file|directory>
	teak headless git status [--json] [--root <directory>]
  teak headless session show [--json] [--root <directory>] [--name <name>]
	teak headless session list [--json] [--root <directory>]
	teak headless session save [--json] [--root <directory>] <name>
	teak headless session activate [--json] [--root <directory>] <name>
	teak headless session health [--json] [--root <directory>] [--name <name>]
	teak headless session cleanup --confirm [--json] [--root <directory>] [--name <name>]
	  teak headless lsp status [--probe] [--json] [--root <directory>]
	  teak headless lsp diagnostics [--json] [--root <directory>] <path>
	  teak headless lsp format [--json] [--apply --expected-sha256 <sha256>] [--root <directory>] <path>
	  teak headless lsp symbols [--json] [--root <directory>] <path>
	  teak headless lsp hover|definition|references [--line <0-based>] [--column <0-based>] [--json] [--root <directory>] <path>
	  teak headless dap status [--json] [--root <directory>]
	  teak headless dap probe [--json] [--root <directory>] [--adapter <command>] -- [args...]
	teak headless agent list [--json] [--root <directory>]
	  teak headless agent show [--json] [--root <directory>] <run-id>
	  teak headless agent cancel --confirm [--json] [--root <directory>] <run-id>
	  teak headless agent reap-stale --confirm [--json] [--root <directory>] --max-silence <duration>

The headless interface is safe to use from Glyphrun, scripts, and agents
without attaching to a terminal. Buffer writes require an expected SHA-256.
`

type headlessOptions struct {
	json           bool
	apply          bool
	confirm        bool
	root           string
	name           string
	maxSilence     string
	regex          bool
	caseSensitive  bool
	semantic       bool
	index          bool
	probe          bool
	line           int
	lineSet        bool
	column         int
	columnSet      bool
	expectedSHA256 string
	listen         string
	token          string
	workspaces     []string
	depth          int
	depthSet       bool
}

type headlessErrorResponse struct {
	State   string `json:"state"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type headlessErrorWriter struct {
	io.Writer
	json bool
}

type headlessContextResponse struct {
	Workspace   string                 `json:"workspace"`
	ProjectRoot string                 `json:"project_root,omitempty"`
	Entries     []headlessContextEntry `json:"entries"`
	Truncated   bool                   `json:"truncated"`
}

type headlessContextEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Bytes int64  `json:"bytes,omitempty"`
}

type headlessBufferResponse struct {
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	Bytes        int    `json:"bytes"`
	Lines        int    `json:"lines"`
	SHA256       string `json:"sha256"`
	Content      string `json:"content"`
}

type headlessSearchResponse struct {
	Workspace  string                 `json:"workspace"`
	Query      string                 `json:"query"`
	Mode       string                 `json:"mode"`
	State      string                 `json:"state"`
	Indexed    bool                   `json:"indexed"`
	Truncated  bool                   `json:"truncated"`
	Files      int                    `json:"files,omitempty"`
	Pending    int                    `json:"pending_changes,omitempty"`
	Detail     string                 `json:"detail,omitempty"`
	Hint       string                 `json:"hint,omitempty"`
	Results    []headlessSearchResult `json:"results"`
	DurationMS float64                `json:"duration_ms"`
}

type headlessSearchResult struct {
	FilePath   string  `json:"file_path"`
	Line       int     `json:"line"`
	Column     int     `json:"column"`
	Preview    string  `json:"preview"`
	Score      float64 `json:"score,omitempty"`
	SymbolName string  `json:"symbol_name,omitempty"`
	ChunkType  string  `json:"chunk_type,omitempty"`
	EndLine    int     `json:"end_line,omitempty"`
}

type headlessExecResponse struct {
	Workspace string   `json:"workspace"`
	Command   string   `json:"command"`
	Args      []string `json:"args,omitempty"`
	State     string   `json:"state"`
	ExitCode  int      `json:"exit_code"`
	Stdout    string   `json:"stdout,omitempty"`
	Stderr    string   `json:"stderr,omitempty"`
	Truncated bool     `json:"truncated"`
	Detail    string   `json:"detail,omitempty"`
}

type headlessToolsResponse struct {
	Workspace string                 `json:"workspace"`
	Tools     []headlessToolStatus   `json:"tools"`
	Metrics   headlessRuntimeMetrics `json:"metrics"`
}

type headlessSessionResponse struct {
	Workspace string         `json:"workspace"`
	Path      string         `json:"path"`
	Name      string         `json:"name,omitempty"`
	State     string         `json:"state"`
	Session   *session.State `json:"session,omitempty"`
	Detail    string         `json:"detail,omitempty"`
}

type headlessSessionListResponse struct {
	Workspace string   `json:"workspace"`
	Names     []string `json:"names"`
}

type headlessSessionHealthResponse struct {
	Workspace string                `json:"workspace"`
	State     string                `json:"state"`
	Sessions  []session.NamedHealth `json:"sessions"`
}

type headlessSessionCleanupResponse struct {
	Workspace string                `json:"workspace"`
	State     string                `json:"state"`
	Removed   []string              `json:"removed"`
	Skipped   []string              `json:"skipped"`
	Sessions  []session.NamedHealth `json:"sessions,omitempty"`
}

type headlessLSPResponse struct {
	Workspace    string              `json:"workspace"`
	Servers      []headlessLSPEntry  `json:"servers"`
	LanguageScan *doctorLanguageScan `json:"language_scan,omitempty"`
}

type headlessLSPDiagnosticsResponse struct {
	Workspace    string               `json:"workspace"`
	Path         string               `json:"path"`
	RelativePath string               `json:"relative_path"`
	LanguageID   string               `json:"language_id,omitempty"`
	Server       string               `json:"server,omitempty"`
	State        string               `json:"state"`
	Diagnostics  []headlessDiagnostic `json:"diagnostics"`
	Detail       string               `json:"detail,omitempty"`
	Hint         string               `json:"hint,omitempty"`
}

type headlessDiagnostic struct {
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"end_line"`
	EndColumn int    `json:"end_column"`
	Severity  int    `json:"severity"`
	Message   string `json:"message"`
	Source    string `json:"source,omitempty"`
}

type headlessLSPFormatResponse struct {
	Workspace    string `json:"workspace"`
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	LanguageID   string `json:"language_id,omitempty"`
	Server       string `json:"server,omitempty"`
	State        string `json:"state"`
	Changed      bool   `json:"changed"`
	Applied      bool   `json:"applied"`
	Edits        int    `json:"edits"`
	InputSHA256  string `json:"input_sha256"`
	OutputSHA256 string `json:"output_sha256"`
	Content      string `json:"content,omitempty"`
	Detail       string `json:"detail,omitempty"`
	Hint         string `json:"hint,omitempty"`
}

type headlessLSPEntry struct {
	LanguageID      string   `json:"language_id"`
	Extensions      []string `json:"extensions"`
	Command         string   `json:"command"`
	DetectedFiles   int      `json:"detected_files,omitempty"`
	Available       bool     `json:"available"`
	Ready           bool     `json:"ready"`
	State           string   `json:"state"`
	Path            string   `json:"path,omitempty"`
	Version         string   `json:"version,omitempty"`
	VersionProbe    string   `json:"version_probe,omitempty"`
	ProtocolProbe   string   `json:"protocol_probe,omitempty"`
	CapabilityProbe string   `json:"capability_probe,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	Hint            string   `json:"hint,omitempty"`
}

type headlessDAPResponse struct {
	Workspace string             `json:"workspace"`
	Adapters  []headlessDAPEntry `json:"adapters"`
}

type headlessDAPEntry struct {
	Type         string   `json:"type"`
	Extensions   []string `json:"extensions"`
	Command      string   `json:"command"`
	Available    bool     `json:"available"`
	State        string   `json:"state"`
	Path         string   `json:"path,omitempty"`
	Version      string   `json:"version,omitempty"`
	VersionProbe string   `json:"version_probe,omitempty"`
	Hint         string   `json:"hint,omitempty"`
}

type headlessDAPProbeResponse struct {
	Workspace string   `json:"workspace"`
	Adapter   string   `json:"adapter"`
	Args      []string `json:"args,omitempty"`
	State     string   `json:"state"`
	Ready     bool     `json:"ready"`
	Detail    string   `json:"detail,omitempty"`
	Hint      string   `json:"hint,omitempty"`
}

type headlessHitspecValidationResponse struct {
	Workspace    string                  `json:"workspace"`
	Path         string                  `json:"path"`
	RelativePath string                  `json:"relative_path"`
	State        string                  `json:"state"`
	Valid        bool                    `json:"valid"`
	Files        int                     `json:"files"`
	Results      []headlessHitspecResult `json:"results"`
	DurationMS   float64                 `json:"duration_ms"`
	Detail       string                  `json:"detail,omitempty"`
	Hint         string                  `json:"hint,omitempty"`
}

type headlessHitspecResult struct {
	File   string   `json:"file"`
	OK     bool     `json:"ok"`
	Errors []string `json:"errors,omitempty"`
}

type headlessAgentResponse struct {
	Workspace string                   `json:"workspace"`
	Path      string                   `json:"path"`
	Runs      []agentruntime.RunRecord `json:"runs"`
}

type headlessAgentRunResponse struct {
	Workspace string                 `json:"workspace"`
	Path      string                 `json:"path"`
	Run       agentruntime.RunRecord `json:"run"`
}

type headlessAgentReapResponse struct {
	Workspace  string               `json:"workspace"`
	Path       string               `json:"path"`
	State      string               `json:"state"`
	MaxSilence string               `json:"max_silence"`
	Reaped     []agentruntime.RunID `json:"reaped"`
}

type headlessAgentCancelResponse struct {
	Workspace string             `json:"workspace"`
	Path      string             `json:"path"`
	State     string             `json:"state"`
	RunID     agentruntime.RunID `json:"run_id"`
}

type headlessToolStatus struct {
	Name string `json:"name"`
	// Mode identifies the bounded probe used for health. It lets clients
	// distinguish a vector-free readiness check from an explicit indexing or
	// full-statistics operation without parsing Detail text.
	Mode            string  `json:"mode,omitempty"`
	State           string  `json:"state"`
	Available       bool    `json:"available"`
	Ready           bool    `json:"ready"`
	Path            string  `json:"path,omitempty"`
	Version         string  `json:"version,omitempty"`
	VersionProbe    string  `json:"version_probe,omitempty"`
	Capability      string  `json:"capability,omitempty"`
	CapabilityProbe string  `json:"capability_probe,omitempty"`
	Detail          string  `json:"detail,omitempty"`
	Hint            string  `json:"hint,omitempty"`
	Files           int     `json:"files,omitempty"`
	Nodes           int     `json:"nodes,omitempty"`
	Edges           int     `json:"edges,omitempty"`
	Records         int     `json:"records,omitempty"`
	PendingChanges  int     `json:"pending_changes,omitempty"`
	DurationMS      float64 `json:"duration_ms,omitempty"`
}

const maxHeadlessCodemapResults = 1_000

type headlessCodemapResponse struct {
	Workspace  string                 `json:"workspace"`
	Operation  string                 `json:"operation"`
	Symbol     string                 `json:"symbol,omitempty"`
	Path       string                 `json:"path,omitempty"`
	Line       *int                   `json:"line,omitempty"`
	State      string                 `json:"state"`
	Context    *codemap.ContextResult `json:"context,omitempty"`
	Impact     *codemap.ImpactResult  `json:"impact,omitempty"`
	Results    []codemap.Symbol       `json:"results,omitempty"`
	Truncated  bool                   `json:"truncated"`
	DurationMS float64                `json:"duration_ms"`
	Detail     string                 `json:"detail,omitempty"`
}

type headlessRuntimeMetrics struct {
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	HeapSysBytes   uint64 `json:"heap_sys_bytes"`
	RSSBytes       uint64 `json:"rss_bytes,omitempty"`
	RSSAvailable   bool   `json:"rss_available"`
}

type headlessGitResponse struct {
	Workspace string             `json:"workspace"`
	State     string             `json:"state"`
	Branch    string             `json:"branch,omitempty"`
	Entries   []headlessGitEntry `json:"entries"`
	Detail    string             `json:"detail,omitempty"`
}

type headlessGitEntry struct {
	Path         string `json:"path"`
	OriginalPath string `json:"original_path,omitempty"`
	IndexStatus  string `json:"index_status"`
	WorkStatus   string `json:"work_status"`
	Staged       bool   `json:"staged"`
	Unstaged     bool   `json:"unstaged"`
	Untracked    bool   `json:"untracked"`
}

func isHeadlessCommand(args []string) bool {
	return len(args) > 0 && args[0] == "headless"
}

func parseHeadlessArgs(args []string) (headlessOptions, []string, bool, error) {
	var opts headlessOptions
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			opts.json = true
		case "--apply":
			opts.apply = true
		case "--confirm":
			opts.confirm = true
		case "--regex":
			opts.regex = true
		case "--case-sensitive":
			opts.caseSensitive = true
		case "--semantic":
			opts.semantic = true
		case "--index":
			opts.index = true
		case "--probe":
			opts.probe = true
		case "--line":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return headlessOptions{}, nil, false, fmt.Errorf("--line requires a value")
			}
			line, parseErr := strconv.Atoi(strings.TrimSpace(args[i+1]))
			if parseErr != nil || line < 0 {
				if parseErr == nil {
					parseErr = errors.New("line must be non-negative")
				}
				return headlessOptions{}, nil, false, fmt.Errorf("--line must be a non-negative integer: %w", parseErr)
			}
			opts.line = line
			opts.lineSet = true
			i++
		case "--column":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return headlessOptions{}, nil, false, fmt.Errorf("--column requires a value")
			}
			column, parseErr := strconv.Atoi(strings.TrimSpace(args[i+1]))
			if parseErr != nil || column < 0 {
				if parseErr == nil {
					parseErr = errors.New("column must be non-negative")
				}
				return headlessOptions{}, nil, false, fmt.Errorf("--column must be a non-negative integer: %w", parseErr)
			}
			opts.column = column
			opts.columnSet = true
			i++
		case "--expected-sha256":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return headlessOptions{}, nil, false, fmt.Errorf("--expected-sha256 requires a value")
			}
			opts.expectedSHA256 = strings.TrimSpace(args[i+1])
			i++
		case "--root":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return headlessOptions{}, nil, false, fmt.Errorf("--root requires a directory")
			}
			opts.root = args[i+1]
			i++
		case "--listen":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return headlessOptions{}, nil, false, fmt.Errorf("--listen requires an address")
			}
			opts.listen = strings.TrimSpace(args[i+1])
			i++
		case "--token":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return headlessOptions{}, nil, false, fmt.Errorf("--token requires a value")
			}
			opts.token = strings.TrimSpace(args[i+1])
			i++
		case "--workspace":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return headlessOptions{}, nil, false, fmt.Errorf("--workspace requires <name>=<directory>")
			}
			opts.workspaces = append(opts.workspaces, strings.TrimSpace(args[i+1]))
			i++
		case "--name":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return headlessOptions{}, nil, false, fmt.Errorf("--name requires a value")
			}
			opts.name = strings.TrimSpace(args[i+1])
			i++
		case "--max-silence":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return headlessOptions{}, nil, false, fmt.Errorf("--max-silence requires a duration")
			}
			opts.maxSilence = strings.TrimSpace(args[i+1])
			i++
		case "--depth":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return headlessOptions{}, nil, false, fmt.Errorf("--depth requires a value")
			}
			depth, parseErr := strconv.Atoi(strings.TrimSpace(args[i+1]))
			if parseErr != nil {
				return headlessOptions{}, nil, false, fmt.Errorf("--depth must be an integer: %w", parseErr)
			}
			opts.depth = depth
			opts.depthSet = true
			i++
		case "-h", "--help":
			return headlessOptions{}, nil, true, nil
		default:
			if strings.HasPrefix(args[i], "-") {
				return headlessOptions{}, nil, false, fmt.Errorf("unknown headless option %q", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	return opts, positional, false, nil
}

func runHeadlessCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runHeadlessCLIContext(context.Background(), args, stdin, stdout, stderr)
}

func runHeadlessCLIContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) == 0 {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}

	command := args[0]
	if command == "-h" || command == "--help" {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if headlessJSONRequested(args[1:]) {
		stderr = headlessErrorWriter{Writer: stderr, json: true}
	}

	switch command {
	case "context":
		return runHeadlessContextContext(ctx, args[1:], stdout, stderr)
	case "project":
		return runHeadlessProjectContext(ctx, args[1:], stdout, stderr)
	case "buffer":
		return runHeadlessBufferContext(ctx, args[1:], stdin, stdout, stderr)
	case "search":
		return runHeadlessSearchContext(ctx, args[1:], stdout, stderr)
	case "codemap":
		return runHeadlessCodemapContext(ctx, args[1:], stdout, stderr)
	case "exec":
		return runHeadlessExecContext(ctx, args[1:], stdout, stderr)
	case "tools":
		return runHeadlessToolsContext(ctx, args[1:], stdout, stderr)
	case "health":
		return runHeadlessHealthContext(ctx, args[1:], stdout, stderr)
	case "serve":
		return runHeadlessServe(args[1:], stdout, stderr)
	case "mcp":
		return runHeadlessMCPContext(ctx, args[1:], stdin, stdout, stderr)
	case "hitspec":
		return runHeadlessHitspecContext(ctx, args[1:], stdout, stderr)
	case "git":
		return runHeadlessGitContext(ctx, args[1:], stdout, stderr)
	case "session":
		return runHeadlessSessionContext(ctx, args[1:], stdout, stderr)
	case "lsp":
		return runHeadlessLSPContext(ctx, args[1:], stdout, stderr)
	case "dap":
		return runHeadlessDAPContext(ctx, args[1:], stdout, stderr)
	case "agent":
		return runHeadlessAgentContext(ctx, args[1:], stdout, stderr)
	default:
		return writeHeadlessError(stderr, fmt.Errorf("unknown headless operation %q", command))
	}
}

func headlessJSONRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--json" {
			return true
		}
	}
	return false
}

func runHeadlessAgentContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	operation := args[0]
	if operation != "list" && operation != "show" && operation != "cancel" && operation != "reap-stale" {
		return writeHeadlessError(stderr, fmt.Errorf("unknown agent operation %q", args[0]))
	}
	opts, positional, help, err := parseHeadlessArgs(args[1:])
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if operation == "list" && len(positional) != 0 {
		return writeHeadlessError(stderr, fmt.Errorf("agent list does not accept positional arguments"))
	}
	if operation == "show" && len(positional) != 1 {
		return writeHeadlessError(stderr, fmt.Errorf("agent show requires exactly one run id"))
	}
	if operation == "cancel" {
		if !opts.confirm {
			return writeHeadlessError(stderr, fmt.Errorf("agent cancel requires --confirm"))
		}
		if len(positional) != 1 {
			return writeHeadlessError(stderr, fmt.Errorf("agent cancel requires exactly one run id"))
		}
	}
	if operation == "reap-stale" {
		if !opts.confirm {
			return writeHeadlessError(stderr, fmt.Errorf("agent reap-stale requires --confirm"))
		}
		if len(positional) != 0 {
			return writeHeadlessError(stderr, fmt.Errorf("agent reap-stale does not accept positional arguments"))
		}
		if opts.maxSilence == "" {
			return writeHeadlessError(stderr, fmt.Errorf("agent reap-stale requires --max-silence"))
		}
	}
	root, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if err := ctx.Err(); err != nil {
		return writeHeadlessError(stderr, err)
	}
	if operation == "show" {
		response, err := collectHeadlessAgentRunContext(ctx, root, agentruntime.RunID(positional[0]))
		if err != nil {
			return writeHeadlessError(stderr, err)
		}
		if opts.json {
			return writeHeadlessJSON(stdout, response)
		}
		return writeHeadlessText(stdout, stderr, fmt.Sprintf("Workspace: %s\nRun: %s\nStatus: %s\nObjective: %s\n", response.Workspace, response.Run.ID, response.Run.Status, response.Run.Spec.Objective))
	}
	if operation == "reap-stale" {
		maxSilence, parseErr := time.ParseDuration(opts.maxSilence)
		if parseErr != nil || maxSilence <= 0 {
			if parseErr == nil {
				parseErr = errors.New("duration must be positive")
			}
			return writeHeadlessError(stderr, fmt.Errorf("invalid --max-silence %q: %w", opts.maxSilence, parseErr))
		}
		path := headlessAgentStorePath(root)
		manager, managerErr := agentruntime.NewManager(agentruntime.ManagerConfig{
			Store:        agentruntime.FileStore{Path: path},
			SkipRecovery: true,
		})
		if managerErr != nil {
			return writeHeadlessError(stderr, fmt.Errorf("load agent runs: %w", managerErr))
		}
		reaped, reapErr := manager.ReapStale(maxSilence)
		if reapErr != nil {
			return writeHeadlessError(stderr, fmt.Errorf("reap stale agent runs: %w", reapErr))
		}
		response := headlessAgentReapResponse{
			Workspace:  root,
			Path:       path,
			State:      "nothing_to_reap",
			MaxSilence: maxSilence.String(),
			Reaped:     reaped,
		}
		if len(reaped) > 0 {
			response.State = "reaped"
		}
		if opts.json {
			return writeHeadlessJSON(stdout, response)
		}
		var body strings.Builder
		fmt.Fprintf(&body, "Workspace: %s\nState: %s\nReaped: %d\n", root, response.State, len(reaped))
		for _, id := range reaped {
			fmt.Fprintf(&body, "reaped %s\n", id)
		}
		return writeHeadlessText(stdout, stderr, body.String())
	}
	if operation == "cancel" {
		path := headlessAgentStorePath(root)
		manager, managerErr := agentruntime.NewManager(agentruntime.ManagerConfig{
			Store:        agentruntime.FileStore{Path: path},
			SkipRecovery: true,
		})
		if managerErr != nil {
			return writeHeadlessError(stderr, fmt.Errorf("load agent runs: %w", managerErr))
		}
		id := agentruntime.RunID(positional[0])
		if err := manager.Cancel(id); err != nil {
			return writeHeadlessError(stderr, fmt.Errorf("cancel agent run %q: %w", id, err))
		}
		run, err := manager.Get(id)
		if err != nil {
			return writeHeadlessError(stderr, fmt.Errorf("load cancelled agent run %q: %w", id, err))
		}
		state := "already_terminal"
		if run.Status == agentruntime.RunCancelled {
			state = "cancelled"
		}
		response := headlessAgentCancelResponse{
			Workspace: root,
			Path:      path,
			State:     state,
			RunID:     id,
		}
		if opts.json {
			return writeHeadlessJSON(stdout, response)
		}
		return writeHeadlessText(stdout, stderr, fmt.Sprintf("Workspace: %s\nState: %s\nRun: %s\n", root, response.State, id))
	}
	response, err := collectHeadlessAgentRunsContext(ctx, root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if opts.json {
		return writeHeadlessJSON(stdout, response)
	}
	var body strings.Builder
	fmt.Fprintf(&body, "Workspace: %s\nRuns: %d\n", root, len(response.Runs))
	for _, run := range response.Runs {
		fmt.Fprintf(&body, "%s %-11s %s\n", run.ID, run.Status, run.Spec.Objective)
	}
	return writeHeadlessText(stdout, stderr, body.String())
}

func runHeadlessLSPContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if args[0] != "status" && args[0] != "diagnostics" && args[0] != "format" &&
		args[0] != "symbols" && args[0] != "hover" && args[0] != "definition" && args[0] != "references" {
		return writeHeadlessError(stderr, fmt.Errorf("unknown lsp operation %q", args[0]))
	}
	opts, positional, help, err := parseHeadlessArgs(args[1:])
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if opts.probe && args[0] != "status" {
		return writeHeadlessError(stderr, fmt.Errorf("--probe is only valid for lsp status"))
	}
	root, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if args[0] == "symbols" || args[0] == "hover" || args[0] == "definition" || args[0] == "references" {
		return runHeadlessLSPIntelligenceContext(ctx, root, args[0], opts, positional, stdout, stderr)
	}
	if args[0] == "diagnostics" || args[0] == "format" {
		if len(positional) != 1 {
			return writeHeadlessError(stderr, fmt.Errorf("lsp %s requires exactly one path", args[0]))
		}
		workspace, path, err := resolveHeadlessBufferTarget(root, positional[0])
		if err != nil {
			return writeHeadlessError(stderr, err)
		}
		if args[0] == "format" {
			if opts.apply && opts.expectedSHA256 == "" {
				return writeHeadlessError(stderr, fmt.Errorf("lsp format --apply requires --expected-sha256"))
			}
			response, err := collectHeadlessLSPFormatContext(ctx, workspace, path)
			if err != nil {
				return writeHeadlessError(stderr, err)
			}
			if opts.apply && response.State == "ready" {
				if !strings.EqualFold(strings.TrimSpace(opts.expectedSHA256), response.InputSHA256) {
					return writeHeadlessError(stderr, fmt.Errorf("format snapshot changed before apply: expected %s, found %s", opts.expectedSHA256, response.InputSHA256))
				}
				if response.Changed {
					updated, writeErr := writeHeadlessBufferContext(ctx, workspace, path, opts.expectedSHA256, []byte(response.Content))
					if writeErr != nil {
						return writeHeadlessError(stderr, writeErr)
					}
					response.Applied = true
					response.OutputSHA256 = updated.SHA256
					response.Content = updated.Content
					response.Detail = "formatted content applied"
				} else {
					response.Detail = "no formatting changes"
				}
			}
			if opts.json {
				if code := writeHeadlessJSON(stdout, response); code != 0 {
					return code
				}
			} else {
				var body strings.Builder
				fmt.Fprintf(&body, "Workspace: %s\nFile: %s\nServer: %s\nState: %s\nChanged: %t\nApplied: %t\nEdits: %d\n",
					response.Workspace, response.RelativePath, response.Server, response.State, response.Changed, response.Applied, response.Edits)
				if response.Detail != "" {
					fmt.Fprintln(&body, response.Detail)
				}
				if response.Hint != "" {
					fmt.Fprintf(&body, "hint: %s\n", response.Hint)
				}
				if response.Content != "" {
					body.WriteString(response.Content)
				}
				if code := writeHeadlessText(stdout, stderr, body.String()); code != 0 {
					return code
				}
			}
			if response.State == "failed" || response.State == "missing" || response.State == "unsupported" || response.State == "timed_out" || response.State == "cancelled" {
				return 1
			}
			return 0
		}
		response, err := collectHeadlessLSPDiagnosticsContext(ctx, workspace, path)
		if err != nil {
			return writeHeadlessError(stderr, err)
		}
		if opts.json {
			if code := writeHeadlessJSON(stdout, response); code != 0 {
				return code
			}
		} else {
			var body strings.Builder
			fmt.Fprintf(&body, "Workspace: %s\nFile: %s\nServer: %s\nState: %s\nDiagnostics: %d\n",
				response.Workspace, response.RelativePath, response.Server, response.State, len(response.Diagnostics))
			for _, diagnostic := range response.Diagnostics {
				fmt.Fprintf(&body, "%s:%d:%d: %s\n", response.RelativePath, diagnostic.Line+1, diagnostic.Column+1, diagnostic.Message)
			}
			if response.Detail != "" {
				fmt.Fprintln(&body, response.Detail)
			}
			if response.Hint != "" {
				fmt.Fprintf(&body, "hint: %s\n", response.Hint)
			}
			if code := writeHeadlessText(stdout, stderr, body.String()); code != 0 {
				return code
			}
		}
		if response.State == "failed" || response.State == "missing" || response.State == "unsupported" || response.State == "timed_out" || response.State == "cancelled" {
			return 1
		}
		return 0
	}
	if len(positional) != 0 {
		return writeHeadlessError(stderr, fmt.Errorf("lsp status does not accept positional arguments"))
	}
	response, err := collectHeadlessLSPStatusContextWithProbe(ctx, root, opts.probe)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if opts.json {
		return writeHeadlessJSON(stdout, response)
	}
	var body strings.Builder
	fmt.Fprintf(&body, "Workspace: %s\n", root)
	for _, server := range response.Servers {
		state := server.State
		detail := server.LanguageID + " (" + strings.Join(server.Extensions, ",") + ")"
		if server.Path != "" {
			detail += " — " + server.Path
		}
		fmt.Fprintf(&body, "%-12s %-10s %s\n", server.Command, state, detail)
		if server.Hint != "" {
			fmt.Fprintf(&body, "              hint: %s\n", server.Hint)
		}
	}
	return writeHeadlessText(stdout, stderr, body.String())
}

func runHeadlessDAPContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if args[0] != "status" && args[0] != "probe" {
		return writeHeadlessError(stderr, fmt.Errorf("unknown dap operation %q", args[0]))
	}
	if args[0] == "probe" {
		return runHeadlessDAPProbeContext(ctx, args[1:], stdout, stderr)
	}
	opts, positional, help, err := parseHeadlessArgs(args[1:])
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if len(positional) != 0 {
		return writeHeadlessError(stderr, fmt.Errorf("dap status does not accept positional arguments"))
	}
	root, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	response := collectHeadlessDAPStatusContext(ctx, root)
	if opts.json {
		return writeHeadlessJSON(stdout, response)
	}
	var body strings.Builder
	fmt.Fprintf(&body, "Workspace: %s\n", root)
	for _, adapter := range response.Adapters {
		detail := adapter.Type + " (" + strings.Join(adapter.Extensions, ",") + ")"
		if adapter.Path != "" {
			detail += " — " + adapter.Path
		}
		fmt.Fprintf(&body, "%-12s %-10s %s\n", adapter.Command, adapter.State, detail)
		if adapter.Hint != "" {
			fmt.Fprintf(&body, "              hint: %s\n", adapter.Hint)
		}
	}
	return writeHeadlessText(stdout, stderr, body.String())
}

func runHeadlessDAPProbeContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	var opts headlessOptions
	var adapter string
	var adapterArgs []string
	separator := false
	for i := 0; i < len(args); i++ {
		if separator {
			adapterArgs = append(adapterArgs, args[i])
			continue
		}
		switch args[i] {
		case "--":
			separator = true
		case "--json":
			opts.json = true
		case "--root":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return writeHeadlessError(stderr, fmt.Errorf("--root requires a directory"))
			}
			opts.root = args[i+1]
			i++
		case "--adapter":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return writeHeadlessError(stderr, fmt.Errorf("--adapter requires a command"))
			}
			adapter = strings.TrimSpace(args[i+1])
			i++
		case "-h", "--help":
			_, _ = io.WriteString(stdout, headlessUsageText)
			return 0
		default:
			return writeHeadlessError(stderr, fmt.Errorf("unknown dap probe option %q", args[i]))
		}
	}
	root, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if adapter == "" {
		config := dap.DefaultGoDebugConfig(filepath.Join(root, "main.go"))
		adapter = config.Command
		adapterArgs = append([]string(nil), config.Args...)
	}
	response := collectHeadlessDAPProbeContext(ctx, root, adapter, adapterArgs)
	if opts.json {
		if code := writeHeadlessJSON(stdout, response); code != 0 {
			return code
		}
	} else {
		var body strings.Builder
		fmt.Fprintf(&body, "Workspace: %s\nAdapter: %s %s\nState: %s\nReady: %t\n",
			response.Workspace, response.Adapter, strings.Join(response.Args, " "), response.State, response.Ready)
		if response.Detail != "" {
			fmt.Fprintln(&body, response.Detail)
		}
		if response.Hint != "" {
			fmt.Fprintf(&body, "hint: %s\n", response.Hint)
		}
		if code := writeHeadlessText(stdout, stderr, body.String()); code != 0 {
			return code
		}
	}
	if response.State != "ready" {
		return 1
	}
	return 0
}

func runHeadlessSessionContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	operation := args[0]
	if operation != "show" && operation != "list" && operation != "save" && operation != "activate" && operation != "health" && operation != "cleanup" {
		return writeHeadlessError(stderr, fmt.Errorf("unknown session operation %q", args[0]))
	}
	opts, positional, help, err := parseHeadlessArgs(args[1:])
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	root, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if err := ctx.Err(); err != nil {
		return writeHeadlessError(stderr, err)
	}

	switch operation {
	case "list":
		if len(positional) != 0 || opts.name != "" {
			return writeHeadlessError(stderr, fmt.Errorf("session list does not accept a name or positional arguments"))
		}
		names, err := session.ListNamed(root)
		if err != nil {
			return writeHeadlessError(stderr, fmt.Errorf("list named sessions: %w", err))
		}
		response := headlessSessionListResponse{Workspace: root, Names: names}
		if opts.json {
			return writeHeadlessJSON(stdout, response)
		}
		var body strings.Builder
		fmt.Fprintf(&body, "Workspace: %s\nNamed sessions: %d\n", root, len(names))
		for _, name := range names {
			fmt.Fprintln(&body, name)
		}
		return writeHeadlessText(stdout, stderr, body.String())
	case "save":
		if len(positional) != 1 || opts.name != "" {
			return writeHeadlessError(stderr, fmt.Errorf("session save requires exactly one name"))
		}
		state, loadErr := session.LoadContextForRoot(ctx, root)
		if loadErr != nil {
			return writeHeadlessError(stderr, fmt.Errorf("load current session: %w", loadErr))
		}
		if err := session.SaveNamed(state, positional[0]); err != nil {
			return writeHeadlessError(stderr, fmt.Errorf("save named session: %w", err))
		}
		response := collectHeadlessSessionByNameContext(ctx, root, positional[0])
		if opts.json {
			return writeHeadlessJSON(stdout, response)
		}
		return writeHeadlessText(stdout, stderr, fmt.Sprintf("Saved session %q for %s\n", positional[0], root))
	case "activate":
		if len(positional) != 1 || opts.name != "" {
			return writeHeadlessError(stderr, fmt.Errorf("session activate requires exactly one name"))
		}
		state, loadErr := session.LoadNamed(ctx, root, positional[0])
		if loadErr != nil {
			return writeHeadlessError(stderr, fmt.Errorf("load named session: %w", loadErr))
		}
		if err := session.Save(state); err != nil {
			return writeHeadlessError(stderr, fmt.Errorf("activate named session: %w", err))
		}
		response := collectHeadlessSessionContext(ctx, root)
		if opts.json {
			return writeHeadlessJSON(stdout, response)
		}
		return writeHeadlessText(stdout, stderr, fmt.Sprintf("Activated session %q for %s\n", positional[0], root))
	case "health":
		if len(positional) != 0 {
			return writeHeadlessError(stderr, fmt.Errorf("session health does not accept positional arguments"))
		}
		var health []session.NamedHealth
		if opts.name != "" {
			entry, checkErr := session.CheckNamed(ctx, root, opts.name)
			if checkErr != nil {
				return writeHeadlessError(stderr, fmt.Errorf("check named session: %w", checkErr))
			}
			health = []session.NamedHealth{entry}
		} else {
			health, err = session.CheckNamedAll(ctx, root)
			if err != nil {
				return writeHeadlessError(stderr, fmt.Errorf("check named sessions: %w", err))
			}
		}
		state := aggregateSessionHealth(health)
		response := headlessSessionHealthResponse{Workspace: root, State: state, Sessions: health}
		if opts.json {
			if code := writeHeadlessJSON(stdout, response); code != 0 {
				return code
			}
		} else {
			var body strings.Builder
			fmt.Fprintf(&body, "Workspace: %s\nState: %s\nSessions: %d\n", root, state, len(health))
			for _, entry := range health {
				fmt.Fprintf(&body, "%s: %s (%d tabs, %d issues)\n", entry.Name, entry.State, entry.Tabs, len(entry.Issues))
			}
			if code := writeHeadlessText(stdout, stderr, body.String()); code != 0 {
				return code
			}
		}
		if state == "stale" || state == "invalid" || state == "missing" {
			return 1
		}
		return 0
	case "cleanup":
		if !opts.confirm {
			return writeHeadlessError(stderr, fmt.Errorf("session cleanup requires --confirm"))
		}
		if len(positional) > 1 || (len(positional) == 1 && opts.name != "") {
			return writeHeadlessError(stderr, fmt.Errorf("session cleanup accepts at most one name"))
		}
		name := opts.name
		if len(positional) == 1 {
			name = positional[0]
		}
		var health []session.NamedHealth
		if name != "" {
			entry, checkErr := session.CheckNamed(ctx, root, name)
			if checkErr != nil {
				return writeHeadlessError(stderr, fmt.Errorf("check named session: %w", checkErr))
			}
			health = []session.NamedHealth{entry}
		} else {
			health, err = session.CheckNamedAll(ctx, root)
			if err != nil {
				return writeHeadlessError(stderr, fmt.Errorf("check named sessions: %w", err))
			}
		}
		response := headlessSessionCleanupResponse{Workspace: root, Sessions: health}
		for _, entry := range health {
			if entry.State != "stale" {
				response.Skipped = append(response.Skipped, entry.Name)
				continue
			}
			if removeErr := session.RemoveNamed(root, entry.Name); removeErr != nil {
				return writeHeadlessError(stderr, fmt.Errorf("remove stale session %q: %w", entry.Name, removeErr))
			}
			response.Removed = append(response.Removed, entry.Name)
		}
		if len(response.Removed) > 0 {
			response.State = "cleaned"
		} else {
			response.State = "nothing_to_clean"
		}
		if opts.json {
			return writeHeadlessJSON(stdout, response)
		}
		var body strings.Builder
		fmt.Fprintf(&body, "Workspace: %s\nState: %s\nRemoved: %d\n", root, response.State, len(response.Removed))
		for _, removed := range response.Removed {
			fmt.Fprintf(&body, "removed %s\n", removed)
		}
		return writeHeadlessText(stdout, stderr, body.String())
	default:
		if len(positional) != 0 {
			return writeHeadlessError(stderr, fmt.Errorf("session show does not accept positional arguments"))
		}
		if opts.name != "" {
			if _, err := session.NamedPath(root, opts.name); err != nil {
				return writeHeadlessError(stderr, err)
			}
		}
		response := collectHeadlessSessionByNameContext(ctx, root, opts.name)
		if err := ctx.Err(); err != nil {
			return writeHeadlessError(stderr, err)
		}
		if opts.json {
			return writeHeadlessJSON(stdout, response)
		}
		var body strings.Builder
		fmt.Fprintf(&body, "Workspace: %s\nSession: %s\nPath: %s\n", response.Workspace, response.State, response.Path)
		if response.Name != "" {
			fmt.Fprintf(&body, "Name: %s\n", response.Name)
		}
		if response.Detail != "" {
			fmt.Fprintln(&body, response.Detail)
		}
		if response.Session != nil {
			fmt.Fprintf(&body, "Tabs: %d\n", len(response.Session.Tabs))
		}
		return writeHeadlessText(stdout, stderr, body.String())
	}
}

func aggregateSessionHealth(health []session.NamedHealth) string {
	if len(health) == 0 {
		return "empty"
	}
	for _, entry := range health {
		if entry.State == "invalid" {
			return "invalid"
		}
	}
	for _, entry := range health {
		if entry.State == "stale" || entry.State == "missing" {
			return "stale"
		}
	}
	return "healthy"
}

func runHeadlessToolsContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if args[0] != "status" {
		return writeHeadlessError(stderr, fmt.Errorf("unknown tools operation %q", args[0]))
	}
	opts, positional, help, err := parseHeadlessArgs(args[1:])
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if len(positional) != 0 {
		return writeHeadlessError(stderr, fmt.Errorf("tools status does not accept positional arguments"))
	}
	root, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	operationCtx, cancel := context.WithTimeout(ctx, headlessHealthTimeout)
	defer cancel()
	response := collectHeadlessToolStatusContext(operationCtx, root)
	if opts.json {
		return writeHeadlessJSON(stdout, response)
	}
	var body strings.Builder
	fmt.Fprintf(&body, "Workspace: %s\n", root)
	for _, tool := range response.Tools {
		line := fmt.Sprintf("%-8s %-10s %s", strings.ToUpper(tool.State), tool.Name, tool.Detail)
		if tool.Path != "" {
			line += " (" + tool.Path + ")"
		}
		fmt.Fprintln(&body, strings.TrimSpace(line))
		if tool.Hint != "" {
			fmt.Fprintf(&body, "         hint: %s\n", tool.Hint)
		}
	}
	return writeHeadlessText(stdout, stderr, body.String())
}

func runHeadlessHitspecContext(parentCtx context.Context, args []string, stdout, stderr io.Writer) int {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if args[0] != "validate" {
		return writeHeadlessError(stderr, fmt.Errorf("unknown hitspec operation %q", args[0]))
	}
	opts, positional, help, err := parseHeadlessArgs(args[1:])
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if len(positional) != 1 {
		return writeHeadlessError(stderr, fmt.Errorf("hitspec validate requires exactly one file or directory"))
	}
	if err := parentCtx.Err(); err != nil {
		return writeHeadlessError(stderr, err)
	}
	root, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	target, relative, err := resolveHeadlessWorkspaceTarget(root, positional[0])
	if err != nil {
		return writeHeadlessError(stderr, err)
	}

	started := time.Now()
	response := headlessHitspecValidationResponse{
		Workspace:    root,
		Path:         target,
		RelativePath: relative,
		State:        "failed",
		Results:      make([]headlessHitspecResult, 0),
	}
	if err := parentCtx.Err(); err != nil {
		response.State = headlessHitspecContextState(err)
		response.Detail = err.Error()
		response.DurationMS = elapsedHeadlessMS(started)
		return writeHeadlessHitspecResponse(stdout, response, opts.json, 1)
	}
	ctx, cancel := context.WithTimeout(parentCtx, 15*time.Second)
	defer cancel()
	cmd, err := toolpath.Command(ctx, "hitspec", "validate", target, "--json")
	if err != nil {
		response.State = "missing"
		response.Detail = err.Error()
		if missing, ok := err.(*toolpath.MissingToolError); ok {
			response.Hint = missing.Hint
		}
		response.DurationMS = elapsedHeadlessMS(started)
		return writeHeadlessHitspecResponse(stdout, response, opts.json, 1)
	}
	cmd.Dir = root
	stdoutBuffer := &headlessOutputBuffer{limit: headlessMaxCommandOutput, onLimit: cancel}
	stderrBuffer := &headlessOutputBuffer{limit: headlessMaxCommandOutput, onLimit: cancel}
	cmd.Stdout = stdoutBuffer
	cmd.Stderr = stderrBuffer
	runErr := cmd.Run()
	response.DurationMS = elapsedHeadlessMS(started)
	if err := parentCtx.Err(); err != nil {
		response.State = headlessHitspecContextState(err)
		response.Detail = err.Error()
		return writeHeadlessHitspecResponse(stdout, response, opts.json, 1)
	}
	if stdoutBuffer.truncated || stderrBuffer.truncated {
		response.Detail = fmt.Sprintf("hitspec output exceeds %d-byte stream limit", headlessMaxCommandOutput)
		return writeHeadlessHitspecResponse(stdout, response, opts.json, 1)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		response.State = "timed_out"
		response.Detail = "hitspec validation timed out"
		return writeHeadlessHitspecResponse(stdout, response, opts.json, 1)
	}

	var results []headlessHitspecResult
	if parseErr := json.Unmarshal(stdoutBuffer.Bytes(), &results); parseErr != nil {
		response.Detail = fmt.Sprintf("parse hitspec validation output: %v", parseErr)
		if stderrText := strings.TrimSpace(stderrBuffer.String()); stderrText != "" {
			response.Detail += ": " + stderrText
		}
		return writeHeadlessHitspecResponse(stdout, response, opts.json, 1)
	}
	normalizedResults, contractErr := validateHeadlessHitspecResults(root, target, results)
	if contractErr != nil {
		response.Detail = "hitspec output contract invalid: " + contractErr.Error()
		return writeHeadlessHitspecResponse(stdout, response, opts.json, 1)
	}
	response.Results = normalizedResults
	response.Files = len(results)
	response.Valid = true
	for _, result := range results {
		if !result.OK {
			response.Valid = false
		}
	}
	if !response.Valid {
		response.State = "invalid"
		response.Detail = "hitspec validation found one or more invalid files"
		return writeHeadlessHitspecResponse(stdout, response, opts.json, 1)
	}
	if runErr != nil {
		response.Detail = runErr.Error()
		if stderrText := strings.TrimSpace(stderrBuffer.String()); stderrText != "" {
			response.Detail += ": " + stderrText
		}
		return writeHeadlessHitspecResponse(stdout, response, opts.json, 1)
	}
	response.State = "ready"
	response.Detail = fmt.Sprintf("validated %d file(s)", response.Files)
	return writeHeadlessHitspecResponse(stdout, response, opts.json, 0)
}

func headlessHitspecContextState(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed_out"
	}
	return "cancelled"
}

// validateHeadlessHitspecResults treats the external validator's JSON as
// untrusted input. Normalize paths to workspace-relative slash form and reject
// malformed or out-of-root records before exposing them to an agent/UI. A
// validator must also keep its ok/errors fields internally consistent; otherwise
// callers cannot safely distinguish a valid result from a broken adapter.
func validateHeadlessHitspecResults(root, target string, results []headlessHitspecResult) ([]headlessHitspecResult, error) {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	targetReal, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil, fmt.Errorf("resolve target: %w", err)
	}
	targetInfo, err := os.Stat(targetReal)
	if err != nil {
		return nil, fmt.Errorf("stat target: %w", err)
	}

	normalized := make([]headlessHitspecResult, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for index, result := range results {
		file := strings.TrimSpace(result.File)
		if file == "" {
			return nil, fmt.Errorf("result %d has an empty file", index)
		}
		if strings.ContainsRune(file, '\x00') {
			return nil, fmt.Errorf("result %d contains a NUL in file", index)
		}
		if result.OK && len(result.Errors) > 0 {
			return nil, fmt.Errorf("result %d is ok but includes errors", index)
		}
		if !result.OK && len(result.Errors) == 0 {
			return nil, fmt.Errorf("result %d is invalid but has no errors", index)
		}

		candidate := file
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(rootReal, candidate)
		}
		candidate, err = filepath.Abs(candidate)
		if err != nil {
			return nil, fmt.Errorf("result %d path %q: %w", index, file, err)
		}
		candidateReal := candidate
		if resolved, resolveErr := filepath.EvalSymlinks(candidate); resolveErr == nil {
			candidateReal = resolved
		}
		candidateInfo, statErr := os.Stat(candidateReal)
		if statErr != nil {
			return nil, fmt.Errorf("result %d file %q does not exist: %w", index, file, statErr)
		}
		if !candidateInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("result %d file %q is not a regular file", index, file)
		}
		relative, relErr := filepath.Rel(rootReal, candidateReal)
		if relErr != nil || filepath.IsAbs(relative) || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("result %d file %q is outside workspace", index, file)
		}

		if targetInfo.IsDir() {
			targetRelative, targetErr := filepath.Rel(targetReal, candidateReal)
			if targetErr != nil || filepath.IsAbs(targetRelative) || targetRelative == ".." || strings.HasPrefix(targetRelative, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("result %d file %q is outside validation target", index, file)
			}
		} else if filepath.Clean(candidateReal) != filepath.Clean(targetReal) {
			return nil, fmt.Errorf("result %d file %q does not identify the validation target", index, file)
		}

		relative = filepath.ToSlash(relative)
		if _, duplicate := seen[relative]; duplicate {
			return nil, fmt.Errorf("result %d repeats file %q", index, relative)
		}
		seen[relative] = struct{}{}
		result.File = relative
		normalized = append(normalized, result)
	}
	return normalized, nil
}

func writeHeadlessHitspecResponse(stdout io.Writer, response headlessHitspecValidationResponse, jsonOutput bool, exitCode int) int {
	if jsonOutput {
		if code := writeHeadlessJSON(stdout, response); code != 0 {
			return code
		}
		return exitCode
	}
	var body strings.Builder
	fmt.Fprintf(&body, "Workspace: %s\nPath: %s\nState: %s\nValid: %t\nFiles: %d\n",
		response.Workspace, response.RelativePath, response.State, response.Valid, response.Files)
	if response.Detail != "" {
		fmt.Fprintf(&body, "Detail: %s\n", response.Detail)
	}
	for _, result := range response.Results {
		state := "ok"
		if !result.OK {
			state = "invalid"
		}
		fmt.Fprintf(&body, "%s %s\n", state, result.File)
		for _, problem := range result.Errors {
			fmt.Fprintf(&body, "  %s\n", problem)
		}
	}
	if _, err := io.WriteString(stdout, body.String()); err != nil {
		return 1
	}
	return exitCode
}

func runHeadlessGitContext(parentCtx context.Context, args []string, stdout, stderr io.Writer) int {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if args[0] != "status" {
		return writeHeadlessError(stderr, fmt.Errorf("unknown git operation %q", args[0]))
	}
	opts, positional, help, err := parseHeadlessArgs(args[1:])
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if len(positional) != 0 {
		return writeHeadlessError(stderr, fmt.Errorf("git status does not accept positional arguments"))
	}
	root, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	ctx, cancel := context.WithTimeout(parentCtx, 15*time.Second)
	defer cancel()
	snapshot, err := git.StatusContext(ctx, root)
	if err != nil && parentCtx.Err() != nil {
		return writeHeadlessError(stderr, parentCtx.Err())
	}
	response := headlessGitResponse{
		Workspace: root,
		State:     "ready",
		Entries:   make([]headlessGitEntry, 0),
	}
	if err != nil {
		response.State = "unavailable"
		response.Detail = err.Error()
	} else {
		response.Branch = snapshot.Branch
		response.Entries = make([]headlessGitEntry, 0, len(snapshot.Entries))
		for _, entry := range snapshot.Entries {
			response.Entries = append(response.Entries, headlessGitEntry{
				Path:         entry.Path,
				OriginalPath: entry.OriginalPath,
				IndexStatus:  string(entry.IndexStatus),
				WorkStatus:   string(entry.WorkStatus),
				Staged:       entry.IsStagedChange(),
				Unstaged:     entry.IsUnstagedChange(),
				Untracked:    entry.IsUntracked(),
			})
		}
	}
	if opts.json {
		if code := writeHeadlessJSON(stdout, response); code != 0 {
			return code
		}
	} else {
		var body strings.Builder
		fmt.Fprintf(&body, "Workspace: %s\nState: %s\nBranch: %s\nChanges: %d\n",
			response.Workspace, response.State, response.Branch, len(response.Entries))
		for _, entry := range response.Entries {
			status := entry.IndexStatus + entry.WorkStatus
			path := entry.Path
			if entry.OriginalPath != "" {
				path += " <- " + entry.OriginalPath
			}
			fmt.Fprintf(&body, "%s %s\n", status, path)
		}
		if response.Detail != "" {
			fmt.Fprintln(&body, response.Detail)
		}
		if code := writeHeadlessText(stdout, stderr, body.String()); code != 0 {
			return code
		}
	}
	if response.State != "ready" {
		return 1
	}
	return 0
}

func runHeadlessContextContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	opts, positional, help, err := parseHeadlessArgs(args)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if len(positional) != 0 {
		return writeHeadlessError(stderr, fmt.Errorf("context does not accept positional arguments"))
	}
	depth := 0
	if opts.depthSet {
		if opts.depth < 0 || opts.depth > headlessMaxContextDepth {
			return writeHeadlessError(stderr, fmt.Errorf("context depth must be between 0 and %d", headlessMaxContextDepth))
		}
		depth = opts.depth
	}
	root, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	response, err := collectHeadlessContextDepthContext(ctx, root, depth)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if opts.json {
		return writeHeadlessJSON(stdout, response)
	}
	var body strings.Builder
	fmt.Fprintf(&body, "Workspace: %s\n", response.Workspace)
	if response.ProjectRoot != "" {
		fmt.Fprintf(&body, "Project root: %s\n", response.ProjectRoot)
	}
	fmt.Fprintf(&body, "Entries: %d%s\n", len(response.Entries), truncatedSuffix(response.Truncated))
	for _, entry := range response.Entries {
		fmt.Fprintf(&body, "%-9s %s\n", entry.Kind, entry.Path)
	}
	return writeHeadlessText(stdout, stderr, body.String())
}

func runHeadlessBufferContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if args[0] != "read" && args[0] != "write" {
		return writeHeadlessError(stderr, fmt.Errorf("unknown buffer operation %q", args[0]))
	}
	opts, positional, help, err := parseHeadlessArgs(args[1:])
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if len(positional) != 1 {
		return writeHeadlessError(stderr, fmt.Errorf("buffer %s requires exactly one path", args[0]))
	}
	if err := ctx.Err(); err != nil {
		return writeHeadlessError(stderr, err)
	}
	root, path, err := resolveHeadlessBufferTarget(opts.root, positional[0])
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	var response headlessBufferResponse
	if args[0] == "write" {
		if opts.expectedSHA256 == "" {
			return writeHeadlessError(stderr, fmt.Errorf("buffer write requires --expected-sha256"))
		}
		data, readErr := io.ReadAll(io.LimitReader(stdin, headlessMaxBufferBytes+1))
		if readErr != nil {
			return writeHeadlessError(stderr, fmt.Errorf("read buffer input: %w", readErr))
		}
		if len(data) > headlessMaxBufferBytes {
			return writeHeadlessError(stderr, fmt.Errorf("buffer input exceeds %d-byte write limit", headlessMaxBufferBytes))
		}
		response, err = writeHeadlessBufferContext(ctx, root, path, opts.expectedSHA256, data)
	} else {
		response, err = readHeadlessBufferContext(ctx, root, path)
	}
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if opts.json {
		return writeHeadlessJSON(stdout, response)
	}
	if _, err := io.WriteString(stdout, response.Content); err != nil {
		return writeHeadlessError(stderr, fmt.Errorf("write buffer content: %w", err))
	}
	return 0
}

func runHeadlessSearchContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	opts, positional, help, err := parseHeadlessArgs(args)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if opts.index && !opts.semantic {
		return writeHeadlessError(stderr, fmt.Errorf("--index requires --semantic"))
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return writeHeadlessError(stderr, fmt.Errorf("search requires exactly one non-empty query"))
	}
	root, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if opts.semantic {
		return runHeadlessSemanticSearchContext(ctx, root, positional[0], opts, stdout, stderr)
	}
	operationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	started := time.Now()
	results, err := search.TextSearchContext(operationCtx, root, positional[0], search.SearchOpts{
		Regex:         opts.regex,
		CaseSensitive: opts.caseSensitive,
	})
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	results, err = search.ValidateResults(root, results)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	response := headlessSearchResponse{
		Workspace:  root,
		Query:      positional[0],
		Mode:       "text",
		State:      "ready",
		Results:    make([]headlessSearchResult, 0, len(results)),
		DurationMS: float64(time.Since(started).Microseconds()) / 1000,
	}
	response.Results = mapHeadlessSearchResults(results)
	response.Truncated = len(results) >= search.MaxTextSearchResults
	if opts.json {
		return writeHeadlessJSON(stdout, response)
	}
	var body strings.Builder
	fmt.Fprintf(&body, "Workspace: %s\nQuery: %s\nResults: %d\n", root, positional[0], len(response.Results))
	for _, result := range response.Results {
		fmt.Fprintf(&body, "%s:%d:%d: %s\n", result.FilePath, result.Line+1, result.Column+1, result.Preview)
	}
	return writeHeadlessText(stdout, stderr, body.String())
}

const headlessSemanticIndexTimeout = 10 * time.Minute

func runHeadlessSemanticSearchContext(parentCtx context.Context, root, query string, opts headlessOptions, stdout, stderr io.Writer) int {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	timeout := 15 * time.Second
	if opts.index {
		timeout = headlessSemanticIndexTimeout
	}
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()
	started := time.Now()
	response := headlessSearchResponse{
		Workspace: root,
		Query:     query,
		Mode:      "semantic",
		Results:   make([]headlessSearchResult, 0),
	}

	if !opts.index {
		status, err := search.VecgrepLightweightStatusContext(ctx, root)
		if err != nil {
			response.State, response.Detail, response.Hint = semanticHeadlessFailure(err)
			response.DurationMS = elapsedHeadlessMS(started)
			return writeHeadlessSearchResponse(stdout, response, opts.json, 1)
		}
		response.Files = status.Files
		response.Pending = status.PendingChanges
		if !status.Ready() {
			response.State = semanticHeadlessStatusState(status)
			response.Detail = "semantic search is read-only; run with --semantic --index to build the index"
			response.DurationMS = elapsedHeadlessMS(started)
			return writeHeadlessSearchResponse(stdout, response, opts.json, 1)
		}
	}

	if opts.index {
		// An explicit index request must not be satisfied by the in-process ready
		// cache: the caller asked for a fresh build/checkpoint now.
		search.InvalidateSemanticIndex(root)
	}
	var (
		results []search.Result
		err     error
	)
	if opts.index {
		results, err = search.SemanticSearchIndexContext(ctx, root, query)
	} else {
		results, err = search.SemanticSearchIndexedContext(ctx, root, query)
	}
	if err != nil {
		response.State, response.Detail, response.Hint = semanticHeadlessFailure(err)
		response.DurationMS = elapsedHeadlessMS(started)
		return writeHeadlessSearchResponse(stdout, response, opts.json, 1)
	}
	results, err = search.ValidateResults(root, results)
	if err != nil {
		response.State = "failed"
		response.Detail = err.Error()
		response.DurationMS = elapsedHeadlessMS(started)
		return writeHeadlessSearchResponse(stdout, response, opts.json, 1)
	}

	response.State = "ready"
	response.Indexed = true
	response.Results = mapHeadlessSearchResults(results)
	response.Truncated = len(results) >= search.MaxSemanticSearchResults
	if opts.index {
		// Keep the response metadata vector-free. The compatibility status path
		// can open the full HNSW graph on older vecgrep releases, immediately
		// after the explicit index operation has already used the expensive path.
		if status, statusErr := search.VecgrepLightweightStatusContext(ctx, root); statusErr == nil {
			response.Files = status.Files
			response.Pending = status.PendingChanges
		} else if errors.Is(statusErr, search.ErrVecgrepLightweightUnsupported) {
			response.Detail = statusErr.Error()
			response.Hint = "upgrade vecgrep to a release with status --lightweight"
		}
	}
	response.DurationMS = elapsedHeadlessMS(started)
	return writeHeadlessSearchResponse(stdout, response, opts.json, 0)
}

func mapHeadlessSearchResults(results []search.Result) []headlessSearchResult {
	out := make([]headlessSearchResult, 0, len(results))
	for _, result := range results {
		out = append(out, headlessSearchResult{
			FilePath:   result.FilePath,
			Line:       result.Line,
			Column:     result.Col,
			Preview:    result.Preview,
			Score:      result.Score,
			SymbolName: result.SymbolName,
			ChunkType:  result.ChunkType,
			EndLine:    result.EndLine,
		})
	}
	return out
}

func elapsedHeadlessMS(started time.Time) float64 {
	return float64(time.Since(started).Microseconds()) / 1000
}

func semanticHeadlessStatusState(status search.VecgrepStatus) string {
	if !status.FreshnessKnown {
		return "unknown"
	}
	if status.FilesKnown && status.Files == 0 {
		return "uninitialized"
	}
	return "stale"
}

func semanticHeadlessFailure(err error) (state, detail, hint string) {
	state = "failed"
	detail = err.Error()
	if toolpath.IsMissing(err) {
		state = "missing"
		var missing *toolpath.MissingToolError
		if errors.As(err, &missing) {
			hint = missing.Hint
		}
		return state, detail, hint
	}
	if errors.Is(err, search.ErrVecgrepLightweightUnsupported) {
		state = "unsupported"
		hint = "upgrade vecgrep to a release with status --lightweight"
		return state, detail, hint
	}
	if errors.Is(err, search.ErrSemanticIndexNotReady) {
		state = "stale"
		return state, detail, hint
	}
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "not in a vecgrep project") ||
		strings.Contains(lower, "not initialized") ||
		strings.Contains(lower, "no vecgrep project") ||
		strings.Contains(lower, "project not registered") {
		state = "uninitialized"
	}
	return state, detail, hint
}

func writeHeadlessSearchResponse(stdout io.Writer, response headlessSearchResponse, jsonOutput bool, exitCode int) int {
	if jsonOutput {
		if code := writeHeadlessJSON(stdout, response); code != 0 {
			return code
		}
		return exitCode
	}
	var body strings.Builder
	fmt.Fprintf(&body, "Workspace: %s\nMode: %s\nState: %s\nQuery: %s\nResults: %d\n", response.Workspace, response.Mode, response.State, response.Query, len(response.Results))
	if response.Detail != "" {
		fmt.Fprintf(&body, "Detail: %s\n", response.Detail)
	}
	for _, result := range response.Results {
		fmt.Fprintf(&body, "%s:%d:%d: %s\n", result.FilePath, result.Line+1, result.Column+1, result.Preview)
	}
	if _, err := io.WriteString(stdout, body.String()); err != nil {
		return 1
	}
	return exitCode
}

func runHeadlessCodemapContext(parentCtx context.Context, args []string, stdout, stderr io.Writer) int {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	operation := args[0]
	switch operation {
	case "context", "callers", "callees", "find", "impact", "symbols", "symbol-at":
	default:
		return writeHeadlessError(stderr, fmt.Errorf("unknown codemap operation %q", operation))
	}
	opts, positional, help, err := parseHeadlessArgs(args[1:])
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		if operation == "symbols" || operation == "symbol-at" {
			return writeHeadlessError(stderr, fmt.Errorf("codemap %s requires exactly one workspace-relative path", operation))
		}
		return writeHeadlessError(stderr, fmt.Errorf("codemap %s requires exactly one symbol", operation))
	}
	if opts.depthSet && operation != "impact" {
		return writeHeadlessError(stderr, fmt.Errorf("--depth is only valid for codemap impact"))
	}
	if operation == "symbols" || operation == "symbol-at" {
		if opts.columnSet {
			return writeHeadlessError(stderr, fmt.Errorf("--column is only valid for LSP position queries"))
		}
		if operation == "symbols" && opts.lineSet {
			return writeHeadlessError(stderr, fmt.Errorf("--line is only valid for codemap symbol-at"))
		}
		if operation == "symbol-at" && !opts.lineSet {
			return writeHeadlessError(stderr, fmt.Errorf("codemap symbol-at requires --line"))
		}
	} else if opts.lineSet || opts.columnSet {
		return writeHeadlessError(stderr, fmt.Errorf("--line/--column are only valid for codemap symbol-at or LSP position queries"))
	}
	root, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}

	symbol := strings.TrimSpace(positional[0])
	ctx, cancel := context.WithTimeout(parentCtx, 15*time.Second)
	defer cancel()
	started := time.Now()
	response := headlessCodemapResponse{
		Workspace:  root,
		Operation:  operation,
		Symbol:     symbol,
		State:      "ready",
		DurationMS: 0,
	}
	if operation == "symbols" || operation == "symbol-at" {
		file, pathErr := headlessCodemapFilePath(positional[0])
		if pathErr != nil {
			return writeHeadlessError(stderr, pathErr)
		}
		response.Symbol = ""
		response.Path = file
		if operation == "symbol-at" {
			line := opts.line
			response.Line = &line
		}
	}
	var queryErr error
	switch operation {
	case "context":
		var result *codemap.ContextResult
		result, queryErr = codemap.Context(ctx, root, symbol)
		if result != nil {
			bounded, truncated := boundHeadlessCodemapContext(*result)
			response.Context = &bounded
			response.Truncated = truncated
		}
	case "callers", "callees", "find":
		var results []codemap.Symbol
		switch operation {
		case "callers":
			results, queryErr = codemap.Callers(ctx, root, symbol)
		case "callees":
			results, queryErr = codemap.Callees(ctx, root, symbol)
		case "find":
			results, queryErr = codemap.Find(ctx, root, symbol)
		}
		response.Results, response.Truncated = boundHeadlessCodemapSymbols(results)
	case "impact":
		depth := 3
		if opts.depthSet {
			depth = opts.depth
		}
		var result *codemap.ImpactResult
		result, queryErr = codemap.Impact(ctx, root, symbol, depth)
		if result != nil {
			bounded, truncated := boundHeadlessCodemapImpact(*result)
			response.Impact = &bounded
			response.Truncated = truncated
		}
	case "symbols":
		var results []codemap.Symbol
		results, queryErr = codemap.Symbols(ctx, root, response.Path)
		response.Results, response.Truncated = boundHeadlessCodemapSymbols(results)
	case "symbol-at":
		result, symbolErr := codemap.SymbolAt(ctx, root, response.Path, opts.line)
		queryErr = symbolErr
		if result != nil {
			response.Results = []codemap.Symbol{*result}
		}
	}
	response.DurationMS = float64(time.Since(started).Microseconds()) / 1000
	if queryErr != nil {
		return writeHeadlessError(stderr, queryErr)
	}
	if opts.json {
		return writeHeadlessJSON(stdout, response)
	}
	var body strings.Builder
	fmt.Fprintf(&body, "Workspace: %s\nOperation: %s\nSymbol: %s\nState: %s\nDuration: %.3fms\n",
		response.Workspace, response.Operation, response.Symbol, response.State, response.DurationMS)
	if response.Context != nil {
		fmt.Fprintf(&body, "Definitions: %d\nCallers: %d\nCallees: %d\nReferences: %d\nTests: %d\n",
			len(response.Context.Definitions), len(response.Context.Callers), len(response.Context.Callees),
			len(response.Context.References), len(response.Context.Tests))
	}
	if response.Impact != nil {
		fmt.Fprintf(&body, "Locations: %d\nDirect callers: %d\nBlast radius: %d\nTests: %d\n",
			len(response.Impact.Locations), len(response.Impact.DirectCallers), len(response.Impact.BlastRadius), len(response.Impact.Tests))
	}
	if response.Results != nil {
		fmt.Fprintf(&body, "Results: %d%s\n", len(response.Results), truncatedSuffix(response.Truncated))
		for _, result := range response.Results {
			fmt.Fprintf(&body, "%s:%d: %s\n", result.File, result.StartLine, result.Symbol)
		}
	}
	return writeHeadlessText(stdout, stderr, body.String())
}

func headlessCodemapFilePath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("codemap file path is empty")
	}
	if strings.IndexByte(raw, 0) >= 0 {
		return "", errors.New("codemap file path contains a NUL byte")
	}
	if filepath.IsAbs(raw) {
		return "", fmt.Errorf("codemap file path %q must be relative to the workspace", raw)
	}
	clean := filepath.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("codemap file path %q is outside the workspace", raw)
	}
	return filepath.ToSlash(clean), nil
}

func boundHeadlessCodemapSymbols(symbols []codemap.Symbol) ([]codemap.Symbol, bool) {
	if len(symbols) <= maxHeadlessCodemapResults {
		return symbols, false
	}
	return symbols[:maxHeadlessCodemapResults], true
}

func boundHeadlessCodemapContext(result codemap.ContextResult) (codemap.ContextResult, bool) {
	var truncated bool
	result.Definitions, truncated = boundHeadlessCodemapSymbols(result.Definitions)
	resultTruncated := truncated
	result.Callers, truncated = boundHeadlessCodemapSymbols(result.Callers)
	resultTruncated = resultTruncated || truncated
	result.Callees, truncated = boundHeadlessCodemapSymbols(result.Callees)
	resultTruncated = resultTruncated || truncated
	result.References, truncated = boundHeadlessCodemapSymbols(result.References)
	resultTruncated = resultTruncated || truncated
	result.Tests, truncated = boundHeadlessCodemapSymbols(result.Tests)
	return result, resultTruncated || truncated
}

func boundHeadlessCodemapImpact(result codemap.ImpactResult) (codemap.ImpactResult, bool) {
	var truncated bool
	result.Locations, truncated = boundHeadlessCodemapSymbols(result.Locations)
	resultTruncated := truncated
	result.DirectCallers, truncated = boundHeadlessCodemapSymbols(result.DirectCallers)
	resultTruncated = resultTruncated || truncated
	if len(result.BlastRadius) > maxHeadlessCodemapResults {
		result.BlastRadius = result.BlastRadius[:maxHeadlessCodemapResults]
		resultTruncated = true
	}
	result.Tests, truncated = boundHeadlessCodemapSymbols(result.Tests)
	resultTruncated = resultTruncated || truncated
	if len(result.TestCommands) > maxHeadlessCodemapResults {
		result.TestCommands = result.TestCommands[:maxHeadlessCodemapResults]
		resultTruncated = true
	}
	return result, resultTruncated
}

func runHeadlessExecContext(parentCtx context.Context, args []string, stdout, stderr io.Writer) int {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	var opts headlessOptions
	confirm := false
	commandArgs := make([]string, 0)
	separator := false
	for i := 0; i < len(args); i++ {
		if separator {
			commandArgs = append(commandArgs, args[i])
			continue
		}
		switch args[i] {
		case "--":
			separator = true
		case "--confirm":
			confirm = true
		case "--json":
			opts.json = true
		case "--root":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return writeHeadlessError(stderr, fmt.Errorf("--root requires a directory"))
			}
			opts.root = args[i+1]
			i++
		default:
			return writeHeadlessError(stderr, fmt.Errorf("headless exec options must appear before --; got %q", args[i]))
		}
	}
	if !confirm {
		return writeHeadlessError(stderr, fmt.Errorf("headless exec requires --confirm"))
	}
	if len(commandArgs) == 0 || strings.TrimSpace(commandArgs[0]) == "" {
		return writeHeadlessError(stderr, fmt.Errorf("headless exec requires a command after --"))
	}
	argumentBytes := 0
	for _, argument := range commandArgs {
		if len(argument) > headlessMaxCommandArgsBytes-argumentBytes-1 {
			return writeHeadlessError(stderr, fmt.Errorf("command arguments exceed %d bytes", headlessMaxCommandArgsBytes))
		}
		argumentBytes += len(argument) + 1
	}
	root, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}

	ctx, cancel := context.WithTimeout(parentCtx, headlessCommandTimeout)
	defer cancel()
	cmd, err := toolpath.Command(ctx, commandArgs[0], commandArgs[1:]...)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	toolpath.ConfigureCommand(cmd)
	cmd.Dir = root
	stdoutBuffer := &headlessOutputBuffer{limit: headlessMaxCommandOutput, onLimit: cancel}
	stderrBuffer := &headlessOutputBuffer{limit: headlessMaxCommandOutput, onLimit: cancel}
	cmd.Stdout = stdoutBuffer
	cmd.Stderr = stderrBuffer
	runErr := cmd.Run()
	response := headlessExecResponse{
		Workspace: root,
		Command:   commandArgs[0],
		Args:      append([]string(nil), commandArgs[1:]...),
		State:     "completed",
		ExitCode:  0,
		Stdout:    stdoutBuffer.String(),
		Stderr:    stderrBuffer.String(),
		Truncated: stdoutBuffer.truncated || stderrBuffer.truncated,
	}
	if runErr != nil {
		response.State, response.ExitCode = classifyHeadlessProcessFailure(ctx, runErr)
		response.Detail = runErr.Error()
	}
	if stdoutBuffer.truncated || stderrBuffer.truncated {
		response.State = "failed"
		response.ExitCode = -1
		response.Detail = fmt.Sprintf("command output exceeds %d-byte stream limit", headlessMaxCommandOutput)
	}
	if opts.json {
		if code := writeHeadlessJSON(stdout, response); code != 0 {
			return code
		}
	} else {
		var body strings.Builder
		fmt.Fprintf(&body, "Workspace: %s\nCommand: %s %s\nState: %s\nExit code: %d\n", root, response.Command, strings.Join(response.Args, " "), response.State, response.ExitCode)
		if response.Stdout != "" {
			body.WriteString(response.Stdout)
		}
		if response.Stderr != "" {
			body.WriteString(response.Stderr)
		}
		if response.Detail != "" {
			fmt.Fprintln(&body, response.Detail)
		}
		if code := writeHeadlessText(stdout, stderr, body.String()); code != 0 {
			return code
		}
	}
	if response.State != "completed" {
		return 1
	}
	return 0
}

// classifyHeadlessProcessFailure gives deadline state precedence over the
// process error returned by os/exec. On Unix, killing a timed-out process
// commonly yields *exec.ExitError, which must not hide the machine-actionable
// timed_out state from Glyphrun or an agent supervisor.
func classifyHeadlessProcessFailure(ctx context.Context, runErr error) (state string, exitCode int) {
	if ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timed_out", -1
	}
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled", -1
	}
	state = "failed"
	exitCode = -1
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		exitCode = exitErr.ExitCode()
	}
	return state, exitCode
}

type headlessOutputBuffer struct {
	bytes.Buffer
	limit         int
	truncated     bool
	onLimit       func()
	limitNotified bool
}

func (b *headlessOutputBuffer) Write(p []byte) (int, error) {
	if b.Len() >= b.limit {
		b.markTruncated()
		return len(p), nil
	}
	remaining := b.limit - b.Len()
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		b.markTruncated()
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func (b *headlessOutputBuffer) WriteString(s string) (int, error) {
	return b.Write([]byte(s))
}

// ReadFrom overrides the ReadFrom method promoted by bytes.Buffer. Without
// this override io.Copy would bypass Write entirely and an external command
// could exceed the headless output bound in one pipe copy.
func (b *headlessOutputBuffer) ReadFrom(r io.Reader) (int64, error) {
	remaining := int64(b.limit - b.Len())
	if remaining < 0 {
		remaining = 0
	}
	// Read one byte past the inclusive limit so an output that ends exactly
	// at the limit is accepted, while real overflow is still detected without
	// allowing the extra byte to remain in the response.
	n, err := b.Buffer.ReadFrom(io.LimitReader(r, remaining+1))
	if n > remaining {
		b.Truncate(b.limit)
		b.markTruncated()
	}
	return n, err
}

func (b *headlessOutputBuffer) markTruncated() {
	if b.truncated {
		return
	}
	b.truncated = true
	if !b.limitNotified {
		b.limitNotified = true
		if b.onLimit != nil {
			b.onLimit()
		}
	}
}

func collectHeadlessContext(root string) (headlessContextResponse, error) {
	return collectHeadlessContextDepthContext(context.Background(), root, 0)
}

type headlessContextDirectory struct {
	path     string
	relative string
	depth    int
}

func collectHeadlessContextDepth(root string, maxDepth int) (headlessContextResponse, error) {
	return collectHeadlessContextDepthContext(context.Background(), root, maxDepth)
}

func collectHeadlessContextDepthContext(ctx context.Context, root string, maxDepth int) (headlessContextResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxDepth < 0 || maxDepth > headlessMaxContextDepth {
		return headlessContextResponse{}, fmt.Errorf("context depth must be between 0 and %d", headlessMaxContextDepth)
	}
	entries := make([]headlessContextEntry, 0, minInt(headlessMaxContextEntries, 64))
	queue := []headlessContextDirectory{{path: root, depth: 0}}
	truncated := false
	visitedDirs := 0
	for len(queue) > 0 && len(entries) < headlessMaxContextEntries {
		if err := ctx.Err(); err != nil {
			return headlessContextResponse{}, err
		}
		directory := queue[0]
		queue = queue[1:]
		visitedDirs++
		remaining := headlessMaxContextEntries - len(entries)
		children, childTruncated, err := readHeadlessDirEntries(directory.path, remaining)
		if err != nil {
			return headlessContextResponse{}, fmt.Errorf("read workspace directory %q: %w", directory.path, err)
		}
		if err := ctx.Err(); err != nil {
			return headlessContextResponse{}, err
		}
		truncated = truncated || childTruncated
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, entry := range children {
			if err := ctx.Err(); err != nil {
				return headlessContextResponse{}, err
			}
			if len(entries) >= headlessMaxContextEntries {
				truncated = true
				break
			}
			relative := entry.Name()
			if directory.relative != "" {
				relative = filepath.Join(directory.relative, entry.Name())
			}
			relative = filepath.ToSlash(relative)
			kind := "other"
			bytes := int64(0)
			isSymlink := entry.Type()&os.ModeSymlink != 0
			if isSymlink {
				kind = "symlink"
			} else if entry.IsDir() {
				kind = "directory"
			} else if info, infoErr := entry.Info(); infoErr == nil && info.Mode().IsRegular() {
				kind = "file"
				bytes = info.Size()
			}
			entries = append(entries, headlessContextEntry{
				Name:  entry.Name(),
				Path:  relative,
				Kind:  kind,
				Bytes: bytes,
			})
			if maxDepth > directory.depth && !isSymlink && entry.IsDir() {
				if visitedDirs+len(queue) >= headlessMaxContextDirs {
					truncated = true
					continue
				}
				queue = append(queue, headlessContextDirectory{
					path:     filepath.Join(directory.path, entry.Name()),
					relative: relative,
					depth:    directory.depth + 1,
				})
			}
		}
	}
	if len(queue) > 0 {
		truncated = true
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	response := headlessContextResponse{
		Workspace:   root,
		ProjectRoot: findProjectRoot(root),
		Entries:     make([]headlessContextEntry, 0, minInt(len(entries), headlessMaxContextEntries)),
		Truncated:   truncated,
	}
	if err := ctx.Err(); err != nil {
		return headlessContextResponse{}, err
	}
	response.Entries = append(response.Entries, entries...)
	return response, nil
}

type headlessDirEntryHeap []os.DirEntry

func (h headlessDirEntryHeap) Len() int           { return len(h) }
func (h headlessDirEntryHeap) Less(i, j int) bool { return h[i].Name() > h[j].Name() }
func (h headlessDirEntryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *headlessDirEntryHeap) Push(value any)    { *h = append(*h, value.(os.DirEntry)) }
func (h *headlessDirEntryHeap) Pop() any {
	old := *h
	n := len(old)
	value := old[n-1]
	*h = old[:n-1]
	return value
}

// readHeadlessDirEntries scans a directory incrementally while retaining only
// the lexicographically smallest bounded result set. os.ReadDir(path) loads
// every entry before the caller can enforce that bound, which is a poor fit
// for workspaces containing generated or dependency-heavy folders.
func readHeadlessDirEntries(root string, limit int) ([]os.DirEntry, bool, error) {
	if limit <= 0 {
		return nil, false, nil
	}
	dir, err := os.Open(root)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = dir.Close() }()

	entries := make(headlessDirEntryHeap, 0, limit)
	heap.Init(&entries)
	truncated := false
	for {
		batchSize := 256
		batch, readErr := dir.ReadDir(batchSize)
		for _, entry := range batch {
			if entries.Len() < limit {
				heap.Push(&entries, entry)
				continue
			}
			truncated = true
			if entry.Name() < entries[0].Name() {
				entries[0] = entry
				heap.Fix(&entries, 0)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, false, readErr
		}
		if len(batch) == 0 {
			break
		}
	}
	return []os.DirEntry(entries), truncated, nil
}

func collectHeadlessSession(root string) headlessSessionResponse {
	return collectHeadlessSessionContext(context.Background(), root)
}

func collectHeadlessSessionContext(ctx context.Context, root string) headlessSessionResponse {
	return collectHeadlessSessionByNameContext(ctx, root, "")
}

func collectHeadlessSessionByNameContext(ctx context.Context, root, name string) headlessSessionResponse {
	if ctx == nil {
		ctx = context.Background()
	}
	path := session.PathForRoot(root)
	if name != "" {
		if namedPath, err := session.NamedPath(root, name); err == nil {
			path = namedPath
		}
	}
	response := headlessSessionResponse{
		Workspace: root,
		Path:      path,
		Name:      name,
		State:     "missing",
	}
	var state session.State
	var err error
	if name == "" {
		state, err = session.LoadContextForRoot(ctx, root)
	} else {
		state, err = session.LoadNamed(ctx, root, name)
	}
	if err != nil {
		if ctx.Err() != nil {
			response.State = "cancelled"
			response.Detail = ctx.Err().Error()
			return response
		}
		if os.IsNotExist(err) {
			response.Detail = "no saved session"
			return response
		}
		response.State = "invalid"
		response.Detail = err.Error()
		return response
	}
	response.State = "present"
	response.Session = &state
	return response
}

func effectiveHeadlessLSPConfigs() ([]lsp.ServerConfig, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return lspConfigsFromConfig(cfg), nil
}

func headlessLSPConfigForFile(path string) (lsp.ServerConfig, bool, error) {
	configs, err := effectiveHeadlessLSPConfigs()
	if err != nil {
		return lsp.ServerConfig{}, false, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	for _, server := range configs {
		for _, candidate := range server.Extensions {
			if strings.ToLower(candidate) == ext {
				return server, true, nil
			}
		}
	}
	return lsp.ServerConfig{}, false, nil
}

func collectHeadlessLSPStatus(root string) (headlessLSPResponse, error) {
	return collectHeadlessLSPStatusContext(context.Background(), root)
}

func collectHeadlessLSPStatusContext(parentCtx context.Context, root string) (headlessLSPResponse, error) {
	return collectHeadlessLSPStatusContextWithProbe(parentCtx, root, false)
}

func collectHeadlessLSPStatusWithProbe(root string) (headlessLSPResponse, error) {
	return collectHeadlessLSPStatusContextWithProbe(context.Background(), root, true)
}

func collectHeadlessLSPStatusContextWithProbe(parentCtx context.Context, root string, protocolProbeRequested bool) (headlessLSPResponse, error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	configs, err := effectiveHeadlessLSPConfigs()
	if err != nil {
		return headlessLSPResponse{}, err
	}
	response := headlessLSPResponse{
		Workspace: root,
		Servers:   make([]headlessLSPEntry, 0, len(configs)),
	}
	// Status is a bounded onboarding probe, not an interactive LSP request.
	// Keep the shared deadline below the contract tested by callers so two
	// unresponsive servers complete together instead of serially consuming the
	// longer operation timeout.
	probeCtx, cancel := context.WithTimeout(parentCtx, 3*time.Second)
	defer cancel()
	entries := make([]headlessLSPEntry, len(configs))
	statusProbes := make([]headlessLSPStatusProbe, 0, len(configs))
	probeIndexes := make(map[string]int, len(configs))
	resolver := toolpath.Default()
	for index, server := range configs {
		entry := headlessLSPEntry{
			LanguageID: server.LanguageID,
			Extensions: append([]string(nil), server.Extensions...),
			Command:    server.Command,
			State:      "missing",
		}
		path, resolveErr := toolpath.Resolve(server.Command)
		if resolveErr != nil {
			entry.Hint = toolpath.Hint(server.Command)
			if missing, ok := resolveErr.(*toolpath.MissingToolError); ok {
				entry.Hint = missing.Hint
			}
		} else {
			entry.Available = true
			entry.State = "available"
			entry.Path = path
			hasVersionProbe := resolver.HasVersionProbe(server.Command)
			if !hasVersionProbe && !protocolProbeRequested {
				entry.State = "unsupported"
				entry.VersionProbe = "unsupported"
				entry.Hint = "no safe non-interactive version probe is configured; use --probe for an LSP handshake"
				entries[index] = entry
				continue
			}
			protocol := protocolProbeRequested && !hasVersionProbe
			if protocol {
				entry.VersionProbe = "unsupported"
				entry.ProtocolProbe = "pending"
			}
			key := headlessLSPStatusProbeKey(server, protocol)
			if _, seen := probeIndexes[key]; !seen {
				probeIndexes[key] = len(statusProbes)
				statusProbes = append(statusProbes, headlessLSPStatusProbe{
					Key:      key,
					Command:  server.Command,
					Server:   server,
					Protocol: protocol,
				})
			}
		}
		entries[index] = entry
	}

	probeResults := make(chan headlessLSPStatusProbeResult, len(statusProbes))
	var probeWait sync.WaitGroup
	probeWait.Add(len(statusProbes))
	for _, probe := range statusProbes {
		go func(probe headlessLSPStatusProbe) {
			defer probeWait.Done()
			result := headlessLSPStatusProbeResult{Key: probe.Key}
			if probe.Protocol {
				probeResult, probeErr := lsp.ProbeProtocolCapabilities(probeCtx, probe.Server, root)
				result.ProtocolState = headlessLSPProtocolProbeState(probeCtx, probeErr)
				result.CapabilityState = result.ProtocolState
				result.Capabilities = append([]string(nil), probeResult.Capabilities...)
				result.Err = probeErr
			} else {
				version, state, probeErr := probeHeadlessToolVersion(probeCtx, probe.Command, probe.Server.Env)
				result.Version = version
				result.VersionState = state
				result.Err = probeErr
			}
			probeResults <- result
		}(probe)
	}
	go func() {
		probeWait.Wait()
		close(probeResults)
	}()
	probesByKey := make(map[string]headlessLSPStatusProbeResult, len(statusProbes))
	for result := range probeResults {
		probesByKey[result.Key] = result
	}

	for index := range entries {
		entry := entries[index]
		if entry.Available {
			protocol := !resolver.HasVersionProbe(configs[index].Command)
			if protocol && !protocolProbeRequested {
				response.Servers = append(response.Servers, entry)
				continue
			}
			key := headlessLSPStatusProbeKey(configs[index], protocol)
			probe := probesByKey[key]
			if protocol {
				entry.ProtocolProbe = probe.ProtocolState
				entry.CapabilityProbe = probe.CapabilityState
				entry.Capabilities = append([]string(nil), probe.Capabilities...)
				if probe.Err != nil {
					entry.State = headlessLSPProtocolProbeState(probeCtx, probe.Err)
					switch entry.State {
					case "timed_out":
						entry.Hint = "LSP protocol probe timed out; verify the server is responsive"
					case "cancelled":
						entry.Hint = "LSP protocol probe was cancelled"
					default:
						entry.Hint = "LSP protocol probe failed: " + probe.Err.Error()
					}
				} else if probe.ProtocolState == "ready" {
					entry.Ready = true
				}
			} else {
				entry.Version = probe.Version
				entry.VersionProbe = probe.VersionState
				if probe.Err != nil {
					switch probe.VersionState {
					case "timed_out":
						entry.State = "timed_out"
						entry.Hint = "version probe timed out; verify the executable is responsive"
					case "cancelled":
						entry.State = "cancelled"
						entry.Hint = "version probe was cancelled"
					default:
						entry.State = "failed"
						entry.Hint = "version probe failed: " + probe.Err.Error()
					}
				} else if probe.VersionState == "ready" {
					entry.Ready = true
				}
			}
		}
		response.Servers = append(response.Servers, entry)
	}
	return response, nil
}

type headlessLSPStatusProbe struct {
	Key      string
	Command  string
	Server   lsp.ServerConfig
	Protocol bool
}

type headlessLSPStatusProbeResult struct {
	Key             string
	Version         string
	VersionState    string
	ProtocolState   string
	CapabilityState string
	Capabilities    []string
	Err             error
}

func headlessLSPStatusProbeKey(server lsp.ServerConfig, protocol bool) string {
	return doctorLanguageProbeKey(server, protocol)
}

func headlessLSPProtocolProbeState(ctx context.Context, err error) string {
	if err == nil {
		return "ready"
	}
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	if ctx != nil && ctx.Err() != nil {
		return "timed_out"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed_out"
	}
	return "failed"
}

func headlessLSPContextState(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "timed_out"
}

func headlessLSPStageDetail(operation, state string) string {
	if state == "cancelled" {
		return operation + " was cancelled"
	}
	return operation + " exceeded the headless timeout"
}

// collectHeadlessHealthLSPStatusContext limits health to language servers that are
// relevant to files in the workspace. The regular `lsp status` command is
// intentionally a complete configuration inventory, but using that inventory
// for project health would mark a Go-only project unhealthy because optional
// Rust, Java, or Lua servers are not installed. Doctor's bounded language
// detector already provides the right evidence and protocol fallback for this
// health-specific view.
func collectHeadlessHealthLSPStatusContext(ctx context.Context, root string) (headlessLSPResponse, *doctorLanguageScan, error) {
	configs, err := effectiveHeadlessLSPConfigs()
	if err != nil {
		return headlessLSPResponse{}, nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, doctorScanTimeout)
	defer cancel()
	languages, scan := detectDoctorLanguagesContext(probeCtx, root, configs, toolpath.Default())
	response := headlessLSPResponse{
		Workspace:    root,
		Servers:      make([]headlessLSPEntry, 0, len(languages)),
		LanguageScan: scan,
	}
	for _, language := range languages {
		entry := headlessLSPEntry{
			LanguageID:    language.LanguageID,
			Extensions:    append([]string(nil), language.Extensions...),
			Command:       language.Server,
			DetectedFiles: language.Files,
			Available:     language.State == "available",
			Ready: language.State == "available" &&
				(language.VersionProbe == "ready" || language.ProtocolProbe == "ready"),
			State:           language.State,
			Path:            language.Path,
			Version:         language.Version,
			VersionProbe:    language.VersionProbe,
			ProtocolProbe:   language.ProtocolProbe,
			CapabilityProbe: language.CapabilityProbe,
			Capabilities:    append([]string(nil), language.Capabilities...),
			Hint:            language.Hint,
		}
		response.Servers = append(response.Servers, entry)
	}
	return response, scan, nil
}

const headlessLSPOperationTimeout = 5 * time.Second

// runHeadlessLSPStage puts a wall-clock bound around LSP methods whose public
// API predates context-aware variants. The client is shut down by the caller
// when this reports a timeout; the buffered result channel lets the worker
// finish without leaking a goroutine if the transport returns later.
func runHeadlessLSPStage(timeout time.Duration, operation func() error) (error, bool) {
	return runHeadlessLSPStageContext(context.Background(), timeout, operation)
}

func runHeadlessLSPStageContext(ctx context.Context, timeout time.Duration, operation func() error) (error, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err, true
	}
	if timeout <= 0 {
		return context.DeadlineExceeded, true
	}
	done := make(chan error, 1)
	go func() { done <- operation() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err, false
	case <-ctx.Done():
		return ctx.Err(), true
	case <-timer.C:
		return context.DeadlineExceeded, true
	}
}

func collectHeadlessLSPDiagnostics(root, path string) (headlessLSPDiagnosticsResponse, error) {
	return collectHeadlessLSPDiagnosticsContext(context.Background(), root, path)
}

func collectHeadlessLSPDiagnosticsContext(parentCtx context.Context, root, path string) (headlessLSPDiagnosticsResponse, error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	operationCtx, cancel := context.WithTimeout(parentCtx, headlessLSPOperationTimeout)
	defer cancel()
	buffer, err := readHeadlessBufferContext(operationCtx, root, path)
	if err != nil {
		return headlessLSPDiagnosticsResponse{}, err
	}
	server, configured, err := headlessLSPConfigForFile(path)
	if err != nil {
		return headlessLSPDiagnosticsResponse{}, err
	}
	response := headlessLSPDiagnosticsResponse{
		Workspace:    root,
		Path:         path,
		RelativePath: buffer.RelativePath,
		Diagnostics:  make([]headlessDiagnostic, 0),
		State:        "unsupported",
		Detail:       "no language server is configured for this file type",
	}
	if !configured {
		return response, nil
	}
	response.LanguageID = server.LanguageID
	response.Server = server.Command
	if _, err := toolpath.Resolve(server.Command); err != nil {
		response.State = "missing"
		response.Detail = "language server is not installed"
		if missing, ok := err.(*toolpath.MissingToolError); ok {
			response.Hint = missing.Hint
		}
		return response, nil
	}

	messages := make(chan any, 32)
	client, err := lsp.NewClient(server, root, messages)
	if err != nil {
		response.State = "failed"
		response.Detail = fmt.Sprintf("start language server: %v", err)
		return response, nil
	}
	defer func() {
		client.Shutdown()
		if operationCtx.Err() != nil {
			client.Terminate()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = client.WaitForShutdown(ctx)
		cancel()
	}()
	deadline := time.Now().Add(headlessLSPOperationTimeout)
	if err, timedOut := runHeadlessLSPStageContext(operationCtx, time.Until(deadline), client.Initialize); timedOut {
		client.Shutdown()
		response.State = headlessLSPContextState(err)
		response.Detail = headlessLSPStageDetail("language server initialization", response.State)
		return response, nil
	} else if err != nil {
		response.State = "failed"
		response.Detail = fmt.Sprintf("initialize language server: %v", err)
		return response, nil
	}

	uri := lsp.FileURI(path)
	client.DidOpen(uri, server.LanguageID, 1, buffer.Content)
	remaining := time.Until(deadline)
	if remaining <= 0 {
		response.State = "timed_out"
		response.Detail = "language server initialization exceeded the headless timeout"
		return response, nil
	}
	diagnosticsDeadline := time.NewTimer(remaining)
	defer diagnosticsDeadline.Stop()
	for {
		select {
		case message := <-messages:
			switch typed := message.(type) {
			case lsp.DiagnosticsMsg:
				if typed.URI != uri {
					continue
				}
				response.State = "ready"
				response.Detail = "diagnostics received"
				response.Diagnostics = headlessDiagnostics(typed.Diagnostics)
				return response, nil
			case lsp.ServerExitedMsg:
				response.State = "failed"
				response.Detail = "language server exited before publishing diagnostics"
				return response, nil
			}
		case <-diagnosticsDeadline.C:
			response.State = "timed_out"
			response.Detail = "language server initialized; no diagnostics were published before the headless timeout"
			return response, nil
		case <-operationCtx.Done():
			response.State = headlessLSPContextState(operationCtx.Err())
			response.Detail = headlessLSPStageDetail("language server diagnostics request", response.State)
			return response, nil
		}
	}
}

func collectHeadlessLSPFormat(root, path string) (headlessLSPFormatResponse, error) {
	return collectHeadlessLSPFormatContext(context.Background(), root, path)
}

func collectHeadlessLSPFormatContext(parentCtx context.Context, root, path string) (headlessLSPFormatResponse, error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	operationCtx, cancel := context.WithTimeout(parentCtx, headlessLSPOperationTimeout)
	defer cancel()
	buffer, err := readHeadlessBufferContext(operationCtx, root, path)
	if err != nil {
		return headlessLSPFormatResponse{}, err
	}
	server, configured, err := headlessLSPConfigForFile(path)
	if err != nil {
		return headlessLSPFormatResponse{}, err
	}
	response := headlessLSPFormatResponse{
		Workspace:    root,
		Path:         path,
		RelativePath: buffer.RelativePath,
		State:        "unsupported",
		InputSHA256:  buffer.SHA256,
		OutputSHA256: buffer.SHA256,
		Detail:       "no language server is configured for this file type",
	}
	if !configured {
		return response, nil
	}
	response.LanguageID = server.LanguageID
	response.Server = server.Command
	if _, err := toolpath.Resolve(server.Command); err != nil {
		response.State = "missing"
		response.Detail = "language server is not installed"
		if missing, ok := err.(*toolpath.MissingToolError); ok {
			response.Hint = missing.Hint
		}
		return response, nil
	}

	messages := make(chan any, 32)
	client, err := lsp.NewClient(server, root, messages)
	if err != nil {
		response.State = "failed"
		response.Detail = fmt.Sprintf("start language server: %v", err)
		return response, nil
	}
	defer func() {
		client.Shutdown()
		if operationCtx.Err() != nil {
			client.Terminate()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = client.WaitForShutdown(ctx)
		cancel()
	}()
	deadline := time.Now().Add(headlessLSPOperationTimeout)
	if err, timedOut := runHeadlessLSPStageContext(operationCtx, time.Until(deadline), client.Initialize); timedOut {
		client.Shutdown()
		response.State = headlessLSPContextState(err)
		response.Detail = headlessLSPStageDetail("language server initialization", response.State)
		return response, nil
	} else if err != nil {
		response.State = "failed"
		response.Detail = fmt.Sprintf("initialize language server: %v", err)
		return response, nil
	}
	if !client.SupportsFormatting() {
		response.State = "unsupported"
		response.Detail = "language server does not support document formatting"
		return response, nil
	}

	uri := lsp.FileURI(path)
	client.DidOpen(uri, server.LanguageID, 1, buffer.Content)
	var edits []lsp.TextEdit
	remaining := time.Until(deadline)
	err, timedOut := runHeadlessLSPStageContext(operationCtx, remaining, func() error {
		var err error
		edits, err = client.FormattingContext(operationCtx, uri, lsp.FormattingOptions{TabSize: 4, InsertSpaces: true})
		return err
	})
	if timedOut {
		client.Shutdown()
		response.State = headlessLSPContextState(err)
		response.Detail = headlessLSPStageDetail("language server formatting", response.State)
		return response, nil
	}
	if err != nil {
		response.State = "failed"
		response.Detail = fmt.Sprintf("formatting request failed: %v", err)
		return response, nil
	}
	formatted, err := applyHeadlessTextEdits(buffer.Content, edits)
	if err != nil {
		response.State = "failed"
		response.Detail = fmt.Sprintf("invalid formatting edits: %v", err)
		return response, nil
	}
	response.State = "ready"
	response.Edits = len(edits)
	response.Content = formatted
	response.Changed = formatted != buffer.Content
	outputSum := sha256.Sum256([]byte(formatted))
	response.OutputSHA256 = hex.EncodeToString(outputSum[:])
	if response.Changed {
		response.Detail = "formatting preview ready"
	} else {
		response.Detail = "no formatting changes"
	}
	return response, nil
}

func applyHeadlessTextEdits(content string, edits []lsp.TextEdit) (string, error) {
	if len(edits) > 16_384 {
		return "", fmt.Errorf("formatting result exceeds 16384 text edits")
	}
	rope := text.New([]byte(content))
	type positionedEdit struct {
		edit       lsp.TextEdit
		start, end int
	}
	positioned := make([]positionedEdit, 0, len(edits))
	newBytes := 0
	for _, edit := range edits {
		if len(edit.NewText) > headlessMaxBufferBytes-newBytes {
			return "", fmt.Errorf("formatting result exceeds %d bytes of replacement text", headlessMaxBufferBytes)
		}
		newBytes += len(edit.NewText)
		start, err := headlessTextEditOffset(rope, edit.StartLine, edit.StartCol)
		if err != nil {
			return "", fmt.Errorf("invalid edit start: %w", err)
		}
		end, err := headlessTextEditOffset(rope, edit.EndLine, edit.EndCol)
		if err != nil {
			return "", fmt.Errorf("invalid edit end: %w", err)
		}
		if end < start {
			return "", fmt.Errorf("edit range ends before it starts")
		}
		positioned = append(positioned, positionedEdit{edit: edit, start: start, end: end})
	}
	sort.Slice(positioned, func(i, j int) bool {
		if positioned[i].start != positioned[j].start {
			return positioned[i].start < positioned[j].start
		}
		return positioned[i].end < positioned[j].end
	})
	for i := 1; i < len(positioned); i++ {
		if positioned[i].start < positioned[i-1].end ||
			(positioned[i].start == positioned[i-1].start && positioned[i].end == positioned[i-1].end) {
			return "", fmt.Errorf("overlapping or ambiguous edits")
		}
	}
	for i := len(positioned) - 1; i >= 0; i-- {
		edit := positioned[i]
		rope = rope.Delete(edit.start, edit.end-edit.start).Insert(edit.start, []byte(edit.edit.NewText))
		if rope.Len() > headlessMaxBufferBytes {
			return "", fmt.Errorf("formatted buffer exceeds %d-byte limit", headlessMaxBufferBytes)
		}
	}
	return rope.String(), nil
}

func headlessTextEditOffset(rope *text.Rope, line, column int) (int, error) {
	if line < 0 || line >= rope.LineCount() {
		return 0, fmt.Errorf("line %d is outside document", line)
	}
	lineBytes := rope.Line(line)
	if column < 0 || column > len(lineBytes) {
		return 0, fmt.Errorf("column %d is outside line %d", column, line)
	}
	if !utf8.Valid(lineBytes) {
		return 0, fmt.Errorf("line %d is not valid UTF-8", line)
	}
	if column < len(lineBytes) && !utf8.RuneStart(lineBytes[column]) {
		return 0, fmt.Errorf("column %d splits a UTF-8 sequence", column)
	}
	return rope.LineStart(line) + column, nil
}

func headlessDiagnostics(diagnostics []lsp.Diagnostic) []headlessDiagnostic {
	result := make([]headlessDiagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		result = append(result, headlessDiagnostic{
			Line:      diagnostic.Range.Start.Line,
			Column:    diagnostic.Range.Start.Character,
			EndLine:   diagnostic.Range.End.Line,
			EndColumn: diagnostic.Range.End.Character,
			Severity:  int(diagnostic.Severity),
			Message:   diagnostic.Message,
			Source:    diagnostic.Source,
		})
	}
	return result
}

// collectHeadlessDAPStatus reports adapter availability without starting a
// debug session. The current built-in launch contract is Go + Delve; exposing
// it here lets scripts and agents distinguish "debugging is supported" from
// "the adapter is not installed" without spawning a process.
func collectHeadlessDAPStatus(root string) headlessDAPResponse {
	return collectHeadlessDAPStatusContext(context.Background(), root)
}

func collectHeadlessDAPStatusContext(ctx context.Context, root string) headlessDAPResponse {
	if ctx == nil {
		ctx = context.Background()
	}
	config := dap.DefaultGoDebugConfig(filepath.Join(root, "main.go"))
	entry := headlessDAPEntry{
		Type:       config.Type,
		Extensions: []string{".go"},
		Command:    config.Command,
		State:      "missing",
	}
	if err := ctx.Err(); err != nil {
		entry.State = headlessDAPContextState(err)
		if entry.State == "cancelled" {
			entry.Hint = "DAP status probe was cancelled"
		} else {
			entry.Hint = "DAP status probe timed out"
		}
		return headlessDAPResponse{
			Workspace: root,
			Adapters:  []headlessDAPEntry{entry},
		}
	}
	path, err := toolpath.Resolve(config.Command)
	if err != nil {
		entry.Hint = toolpath.Hint(config.Command)
		if missing, ok := err.(*toolpath.MissingToolError); ok {
			entry.Hint = missing.Hint
		}
	} else {
		entry.Available = true
		entry.State = "available"
		entry.Path = path
		entry.Version, entry.VersionProbe, err = probeHeadlessToolVersion(ctx, config.Command, nil)
		if err != nil {
			switch entry.VersionProbe {
			case "cancelled":
				entry.State = "cancelled"
				entry.Hint = "version probe was cancelled"
			case "timed_out":
				entry.State = "timed_out"
				entry.Hint = "version probe timed out"
			default:
				entry.State = "failed"
				entry.Hint = "version probe failed: " + err.Error()
			}
		}
	}
	return headlessDAPResponse{
		Workspace: root,
		Adapters:  []headlessDAPEntry{entry},
	}
}

const headlessDAPProbeTimeout = 5 * time.Second

func collectHeadlessDAPProbe(root, adapter string, args []string) headlessDAPProbeResponse {
	return collectHeadlessDAPProbeContext(context.Background(), root, adapter, args)
}

func collectHeadlessDAPProbeContext(parentCtx context.Context, root, adapter string, args []string) headlessDAPProbeResponse {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	operationCtx, cancel := context.WithTimeout(parentCtx, headlessDAPProbeTimeout)
	defer cancel()
	response := headlessDAPProbeResponse{
		Workspace: root,
		Adapter:   adapter,
		Args:      append([]string(nil), args...),
		State:     "missing",
	}
	if err := parentCtx.Err(); err != nil {
		response.State = headlessDAPContextState(err)
		response.Detail = headlessDAPStageDetail("debug adapter initialization", response.State)
		return response
	}
	if _, err := toolpath.Resolve(adapter); err != nil {
		response.Detail = "debug adapter is not installed"
		if missing, ok := err.(*toolpath.MissingToolError); ok {
			response.Hint = missing.Hint
		}
		return response
	}

	client, err := dap.NewClient(adapter, args, make(chan any, 16))
	if err != nil {
		response.State = "failed"
		response.Detail = fmt.Sprintf("start debug adapter: %v", err)
		return response
	}
	defer func() {
		client.Shutdown()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = client.WaitForShutdown(ctx)
		cancel()
	}()

	err, timedOut := runHeadlessDAPStageContext(operationCtx, headlessDAPProbeTimeout, client.Initialize)
	if timedOut {
		response.State = headlessDAPContextState(err)
		response.Detail = headlessDAPStageDetail("debug adapter initialization", response.State)
		return response
	}
	if err != nil {
		response.State = "failed"
		response.Detail = fmt.Sprintf("initialize debug adapter: %v", err)
		return response
	}
	response.State = "ready"
	response.Ready = true
	response.Detail = "debug adapter initialized"
	return response
}

// runHeadlessDAPStage puts a wall-clock bound around DAP methods whose public
// API predates context-aware variants. The buffered result channel lets a
// transport that returns later finish without leaking a goroutine; the caller
// still shuts down the adapter after a timeout.
func runHeadlessDAPStage(timeout time.Duration, operation func() error) (error, bool) {
	return runHeadlessDAPStageContext(context.Background(), timeout, operation)
}

func runHeadlessDAPStageContext(ctx context.Context, timeout time.Duration, operation func() error) (error, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err, true
	}
	if timeout <= 0 {
		return context.DeadlineExceeded, true
	}
	done := make(chan error, 1)
	go func() { done <- operation() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err, false
	case <-ctx.Done():
		return ctx.Err(), true
	case <-timer.C:
		return context.DeadlineExceeded, true
	}
}

func headlessDAPContextState(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "timed_out"
}

func headlessDAPStageDetail(operation, state string) string {
	if state == "cancelled" {
		return operation + " was cancelled"
	}
	return fmt.Sprintf("%s exceeded %s", operation, headlessDAPProbeTimeout)
}

func collectHeadlessAgentRuns(root string) (headlessAgentResponse, error) {
	return collectHeadlessAgentRunsContext(context.Background(), root)
}

func collectHeadlessAgentRunsContext(ctx context.Context, root string) (headlessAgentResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return headlessAgentResponse{}, err
	}
	path := headlessAgentStorePath(root)
	manager, err := agentruntime.NewManager(agentruntime.ManagerConfig{
		Store:        agentruntime.FileStore{Path: path},
		SkipRecovery: true,
	})
	if err != nil {
		return headlessAgentResponse{}, fmt.Errorf("load agent runs: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return headlessAgentResponse{}, err
	}
	runs, err := manager.ListWithError()
	if err != nil {
		return headlessAgentResponse{}, fmt.Errorf("refresh agent runs: %w", err)
	}
	return headlessAgentResponse{
		Workspace: root,
		Path:      path,
		Runs:      runs,
	}, nil
}

func collectHeadlessAgentRun(root string, id agentruntime.RunID) (headlessAgentRunResponse, error) {
	return collectHeadlessAgentRunContext(context.Background(), root, id)
}

func collectHeadlessAgentRunContext(ctx context.Context, root string, id agentruntime.RunID) (headlessAgentRunResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return headlessAgentRunResponse{}, err
	}
	path := headlessAgentStorePath(root)
	manager, err := agentruntime.NewManager(agentruntime.ManagerConfig{
		Store:        agentruntime.FileStore{Path: path},
		SkipRecovery: true,
	})
	if err != nil {
		return headlessAgentRunResponse{}, fmt.Errorf("load agent runs: %w", err)
	}
	run, err := manager.Get(id)
	if err != nil {
		return headlessAgentRunResponse{}, fmt.Errorf("load agent run %q: %w", id, err)
	}
	if err := ctx.Err(); err != nil {
		return headlessAgentRunResponse{}, err
	}
	return headlessAgentRunResponse{
		Workspace: root,
		Path:      path,
		Run:       run,
	}, nil
}

func headlessAgentStorePath(root string) string {
	return agentruntime.ResolveWorkspaceStorePath(root, session.StateHome())
}

func collectHeadlessToolStatusContext(ctx context.Context, root string) headlessToolsResponse {
	response := headlessToolsResponse{
		Workspace: root,
		Tools:     make([]headlessToolStatus, 0, 3),
	}
	if ctx == nil {
		ctx = context.Background()
	}

	type result struct {
		index int
		value headlessToolStatus
	}
	results := make(chan result, 3)
	var wg sync.WaitGroup
	checks := []func() headlessToolStatus{
		func() headlessToolStatus { return collectCodemapStatus(ctx, root) },
		func() headlessToolStatus { return collectVecgrepStatus(ctx, root) },
		func() headlessToolStatus { return collectSimpleToolStatusContext(ctx, "hitspec") },
	}
	for index, check := range checks {
		wg.Add(1)
		go func(index int, check func() headlessToolStatus) {
			defer wg.Done()
			started := time.Now()
			value := check()
			value.DurationMS = float64(time.Since(started).Microseconds()) / 1000
			results <- result{index: index, value: value}
		}(index, check)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	ordered := make([]headlessToolStatus, len(checks))
	for result := range results {
		ordered[result.index] = result.value
	}
	response.Tools = append(response.Tools, ordered...)
	response.Metrics = collectHeadlessRuntimeMetrics(ctx)
	return response
}

func collectHeadlessRuntimeMetrics(ctx context.Context) headlessRuntimeMetrics {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	metrics := headlessRuntimeMetrics{
		HeapAllocBytes: memory.HeapAlloc,
		HeapSysBytes:   memory.HeapSys,
	}
	if info, err := procmon.Current(ctx); err == nil {
		metrics.RSSBytes = info.RSSBytes
		metrics.RSSAvailable = true
	}
	return metrics
}

func collectCodemapStatus(ctx context.Context, root string) headlessToolStatus {
	status := headlessToolStatus{Name: "codemap", Mode: "structural-manifest"}
	path, err := toolpath.Resolve("codemap")
	if err != nil {
		return unavailableToolStatus(status, err)
	}
	status.Path = path
	status.Available = true
	status = annotateHeadlessToolVersion(ctx, status)
	if status.VersionProbe == "failed" || status.VersionProbe == "timed_out" || status.VersionProbe == "cancelled" {
		return finalizeHeadlessToolStatus(status)
	}
	status.Capability = "structural-manifest"
	status.CapabilityProbe = "pending"
	result, err := codemap.StructuralManifest(ctx, root)
	if err != nil {
		if errors.Is(err, codemap.ErrStructuralManifestUnsupported) {
			status.State = "unsupported"
			status.CapabilityProbe = "unsupported"
			status.Detail = err.Error()
			status.Hint = "upgrade codemap to a release with structural-manifest"
			return finalizeHeadlessToolStatus(status)
		}
		status.CapabilityProbe = headlessCapabilityProbeState(ctx, err)
		status.State = toolFailureStateContext(ctx, err)
		status.Detail = err.Error()
		return finalizeHeadlessToolStatus(status)
	}
	status.Ready = result.Ready()
	status.CapabilityProbe = "ready"
	status.Records = result.TotalRecords
	if status.Ready {
		status.State = "ready"
		status.Detail = fmt.Sprintf("index is current (%d records)", result.TotalRecords)
		return finalizeHeadlessToolStatus(status)
	}
	status.State = "stale"
	status.Detail = fmt.Sprintf("records=%d changed=%d new=%d deleted=%d", result.TotalRecords,
		result.Freshness.Changed, result.Freshness.New, result.Freshness.Deleted)
	return finalizeHeadlessToolStatus(status)
}

func collectVecgrepStatus(ctx context.Context, root string) headlessToolStatus {
	status := headlessToolStatus{Name: "vecgrep", Mode: "lightweight-status"}
	path, err := toolpath.Resolve("vecgrep")
	if err != nil {
		return unavailableToolStatus(status, err)
	}
	status.Path = path
	status.Available = true
	status = annotateHeadlessToolVersion(ctx, status)
	if status.VersionProbe == "failed" || status.VersionProbe == "timed_out" || status.VersionProbe == "cancelled" {
		return finalizeHeadlessToolStatus(status)
	}
	status.Capability = "lightweight-status"
	status.CapabilityProbe = "pending"
	result, err := search.VecgrepLightweightStatusContext(ctx, root)
	if err != nil {
		if errors.Is(err, search.ErrVecgrepLightweightUnsupported) {
			status.State = "unsupported"
			status.CapabilityProbe = "unsupported"
			status.Detail = err.Error()
			status.Hint = "upgrade vecgrep to a release with status --lightweight"
			return finalizeHeadlessToolStatus(status)
		}
		status.CapabilityProbe = headlessCapabilityProbeState(ctx, err)
		status.State = toolFailureStateContext(ctx, err)
		status.Detail = err.Error()
		return finalizeHeadlessToolStatus(status)
	}
	status.Ready = result.Ready()
	status.CapabilityProbe = "ready"
	status.Files = result.Files
	status.PendingChanges = result.PendingChanges
	if status.Ready {
		status.State = "ready"
		status.Detail = "index is current"
	} else if !result.FreshnessKnown {
		status.State = "unknown"
		status.Detail = fmt.Sprintf("freshness unknown (files=%d pending=%d)", result.Files, result.PendingChanges)
	} else {
		status.State = "stale"
		status.Detail = fmt.Sprintf("fresh=%t pending=%d", result.IndexFresh, result.PendingChanges)
	}
	return finalizeHeadlessToolStatus(status)
}

func collectSimpleToolStatus(name string) headlessToolStatus {
	return collectSimpleToolStatusContext(context.Background(), name)
}

func collectSimpleToolStatusContext(ctx context.Context, name string) headlessToolStatus {
	status := headlessToolStatus{Name: name, Mode: "version-probe"}
	path, err := toolpath.Resolve(name)
	if err != nil {
		return unavailableToolStatus(status, err)
	}
	status.Available = true
	status.Ready = true
	status.State = "available"
	status.Detail = "tool resolved"
	status.Path = path
	status = annotateHeadlessToolVersion(ctx, status)
	if status.VersionProbe == "failed" || status.VersionProbe == "timed_out" || status.VersionProbe == "cancelled" {
		return finalizeHeadlessToolStatus(status)
	}
	if status.VersionProbe == "unsupported" {
		status.Ready = false
		status.State = "unsupported"
		status.Detail = "no safe non-interactive version probe is configured"
		status.Hint = "configure a supported tool probe before relying on this capability"
		return finalizeHeadlessToolStatus(status)
	}
	if name == "hitspec" {
		status.Mode = "validate-contract"
		status.Capability = "validate"
		status.CapabilityProbe = "pending"
		capability, capabilityErr := probeDoctorCapability(ctx, toolpath.Default(), name)
		if capabilityErr != nil {
			capabilityState := headlessCapabilityProbeState(ctx, capabilityErr)
			status.CapabilityProbe = capabilityState
			status.Ready = false
			switch capabilityState {
			case "timed_out":
				status.State = "timed_out"
			case "cancelled":
				status.State = "cancelled"
			default:
				status.State = "unsupported"
			}
			status.Detail = "hitspec validate capability probe failed: " + capabilityErr.Error()
			status.Hint = doctorCapabilityHint(name)
			return finalizeHeadlessToolStatus(status)
		}
		status.Capability = capability
		status.CapabilityProbe = "ready"
	}
	return finalizeHeadlessToolStatus(status)
}

func headlessCapabilityProbeState(ctx context.Context, err error) string {
	if ctx != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
			return "cancelled"
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return "timed_out"
		}
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed_out"
	}
	return "failed"
}

func annotateHeadlessToolVersion(ctx context.Context, status headlessToolStatus) headlessToolStatus {
	if !status.Available {
		return status
	}
	version, probeState, err := probeHeadlessToolVersion(ctx, status.Name, nil)
	status.VersionProbe = probeState
	if err != nil {
		if probeState == "cancelled" || (ctx != nil && errors.Is(ctx.Err(), context.Canceled)) {
			status.Ready = false
			status.State = "cancelled"
			status.VersionProbe = "cancelled"
			status.Detail = "version probe was cancelled"
			return status
		}
		if probeState == "timed_out" || (ctx != nil && ctx.Err() != nil) {
			status.Ready = false
			status.State = "timed_out"
			status.VersionProbe = "timed_out"
			status.Detail = "version probe timed out"
			return status
		}
		if status.Detail == "" {
			status.Detail = "version probe failed: " + err.Error()
		} else {
			status.Detail += "; version probe failed: " + err.Error()
		}
		return status
	}
	status.Version = version
	return status
}

// finalizeHeadlessToolStatus prevents a resolved executable whose bounded
// capability probe failed from being reported as usable. Resolution proves
// only that a path exists; the probe is the minimum evidence required before
// an agent or onboarding flow can trust the tool.
func finalizeHeadlessToolStatus(status headlessToolStatus) headlessToolStatus {
	if status.VersionProbe == "failed" {
		status.Ready = false
		status.State = "failed"
	}
	return status
}

func probeHeadlessToolVersion(ctx context.Context, name string, env map[string]string) (string, string, error) {
	resolver := toolpath.Default()
	if !resolver.HasVersionProbe(name) {
		return "", "unsupported", nil
	}
	version, err := resolver.VersionWithEnv(ctx, name, env)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", "cancelled", err
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "", "timed_out", err
		}
		return "", "failed", err
	}
	return version, "ready", nil
}

func unavailableToolStatus(status headlessToolStatus, err error) headlessToolStatus {
	status.State = "unavailable"
	status.Detail = err.Error()
	if missing, ok := err.(*toolpath.MissingToolError); ok {
		status.Hint = missing.Hint
	}
	return status
}

func toolFailureState(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed_out"
	}
	message := strings.ToLower(err.Error())
	for _, phrase := range []string{
		"not initialized",
		"not registered",
		"not a codemap project",
		"not in a codemap project",
		"not in a vecgrep project",
		"no vecgrep project",
		"no index",
	} {
		if strings.Contains(message, phrase) {
			return "uninitialized"
		}
	}
	return "error"
}

func toolFailureStateContext(ctx context.Context, err error) string {
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	if ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timed_out"
	}
	return toolFailureState(err)
}

func resolveHeadlessBufferTarget(root, path string) (string, string, error) {
	if strings.TrimSpace(path) == "" {
		return "", "", fmt.Errorf("buffer path is empty")
	}
	if root != "" {
		workspace, err := doctorWorkspace(root)
		if err != nil {
			return "", "", err
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(workspace, path)
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return "", "", fmt.Errorf("resolve buffer path: %w", err)
		}
		return workspace, absPath, nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve buffer path: %w", err)
	}
	workspace := findProjectRoot(filepath.Dir(absPath))
	if workspace == "" {
		workspace = filepath.Dir(absPath)
	}
	return workspace, absPath, nil
}

func resolveHeadlessWorkspaceTarget(root, target string) (string, string, error) {
	if strings.TrimSpace(target) == "" {
		return "", "", fmt.Errorf("workspace target is empty")
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace: %w", err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(rootReal, target)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace target: %w", err)
	}
	targetReal, err := filepath.EvalSymlinks(targetAbs)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace target %q: %w", target, err)
	}
	relative, err := filepath.Rel(rootReal, targetReal)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("workspace target %q is outside workspace %q", target, root)
	}
	info, err := os.Stat(targetReal)
	if err != nil {
		return "", "", fmt.Errorf("stat workspace target %q: %w", target, err)
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return "", "", fmt.Errorf("workspace target %q is not a regular file or directory", target)
	}
	return targetReal, filepath.ToSlash(relative), nil
}

func readHeadlessBuffer(root, path string) (headlessBufferResponse, error) {
	return readHeadlessBufferContext(context.Background(), root, path)
}

func readHeadlessBufferContext(ctx context.Context, root, path string) (headlessBufferResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return headlessBufferResponse{}, err
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return headlessBufferResponse{}, fmt.Errorf("resolve workspace: %w", err)
	}
	pathReal, err := filepath.EvalSymlinks(path)
	if err != nil {
		return headlessBufferResponse{}, fmt.Errorf("resolve buffer %q: %w", path, err)
	}
	if err := ctx.Err(); err != nil {
		return headlessBufferResponse{}, err
	}
	relative, err := filepath.Rel(rootReal, pathReal)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return headlessBufferResponse{}, fmt.Errorf("buffer path %q is outside workspace %q", path, root)
	}
	info, err := os.Stat(pathReal)
	if err != nil {
		return headlessBufferResponse{}, fmt.Errorf("stat buffer %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return headlessBufferResponse{}, fmt.Errorf("buffer %q is not a regular file", path)
	}
	if info.Size() > headlessMaxBufferBytes {
		return headlessBufferResponse{}, fmt.Errorf("buffer %q exceeds %d-byte read limit", path, headlessMaxBufferBytes)
	}
	data, err := readHeadlessBufferDataContext(ctx, pathReal, info.Size())
	if err != nil {
		return headlessBufferResponse{}, fmt.Errorf("read buffer %q: %w", path, err)
	}
	if !utf8.Valid(data) {
		return headlessBufferResponse{}, fmt.Errorf("buffer %q is not valid UTF-8", path)
	}
	lineCount := 0
	if len(data) > 0 {
		lineCount = strings.Count(string(data), "\n")
		if data[len(data)-1] != '\n' {
			lineCount++
		}
	}
	checksum := sha256.Sum256(data)
	return headlessBufferResponse{
		Path:         pathReal,
		RelativePath: filepath.ToSlash(relative),
		Bytes:        len(data),
		Lines:        lineCount,
		SHA256:       hex.EncodeToString(checksum[:]),
		Content:      string(data),
	}, nil
}

func writeHeadlessBuffer(root, path, expectedSHA256 string, data []byte) (headlessBufferResponse, error) {
	return writeHeadlessBufferContext(context.Background(), root, path, expectedSHA256, data)
}

func writeHeadlessBufferContext(ctx context.Context, root, path, expectedSHA256 string, data []byte) (headlessBufferResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return headlessBufferResponse{}, err
	}
	expectedSHA256 = strings.TrimSpace(expectedSHA256)
	if len(expectedSHA256) != sha256.Size*2 {
		return headlessBufferResponse{}, fmt.Errorf("expected SHA-256 must contain %d hexadecimal characters", sha256.Size*2)
	}
	if _, err := hex.DecodeString(expectedSHA256); err != nil {
		return headlessBufferResponse{}, fmt.Errorf("expected SHA-256 is invalid: %w", err)
	}
	if len(data) > headlessMaxBufferBytes {
		return headlessBufferResponse{}, fmt.Errorf("buffer input exceeds %d-byte write limit", headlessMaxBufferBytes)
	}
	if !utf8.Valid(data) {
		return headlessBufferResponse{}, fmt.Errorf("buffer input is not valid UTF-8")
	}
	lock, err := acquireHeadlessWriteLock(root, path)
	if err != nil {
		return headlessBufferResponse{}, err
	}
	defer func() { _ = lock.Unlock() }()
	current, err := readHeadlessBufferContext(ctx, root, path)
	if err != nil {
		return headlessBufferResponse{}, err
	}
	if !strings.EqualFold(current.SHA256, expectedSHA256) {
		return headlessBufferResponse{}, fmt.Errorf("buffer changed since it was read: expected %s, found %s", expectedSHA256, current.SHA256)
	}
	currentInfo, err := os.Stat(current.Path)
	if err != nil {
		return headlessBufferResponse{}, fmt.Errorf("inspect buffer identity %q: %w", path, err)
	}
	latest, err := readHeadlessBufferContext(ctx, root, path)
	if err != nil {
		return headlessBufferResponse{}, err
	}
	if !strings.EqualFold(latest.SHA256, expectedSHA256) {
		return headlessBufferResponse{}, fmt.Errorf("buffer changed since it was read: expected %s, found %s", expectedSHA256, latest.SHA256)
	}
	latestInfo, err := os.Stat(latest.Path)
	if err != nil {
		return headlessBufferResponse{}, fmt.Errorf("inspect latest buffer identity %q: %w", path, err)
	}
	if !os.SameFile(currentInfo, latestInfo) {
		return headlessBufferResponse{}, fmt.Errorf("buffer changed since it was read: destination identity was replaced")
	}
	if err := ctx.Err(); err != nil {
		return headlessBufferResponse{}, err
	}
	if err := text.WriteRopeAtomicallyIfUnchanged(latest.Path, latestInfo, text.New(data)); err != nil {
		return headlessBufferResponse{}, fmt.Errorf("write buffer %q: %w", path, err)
	}
	return readHeadlessBufferContext(ctx, root, path)
}

func readHeadlessBufferDataContext(ctx context.Context, path string, size int64) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if size < 0 || size > headlessMaxBufferBytes {
		return nil, fmt.Errorf("buffer size %d exceeds read limit", size)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data := make([]byte, 0, int(size))
	chunk := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, readErr := file.Read(chunk)
		if n > 0 {
			data = append(data, chunk[:n]...)
			if len(data) > headlessMaxBufferBytes {
				return nil, fmt.Errorf("buffer exceeds %d-byte read limit", headlessMaxBufferBytes)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, readErr
		}
		if n == 0 {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func writeHeadlessJSON(w io.Writer, value any) int {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return 1
	}
	body = addHeadlessSchemaVersion(body)
	if _, err := w.Write(body); err != nil {
		return 1
	}
	return 0
}

func writeHeadlessText(stdout, stderr io.Writer, content string) int {
	if _, err := io.WriteString(stdout, content); err != nil {
		return writeHeadlessRuntimeError(stderr, fmt.Errorf("write response: %w", err))
	}
	return 0
}

func writeHeadlessRuntimeError(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
	return 1
}

// addHeadlessSchemaVersion gives machine-facing Teak objects one stable
// envelope marker without forcing every response type to embed mutable
// metadata. Existing explicit versions are preserved so a future operation
// can opt into a newer contract deliberately.
func addHeadlessSchemaVersion(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return body
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil || object == nil {
		return body
	}
	if _, exists := object["schema_version"]; exists {
		return body
	}
	open := bytes.IndexByte(trimmed, '{')
	close := bytes.LastIndexByte(trimmed, '}')
	if open < 0 || close <= open {
		return body
	}
	inner := trimmed[open+1 : close]
	var injected []byte
	if len(bytes.TrimSpace(inner)) == 0 {
		injected = append(injected, trimmed[:open+1]...)
		injected = append(injected, []byte(`"schema_version":1`)...)
		injected = append(injected, trimmed[close:]...)
	} else if bytes.Contains(inner, []byte{'\n'}) {
		firstToken := 0
		for firstToken < len(inner) && (inner[firstToken] == ' ' || inner[firstToken] == '\t' || inner[firstToken] == '\r' || inner[firstToken] == '\n') {
			firstToken++
		}
		lineStart := bytes.LastIndexByte(inner[:firstToken], '\n') + 1
		indent := inner[lineStart:firstToken]
		injected = append(injected, trimmed[:open+1]...)
		injected = append(injected, inner[:lineStart]...)
		injected = append(injected, indent...)
		injected = append(injected, []byte(`"schema_version": 1,`)...)
		injected = append(injected, '\n')
		injected = append(injected, inner[lineStart:]...)
		injected = append(injected, trimmed[close:]...)
	} else {
		injected = append(injected, trimmed[:open+1]...)
		injected = append(injected, []byte(`"schema_version":1,`)...)
		injected = append(injected, inner...)
		injected = append(injected, trimmed[close:]...)
	}
	return injected
}

func writeHeadlessError(w io.Writer, err error) int {
	if output, ok := w.(headlessErrorWriter); ok && output.json {
		response := headlessErrorResponse{
			State:   "error",
			Code:    headlessErrorCode(err),
			Message: err.Error(),
		}
		if code := writeHeadlessJSON(output.Writer, response); code != 0 {
			return code
		}
		return 2
	}
	if _, writeErr := fmt.Fprintf(w, "Error: %v\n", err); writeErr != nil {
		return 1
	}
	return 2
}

func headlessErrorCode(err error) string {
	if toolpath.IsMissing(err) {
		return "tool_unavailable"
	}
	if errors.Is(err, context.Canceled) {
		return "request_cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "changed since it was read"):
		return "stale_write"
	case strings.Contains(message, "timeout") || strings.Contains(message, "timed out"):
		return "timeout"
	case strings.Contains(message, "requires") || strings.Contains(message, "unknown") ||
		strings.Contains(message, "invalid") || strings.Contains(message, "must ") ||
		strings.Contains(message, "does not accept"):
		return "invalid_argument"
	default:
		return "operation_failed"
	}
}

func truncatedSuffix(truncated bool) string {
	if truncated {
		return " (truncated)"
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
