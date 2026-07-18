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

	zone.NewGlobal()

	// Derive root directory from file path or use cwd
	rootDir := "."
	if filePath != "" {
		absPath, err := filepath.Abs(filePath)
		if err == nil {
			rootDir = filepath.Dir(absPath)
		}
	} else {
		if cwd, err := os.Getwd(); err == nil {
			rootDir = cwd
		}
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

	model, err := app.NewModel(filePath, rootDir, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
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
