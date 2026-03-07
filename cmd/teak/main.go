package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/app"
	"teak/internal/config"
)

func main() {
	zone.NewGlobal()

	var filePath string
	if len(os.Args) > 1 {
		filePath = os.Args[1]
	}

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
