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

func resolveWorkspacePaths(filePath string) (string, string) {
	if filePath != "" {
		if absolutePath, err := filepath.Abs(filePath); err == nil {
			return absolutePath, filepath.Dir(absolutePath)
		}
		return filePath, filepath.Dir(filePath)
	}
	if cwd, err := os.Getwd(); err == nil {
		return "", cwd
	}
	return "", "."
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
