// Package toolpath resolves external CLI binaries that Teak shells out to.
//
// Teak inherits whatever PATH its parent process hands it. That is usually the
// user's interactive shell, but not always: a tmux server started before a
// shell profile changed, a non-login `sh -c teak`, or a launcher that strips the
// environment all produce a PATH missing the directories where developer tools
// actually live (Homebrew, asdf shims, ~/go/bin, cargo, bun). Resolving a bare
// name with exec.LookPath alone therefore reports "not found" for tools that are
// installed and working from the user's own terminal.
//
// Resolve searches PATH first, so an explicit user PATH or an activated virtual
// environment always wins, then falls back to well-known install directories.
// Results are cached with a short TTL rather than for the process lifetime, so a
// tool installed while Teak is running becomes usable without a restart.
package toolpath

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// cacheTTL bounds how long a resolution is reused. Positive and negative
// results share the TTL: a miss must expire so that installing a tool mid
// session takes effect, and a hit must expire so that uninstalling or
// relocating one is noticed.
const cacheTTL = 30 * time.Second

const (
	// Version probes run external shims and language-tool wrappers. Five
	// seconds keeps the health contract bounded while leaving enough headroom
	// for a busy or race-instrumented host to schedule the child process.
	versionProbeTimeout = 5 * time.Second
	maxProbeOutput      = 64 << 10
)

// ErrVersionProbeUnsupported means Teak does not know a safe, non-interactive
// version command for the requested tool. It is different from a failed probe:
// callers can report "unsupported" without claiming that the executable is
// broken.
var ErrVersionProbeUnsupported = errors.New("version probe is not configured")

var versionProbeArgs = map[string][]string{
	"bash-language-server":       {"--version"},
	"clangd":                     {"--version"},
	"codemap":                    {"--version"},
	"dlv":                        {"version"},
	"git":                        {"--version"},
	"glyph":                      {"--version"},
	"gopls":                      {"version"},
	"hitspec":                    {"--version"},
	"lua-language-server":        {"--version"},
	"opencode":                   {"--version"},
	"pyright-langserver":         {"--version"},
	"rg":                         {"--version"},
	"rust-analyzer":              {"--version"},
	"typescript-language-server": {"--version"},
	"vecgrep":                    {"--version"},
	"yaml-language-server":       {"--version"},
}

// MissingToolError reports a binary that could not be resolved. Hint carries an
// install command when one is known, so callers can render actionable UI
// instead of a bare "not found".
type MissingToolError struct {
	Tool string
	Hint string
}

func (e *MissingToolError) Error() string {
	if e.Hint != "" {
		return e.Tool + " not found. Install with: " + e.Hint
	}
	return e.Tool + " not found in PATH"
}

// IsMissing reports whether err is (or wraps) a MissingToolError.
func IsMissing(err error) bool {
	var missing *MissingToolError
	return errors.As(err, &missing)
}

// installHints maps a binary to the command that installs it. Tools absent from
// this table still resolve normally; they just produce an error without a hint.
var installHints = map[string]string{
	"codemap":                     "brew install codemap",
	"vecgrep":                     "brew install abdul-hamid-achik/tap/vecgrep",
	"bob":                         "brew install bob",
	"tvault":                      "brew install tvault",
	"monitor":                     "brew install monitor",
	"fcheap":                      "go install github.com/abdulachik/fcheap@latest",
	"dlv":                         "go install github.com/go-delve/delve/cmd/dlv@latest",
	"gopls":                       "go install golang.org/x/tools/gopls@latest",
	"pylsp":                       "python -m pip install python-lsp-server",
	"rust-analyzer":               "rustup component add rust-analyzer",
	"typescript-language-server":  "npm install -g typescript-language-server typescript",
	"pyright-langserver":          "npm install -g pyright",
	"bash-language-server":        "npm install -g bash-language-server",
	"yaml-language-server":        "npm install -g yaml-language-server",
	"clangd":                      "brew install llvm",
	"jdtls":                       "brew install jdtls",
	"lua-language-server":         "brew install lua-language-server",
	"zls":                         "brew install zls",
	"solargraph":                  "gem install solargraph",
	"elixir-ls":                   "brew install elixir-ls",
	"vscode-css-language-server":  "npm install -g vscode-langservers-extracted",
	"vscode-html-language-server": "npm install -g vscode-langservers-extracted",
	"vscode-json-language-server": "npm install -g vscode-langservers-extracted",
	"opencode":                    "curl -fsSL https://opencode.ai/install | bash",
	"OmniSharp":                   "dotnet tool install --global omnisharp-roslyn",
	"bp":                          "go install github.com/abdul-hamid-achik/blueprint/cmd/bp@latest",
}

// Hint returns the install command for a binary, or "" when none is known.
func Hint(name string) string { return installHints[name] }

type cacheEntry struct {
	path    string
	err     error
	expires time.Time
}

// Resolver finds external binaries. The zero value is not usable; call New.
type Resolver struct {
	overrides map[string]string
	extraDirs []string

	mu    sync.Mutex
	cache map[string]cacheEntry
	now   func() time.Time // injectable for tests
}

// New builds a Resolver. overrides maps a binary name to an absolute path,
// bypassing all searching for that name; it comes from user configuration and
// is the escape hatch for layouts this package does not know about. A nil or
// empty map is fine.
func New(overrides map[string]string) *Resolver {
	copied := make(map[string]string, len(overrides))
	for name, path := range overrides {
		if path != "" {
			if !filepath.IsAbs(path) {
				if absolute, err := filepath.Abs(path); err == nil {
					path = absolute
				}
			}
			copied[name] = filepath.Clean(path)
		}
	}
	return &Resolver{
		overrides: copied,
		extraDirs: wellKnownDirs(),
		cache:     make(map[string]cacheEntry),
		now:       time.Now,
	}
}

// wellKnownDirs lists directories that commonly hold developer tools but are
// absent from the bare OS PATH. Built once per Resolver: these depend on the
// environment, not on the binary being looked up.
//
// Order matters. Version-manager shims come before static install prefixes so
// that a project-pinned toolchain wins over a stray global copy.
func wellKnownDirs() []string {
	var dirs []string
	seen := make(map[string]bool)
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}

	home, _ := os.UserHomeDir()
	homeJoin := func(parts ...string) string {
		if home == "" {
			return ""
		}
		return filepath.Join(append([]string{home}, parts...)...)
	}

	// Version-manager shims first.
	add(homeJoin(".asdf", "shims"))
	add(homeJoin(".mise", "shims"))
	add(homeJoin(".local", "share", "mise", "shims"))
	add(homeJoin(".rbenv", "shims"))
	add(homeJoin(".pyenv", "shims"))

	// Go: honour GOBIN and GOPATH before assuming the default location.
	add(os.Getenv("GOBIN"))
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		// GOPATH may be a list; only the first element receives `go install`.
		for _, p := range filepath.SplitList(gopath) {
			add(filepath.Join(p, "bin"))
			break
		}
	}
	add(homeJoin("go", "bin"))

	// Per-user install prefixes.
	add(homeJoin(".local", "bin"))
	add(homeJoin(".cargo", "bin"))
	add(homeJoin(".bun", "bin"))
	add(homeJoin(".deno", "bin"))
	add(homeJoin(".npm-global", "bin"))
	add(homeJoin(".opencode", "bin"))

	// System-wide prefixes.
	if runtime.GOOS == "darwin" {
		add("/opt/homebrew/bin")  // Apple silicon
		add("/opt/homebrew/sbin") //
		add("/usr/local/bin")     // Intel Homebrew and manual installs
	} else {
		add("/usr/local/bin")
		add("/home/linuxbrew/.linuxbrew/bin")
		add("/snap/bin")
	}

	return dirs
}

// Resolve returns the absolute path to name. It searches, in order: a
// configured override, PATH, then the well-known directories. On failure it
// returns a *MissingToolError carrying an install hint when one is known.
//
// Both outcomes are cached for cacheTTL, so callers may call this on a hot path
// without worrying about repeated stat syscalls.
func (r *Resolver) Resolve(name string) (string, error) {
	if name == "" {
		return "", &MissingToolError{Tool: name}
	}
	// A name that already carries a separator is a path, not a PATH lookup.
	if filepath.IsAbs(name) || filepath.Base(name) != name {
		candidate := name
		if !filepath.IsAbs(candidate) {
			absolute, err := filepath.Abs(candidate)
			if err != nil {
				return "", &MissingToolError{Tool: name, Hint: "resolve explicit path: " + err.Error()}
			}
			candidate = absolute
		}
		if isExecutable(candidate) {
			return filepath.Clean(candidate), nil
		}
		return "", &MissingToolError{Tool: name}
	}

	r.mu.Lock()
	if r.now == nil {
		r.now = time.Now
	}
	if r.cache == nil {
		r.cache = make(map[string]cacheEntry)
	}
	if r.extraDirs == nil {
		r.extraDirs = wellKnownDirs()
	}
	now := r.now()
	if entry, ok := r.cache[name]; ok && now.Before(entry.expires) {
		r.mu.Unlock()
		return entry.path, entry.err
	}
	r.mu.Unlock()

	path, err := r.lookup(name)

	r.mu.Lock()
	r.cache[name] = cacheEntry{path: path, err: err, expires: now.Add(cacheTTL)}
	r.mu.Unlock()

	return path, err
}

func (r *Resolver) lookup(name string) (string, error) {
	if override, ok := r.overrides[name]; ok {
		if isExecutable(override) {
			return override, nil
		}
		// A configured override that does not exist is a configuration error
		// worth reporting, not something to silently fall back from: falling
		// back would quietly run a different binary than the user asked for.
		return "", &MissingToolError{
			Tool: name,
			Hint: "configured path " + override + " is not executable; fix or remove the override",
		}
	}

	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}

	for _, dir := range r.extraDirs {
		candidate := filepath.Join(dir, name)
		if isExecutable(candidate) {
			return candidate, nil
		}
	}

	return "", &MissingToolError{Tool: name, Hint: installHints[name]}
}

// Available reports whether name can be resolved. It is a convenience wrapper
// over Resolve and shares its cache, so probing availability every frame is
// cheap.
func (r *Resolver) Available(name string) bool {
	_, err := r.Resolve(name)
	return err == nil
}

// Invalidate drops the cached result for name, forcing the next Resolve to
// search again. Pass no arguments to clear the whole cache.
func (r *Resolver) Invalidate(names ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(names) == 0 {
		r.cache = make(map[string]cacheEntry)
		return
	}
	for _, name := range names {
		delete(r.cache, name)
	}
}

// Command builds an *exec.Cmd with an absolute Path, so callers never hand a
// bare name to os/exec and never depend on the inherited PATH.
func (r *Resolver) Command(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	path, err := r.Resolve(name)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	ConfigureCommand(cmd)
	return cmd, nil
}

// ConfigureCommand bounds a resolved command and, on Unix, makes context
// cancellation terminate its process group. Callers that expose command
// execution to scripts or agents should use it so descendants cannot keep
// inherited output pipes alive after the direct process exits.
func ConfigureCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	configureCommandProcess(cmd)
	cmd.WaitDelay = 150 * time.Millisecond
}

// TerminateCommand performs the strongest bounded termination available for a
// command configured by this package. On Unix, Command's Cancel callback
// kills the isolated process group; on other platforms it falls back to the
// direct process cancellation supplied by os/exec.
func TerminateCommand(cmd *exec.Cmd) error {
	if cmd == nil {
		return nil
	}
	if cmd.Cancel != nil {
		return cmd.Cancel()
	}
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// HasVersionProbe reports whether Version knows a safe command for name.
func (r *Resolver) HasVersionProbe(name string) bool {
	_, ok := versionProbeArgs[name]
	return ok
}

// Version executes a bounded, non-interactive version probe. It resolves the
// executable through the same absolute-path path as normal tool invocations,
// caps combined stdout/stderr, and never invokes a shell. A failed probe is
// returned as an error so onboarding can distinguish an installed-but-broken
// binary from a merely missing one.
func (r *Resolver) Version(ctx context.Context, name string) (string, error) {
	return r.version(ctx, name, nil)
}

// VersionWithEnv executes the same bounded version probe as Version while
// forwarding explicit environment overrides. Language servers often depend on
// a project-selected SDK, PATH, or feature flag; health probes must observe
// the same configured environment as the interactive launcher or they can
// report a working server as broken.
func (r *Resolver) VersionWithEnv(ctx context.Context, name string, env map[string]string) (string, error) {
	return r.version(ctx, name, env)
}

func (r *Resolver) version(ctx context.Context, name string, env map[string]string) (string, error) {
	args, ok := versionProbeArgs[name]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrVersionProbeUnsupported, name)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	defer cancel()
	cmd, err := r.Command(probeCtx, name, args...)
	if err != nil {
		return "", err
	}
	if len(env) > 0 {
		cmd.Env = mergeEnvironment(os.Environ(), env)
	}
	// A resolved tool may be a shim or wrapper that launches a child process;
	// keep the whole process tree bounded during the health check.
	ConfigureCommand(cmd)
	var output cappedProbeBuffer
	output.onLimit = func() { _ = TerminateCommand(cmd) }
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		if probeErr := probeCtx.Err(); probeErr != nil {
			return "", fmt.Errorf("%s version probe: %w", name, probeErr)
		}
		if output.Truncated() {
			return "", fmt.Errorf("%s version probe output exceeds %d bytes", name, maxProbeOutput)
		}
		detail := firstProbeOutputLine(output.String())
		if detail == "" {
			return "", fmt.Errorf("%s version probe: %w", name, err)
		}
		return "", fmt.Errorf("%s version probe: %w: %s", name, err, detail)
	}
	if output.Truncated() {
		return "", fmt.Errorf("%s version probe output exceeds %d bytes", name, maxProbeOutput)
	}
	for _, line := range strings.Split(output.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line, nil
		}
	}
	return "", fmt.Errorf("%s version probe returned no output", name)
}

func mergeEnvironment(base []string, overrides map[string]string) []string {
	result := make([]string, 0, len(base)+len(overrides))
	replaced := make(map[string]struct{}, len(overrides))
	for _, entry := range base {
		name, _, hasValue := strings.Cut(entry, "=")
		value, overridden := overrides[name]
		if !hasValue || !overridden {
			result = append(result, entry)
			continue
		}
		if _, alreadyReplaced := replaced[name]; alreadyReplaced {
			continue
		}
		result = append(result, name+"="+value)
		replaced[name] = struct{}{}
	}
	names := make([]string, 0, len(overrides))
	for name := range overrides {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, exists := replaced[name]; !exists {
			result = append(result, name+"="+overrides[name])
		}
	}
	return result
}

func firstProbeOutputLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		const maxDetail = 256
		if len(line) > maxDetail {
			return line[:maxDetail] + "..."
		}
		return line
	}
	return ""
}

// cappedProbeBuffer prevents a malformed or unexpectedly verbose tool from
// turning a health check into an unbounded allocation.
type cappedProbeBuffer struct {
	mu        sync.Mutex
	limitOnce sync.Once
	bytes.Buffer
	truncated bool
	onLimit   func()
}

func (b *cappedProbeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	remaining := maxProbeOutput - b.Len()
	if remaining <= 0 {
		b.truncated = true
		b.mu.Unlock()
		b.limitOnce.Do(func() {
			if b.onLimit != nil {
				b.onLimit()
			}
		})
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		b.truncated = true
		b.mu.Unlock()
		b.limitOnce.Do(func() {
			if b.onLimit != nil {
				b.onLimit()
			}
		})
		return len(p), nil
	}
	n, err := b.Buffer.Write(p)
	b.mu.Unlock()
	return n, err
}

// ReadFrom overrides bytes.Buffer.ReadFrom. Because bytes.Buffer is embedded,
// its optimized implementation would otherwise be promoted and bypass Write,
// defeating the probe's output cap.
func (b *cappedProbeBuffer) ReadFrom(r io.Reader) (int64, error) {
	buf := make([]byte, 32<<10)
	var total int64
	for {
		n, err := r.Read(buf)
		if n > 0 {
			written, writeErr := b.Write(buf[:n])
			total += int64(written)
			if b.Truncated() {
				return total, nil
			}
			if writeErr != nil {
				return total, writeErr
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

func (b *cappedProbeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

func (b *cappedProbeBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// SearchPath returns the directories Resolve consults after PATH. Exposed for
// diagnostics UI so a user can see where Teak looked.
func (r *Resolver) SearchPath() []string {
	return append([]string(nil), r.extraDirs...)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

// Default is the process-wide Resolver used by packages that have no natural
// place to thread one through. Configure replaces it at startup once user
// settings are loaded.
var (
	defaultMu sync.RWMutex
	defaultR  = New(nil)
)

// Configure installs overrides into the default Resolver. Call once at startup.
func Configure(overrides map[string]string) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultR = New(overrides)
}

// Default returns the process-wide Resolver.
func Default() *Resolver {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultR
}

// Resolve calls Resolve on the default Resolver.
func Resolve(name string) (string, error) { return Default().Resolve(name) }

// Available calls Available on the default Resolver.
func Available(name string) bool { return Default().Available(name) }

// Invalidate calls Invalidate on the default Resolver.
func Invalidate(names ...string) { Default().Invalidate(names...) }

// Command calls Command on the default Resolver.
func Command(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	return Default().Command(ctx, name, args...)
}

// Version calls Version on the default Resolver.
func Version(ctx context.Context, name string) (string, error) {
	return Default().Version(ctx, name)
}

// VersionWithEnv calls VersionWithEnv on the default resolver.
func VersionWithEnv(ctx context.Context, name string, env map[string]string) (string, error) {
	return Default().VersionWithEnv(ctx, name, env)
}
