package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

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

func main() {
	filePath, handled, err := handleCLI(os.Args[1:], os.Stdout, version)
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

	// Keep the initial tab and workspace rooted in the same absolute identity.
	// Otherwise opening a relative CLI path again from the absolute file tree
	// creates a duplicate tab and a second LSP document.
	filePath, rootDir := resolveWorkspacePaths(filePath)

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: config: %v\n", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid configuration: %v\n", err)
		os.Exit(1)
	}

	model, err := app.NewModel(filePath, rootDir, cfg)
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

// resolveWorkspacePaths normalizes the optional CLI path into an initial file
// (empty when opening a directory) and the workspace root used by the file tree,
// LSP, git panel, and session restore.
//
// Rules:
//   - no path: workspace is the current working directory
//   - directory path: open that directory as the workspace (no initial file tab)
//   - file path (or not-yet-created path): open the file; workspace is the
//     nearest project root above the file, or the file's parent if none found
func resolveWorkspacePaths(filePath string) (string, string) {
	if filePath == "" {
		if cwd, err := os.Getwd(); err == nil {
			return "", cwd
		}
		return "", "."
	}

	absolutePath, err := filepath.Abs(filePath)
	if err != nil {
		// Fall back to the raw path so callers still get a best-effort root.
		absolutePath = filePath
	}

	// Directory argument → workspace root is the directory itself, no file tab.
	// (Without this, teak ./myproject used filepath.Dir and opened the parent.)
	if info, err := os.Stat(absolutePath); err == nil && info.IsDir() {
		return "", absolutePath
	}

	parent := filepath.Dir(absolutePath)
	if root := findProjectRoot(parent); root != "" {
		return absolutePath, root
	}
	return absolutePath, parent
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

func handleCLI(args []string, stdout io.Writer, buildVersion string) (filePath string, handled bool, err error) {
	if len(args) == 0 {
		return "", false, nil
	}

	switch args[0] {
	case "--version", "-v":
		if buildVersion == "" {
			buildVersion = developmentVersion
		}
		if _, err := fmt.Fprintf(stdout, "teak %s\n", buildVersion); err != nil {
			return "", false, fmt.Errorf("write version: %w", err)
		}
		return "", true, nil
	default:
		if len(args[0]) > 1 && args[0][0] == '-' {
			return "", false, fmt.Errorf("unknown option %q", args[0])
		}
		return args[0], false, nil
	}
}
