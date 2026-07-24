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
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// cacheTTL bounds how long a resolution is reused. Positive and negative
// results share the TTL: a miss must expire so that installing a tool mid
// session takes effect, and a hit must expire so that uninstalling or
// relocating one is noticed.
const cacheTTL = 30 * time.Second

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
	"codemap":                    "brew install codemap",
	"vecgrep":                    "brew install vecgrep",
	"bob":                        "brew install bob",
	"tvault":                     "brew install tvault",
	"monitor":                    "brew install monitor",
	"fcheap":                     "go install github.com/abdulachik/fcheap@latest",
	"dlv":                        "go install github.com/go-delve/delve/cmd/dlv@latest",
	"gopls":                      "go install golang.org/x/tools/gopls@latest",
	"rust-analyzer":              "rustup component add rust-analyzer",
	"typescript-language-server": "npm install -g typescript-language-server typescript",
	"pyright-langserver":         "npm install -g pyright",
	"bash-language-server":       "npm install -g bash-language-server",
	"yaml-language-server":       "npm install -g yaml-language-server",
	"clangd":                     "brew install llvm",
	"opencode":                   "curl -fsSL https://opencode.ai/install | bash",
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
			copied[name] = path
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
		if isExecutable(name) {
			return name, nil
		}
		return "", &MissingToolError{Tool: name}
	}

	now := r.now()
	r.mu.Lock()
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
	path, err := r.Resolve(name)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, path, args...), nil
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
