package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/app"
	"teak/internal/config"
)

const developmentVersion = "dev"

// version is replaced at release time with:
//
//	-ldflags "-X main.version=<version>"
var version = developmentVersion

const usageText = `teak - terminal code editor

Usage:
  teak [options] [path ...]
  teak [options] +<line> <file>
  teak [options] <file>:<line>[:<col>]

Arguments:
  path                 File or directory to open. Multiple files open as tabs.
  +<line> <file>       Open file with the cursor on the given 1-based line.
  <file>:<line>[:col]  Open file at line (and optional column), both 1-based.

Options:
  -h, --help           Show this help and exit
  -v, --version        Print version and exit

Examples:
  teak                         Open current directory
  teak ~/projects/myapp        Open a project folder
  teak main.go                 Open a file (workspace = nearest project root)
  teak a.go b.go               Open multiple files as tabs
  teak main.go:42              Open main.go at line 42
  teak +10 internal/app/app.go Open a file at line 10
`

func main() {
	open, handled, err := handleCLI(os.Args[1:], os.Stdout, version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}
	if handled {
		return
	}
	if err := ensureInteractiveTerminal(os.Stdin, os.Stdout, os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	zone.NewGlobal()

	files, rootDir, err := resolveCLIWorkspace(open)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: config: %v\n", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid configuration: %v\n", err)
		os.Exit(1)
	}

	model, err := app.NewModelWithFiles(files, rootDir, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(model, tea.WithFilter(app.QuitFilter))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// cliTarget is one path argument after location parsing (+line / path:line).
type cliTarget struct {
	path string
	// line/col are 0-based; line < 0 means unspecified.
	line int
	col  int
}

// projectRootMarkers are files/directories that identify a project workspace.
// Walking stops at the nearest ancestor that contains any of these so nested
// packages (e.g. monorepo package.json) win over a parent .git.
var projectRootMarkers = []string{
	".git",
	"go.mod",
	"package.json",
	"Cargo.toml",
	"pyproject.toml",
	"setup.py",
	"Pipfile",
	"composer.json",
	"Gemfile",
	"mix.exs",
	"pom.xml",
	"build.gradle",
	"build.gradle.kts",
	"CMakeLists.txt",
	"Makefile",
	".hg",
	".svn",
}

// resolveCLIWorkspace turns parsed CLI targets into startup files and a
// workspace root for the file tree, git panel, LSP, and session restore.
//
// Rules:
//   - no paths: workspace is the current working directory
//   - single directory: open that directory as the workspace (no file tabs)
//   - one or more files: open them as tabs; workspace is the nearest project
//     root above the first file (or that file's parent if none found)
func resolveCLIWorkspace(targets []cliTarget) ([]app.StartupFile, string, error) {
	if len(targets) == 0 {
		if cwd, err := os.Getwd(); err == nil {
			return nil, cwd, nil
		}
		return nil, ".", nil
	}

	// Resolve each target to an absolute path and classify directories vs files.
	type resolved struct {
		abs  string
		line int
		col  int
		dir  bool
	}
	items := make([]resolved, 0, len(targets))
	for _, t := range targets {
		abs, err := filepath.Abs(t.path)
		if err != nil {
			abs = t.path
		}
		// Trailing separator means the user asked for a directory.
		wantsDir := strings.HasSuffix(t.path, string(filepath.Separator)) || strings.HasSuffix(t.path, "/")
		info, err := os.Stat(abs)
		if err != nil {
			if os.IsNotExist(err) && wantsDir {
				return nil, "", fmt.Errorf("directory does not exist: %s", t.path)
			}
			// Not-yet-created file path — still open as a new buffer.
			items = append(items, resolved{abs: abs, line: t.line, col: t.col, dir: false})
			continue
		}
		if info.IsDir() {
			items = append(items, resolved{abs: abs, line: t.line, col: t.col, dir: true})
			continue
		}
		items = append(items, resolved{abs: abs, line: t.line, col: t.col, dir: false})
	}

	if len(items) == 1 && items[0].dir {
		return nil, items[0].abs, nil
	}

	var files []app.StartupFile
	for _, it := range items {
		if it.dir {
			return nil, "", fmt.Errorf("cannot open directory %q together with other paths", it.abs)
		}
		files = append(files, app.StartupFile{Path: it.abs, Line: it.line, Col: it.col})
	}

	parent := filepath.Dir(files[0].Path)
	if root := findProjectRoot(parent); root != "" {
		return files, root, nil
	}
	return files, parent, nil
}

// resolveWorkspacePaths normalizes a single optional CLI path. Kept for tests
// and as a thin wrapper over resolveCLIWorkspace.
func resolveWorkspacePaths(filePath string) (string, string) {
	var targets []cliTarget
	if filePath != "" {
		targets = []cliTarget{{path: filePath, line: -1}}
	}
	files, root, err := resolveCLIWorkspace(targets)
	if err != nil {
		// Missing-directory errors are not expected from this helper's callers
		// (tests use real dirs); fall back to historical parent-dir behavior.
		if absolutePath, absErr := filepath.Abs(filePath); absErr == nil {
			return absolutePath, filepath.Dir(absolutePath)
		}
		return filePath, filepath.Dir(filePath)
	}
	if len(files) == 0 {
		return "", root
	}
	return files[0].Path, root
}

// findProjectRoot walks up from startDir looking for a project marker.
// Returns the nearest matching directory, or "" if none is found before the
// filesystem root.
func findProjectRoot(startDir string) string {
	dir := filepath.Clean(startDir)
	for {
		if hasProjectMarker(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func hasProjectMarker(dir string) bool {
	for _, marker := range projectRootMarkers {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}

// handleCLI parses flags and path arguments. When handled is true the program
// should exit without starting the editor (help/version).
func handleCLI(args []string, stdout io.Writer, buildVersion string) (targets []cliTarget, handled bool, err error) {
	if len(args) == 0 {
		return nil, false, nil
	}

	pendingLine := -1 // from +N; 0-based once accepted

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--version" || arg == "-v":
			if buildVersion == "" {
				buildVersion = developmentVersion
			}
			if _, err := fmt.Fprintf(stdout, "teak %s\n", buildVersion); err != nil {
				return nil, false, fmt.Errorf("write version: %w", err)
			}
			return nil, true, nil
		case arg == "--help" || arg == "-h":
			if _, err := fmt.Fprint(stdout, usageText); err != nil {
				return nil, false, fmt.Errorf("write help: %w", err)
			}
			return nil, true, nil
		case len(arg) > 1 && arg[0] == '+' && isAllDigits(arg[1:]):
			n, convErr := strconv.Atoi(arg[1:])
			if convErr != nil || n < 1 {
				return nil, false, fmt.Errorf("invalid line number %q", arg)
			}
			pendingLine = n - 1
		case len(arg) > 1 && arg[0] == '-':
			return nil, false, fmt.Errorf("unknown option %q", arg)
		default:
			path, line, col := parseFileLocation(arg)
			if pendingLine >= 0 {
				if line >= 0 {
					return nil, false, fmt.Errorf("conflicting line for %q (both +line and :line)", arg)
				}
				line = pendingLine
				pendingLine = -1
			}
			targets = append(targets, cliTarget{path: path, line: line, col: col})
		}
	}
	if pendingLine >= 0 {
		return nil, false, fmt.Errorf("+line requires a file path")
	}
	return targets, false, nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseFileLocation splits "path", "path:line", or "path:line:col".
// Input line/col are 1-based; returned values are 0-based. line is -1 when
// unspecified. An existing filesystem path is never split (names may contain ':').
func parseFileLocation(arg string) (path string, line, col int) {
	line, col = -1, 0
	if arg == "" {
		return "", -1, 0
	}
	if _, err := os.Stat(arg); err == nil {
		return arg, -1, 0
	}

	// path:line:col
	if p, l, c, ok := splitTrailingLineCol(arg); ok {
		return p, l, c
	}
	// path:line
	if p, l, ok := splitTrailingLine(arg); ok {
		return p, l, 0
	}
	return arg, -1, 0
}

func splitTrailingLineCol(arg string) (path string, line, col int, ok bool) {
	i := strings.LastIndex(arg, ":")
	if i <= 0 {
		return "", 0, 0, false
	}
	c, err := strconv.Atoi(arg[i+1:])
	if err != nil || c < 1 {
		return "", 0, 0, false
	}
	rest := arg[:i]
	j := strings.LastIndex(rest, ":")
	if j <= 0 {
		return "", 0, 0, false
	}
	l, err := strconv.Atoi(rest[j+1:])
	if err != nil || l < 1 {
		return "", 0, 0, false
	}
	path = rest[:j]
	if path == "" || !plausibleFilePath(path) {
		return "", 0, 0, false
	}
	return path, l - 1, c - 1, true
}

func splitTrailingLine(arg string) (path string, line int, ok bool) {
	i := strings.LastIndex(arg, ":")
	if i <= 0 {
		return "", 0, false
	}
	l, err := strconv.Atoi(arg[i+1:])
	if err != nil || l < 1 {
		return "", 0, false
	}
	path = arg[:i]
	if path == "" || !plausibleFilePath(path) {
		return "", 0, false
	}
	return path, l - 1, true
}

func plausibleFilePath(path string) bool {
	if path == "" || path == "." || path == ".." {
		return false
	}
	// Reject pure numeric leftovers from aggressive splitting.
	if _, err := strconv.Atoi(path); err == nil {
		return false
	}
	if _, err := os.Stat(path); err == nil {
		return true
	}
	// New files: accept if the parent directory exists, or the name looks like a path/file.
	parent := filepath.Dir(path)
	if parent != "" && parent != path {
		if info, err := os.Stat(parent); err == nil && info.IsDir() {
			return true
		}
	}
	if strings.ContainsRune(path, filepath.Separator) || strings.Contains(path, "/") {
		return true
	}
	// Bare filenames like main.go:42
	if strings.Contains(path, ".") {
		return true
	}
	// Allow extension-less names (Makefile:10) when not purely numeric.
	return true
}
