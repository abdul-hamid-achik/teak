package filetree

import (
	"image/color"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
)

// Nerd Font v3 codicon / material-style icons for a modern look
var fileIcons = map[string]string{
	".go":      "\U000f07d3", // language-go
	".js":      "\U000f031e", // language-javascript
	".ts":      "\U000f06e6", // language-typescript
	".tsx":     "\U000f0a4f", // react
	".jsx":     "\U000f0a4f", // react
	".py":      "\U000f0320", // language-python
	".rs":      "\U000f0d49", // language-rust
	".html":    "\U000f031b", // language-html5
	".css":     "\U000f031c", // language-css3
	".scss":    "\U000f0c16", // sass
	".json":    "\U000f0626", // code-json
	".md":      "\U000f035f", // language-markdown
	".yaml":    "\U000f0626", // code-json (config)
	".yml":     "\U000f0626", // code-json (config)
	".toml":    "\U000f0626", // code-json (config)
	".sh":      "\U000f018d", // console
	".bash":    "\U000f018d", // console
	".zsh":     "\U000f018d", // console
	".lua":     "\U000f08b1", // language-lua
	".c":       "\U000f0671", // language-c
	".h":       "\U000f0671", // language-c
	".cpp":     "\U000f0672", // language-cpp
	".hpp":     "\U000f0672", // language-cpp
	".java":    "\U000f0324", // language-java
	".rb":      "\U000f0d2d", // language-ruby
	".php":     "\U000f031e", // language-php
	".swift":   "\U000f06e5", // language-swift
	".kt":      "\U000f0324", // kotlin (java-like)
	".sql":     "\U000f01c8", // database
	".xml":     "\U000f05c0", // xml
	".svg":     "\U000f0721", // svg
	".vue":     "\U000f0844", // vuejs
	".lock":    "\U000f033e", // lock
	".env":     "\U000f033e", // lock
	".txt":     "\U000f0219", // file-document
	".mod":     "\U000f07d3", // language-go
	".sum":     "\U000f07d3", // language-go
	".proto":   "\U000f0626", // code-braces
	".graphql": "\U000f0877", // graphql
}

var filenameIcons = map[string]string{
	"Dockerfile":    "\U000f0868", // docker
	"Makefile":      "\U000f018d", // console
	"Taskfile.yml":  "\U000f018d", // console
	".gitignore":    "\U000f02a2", // git
	".gitmodules":   "\U000f02a2", // git
	"go.mod":        "\U000f07d3", // language-go
	"go.sum":        "\U000f07d3", // language-go
	"LICENSE":       "\U000f0d39", // certificate
	"README.md":     "\U000f035f", // markdown
	"CLAUDE.md":     "\U000f035f", // markdown
	"package.json":  "\U000f0399", // npm
	"tsconfig.json": "\U000f06e6", // typescript
	".eslintrc":     "\U000f0c7b", // eslint
	".prettierrc":   "\U000f0c7b", // eslint
}

var iconColors = map[string]color.Color{
	".go":    lipgloss.Color("#00ADD8"),
	".js":    lipgloss.Color("#F7DF1E"),
	".ts":    lipgloss.Color("#3178C6"),
	".tsx":   lipgloss.Color("#61DAFB"),
	".jsx":   lipgloss.Color("#61DAFB"),
	".py":    lipgloss.Color("#3776AB"),
	".rs":    lipgloss.Color("#DEA584"),
	".html":  lipgloss.Color("#E34F26"),
	".css":   lipgloss.Color("#1572B6"),
	".scss":  lipgloss.Color("#CD6799"),
	".json":  lipgloss.Color("#F5C211"),
	".md":    lipgloss.Color("#519aba"),
	".yaml":  lipgloss.Color("#CB171E"),
	".yml":   lipgloss.Color("#CB171E"),
	".toml":  lipgloss.Color("#9C4121"),
	".sh":    lipgloss.Color("#4EAA25"),
	".bash":  lipgloss.Color("#4EAA25"),
	".zsh":   lipgloss.Color("#4EAA25"),
	".lua":   lipgloss.Color("#000080"),
	".c":     lipgloss.Color("#A8B9CC"),
	".h":     lipgloss.Color("#A8B9CC"),
	".cpp":   lipgloss.Color("#F34B7D"),
	".hpp":   lipgloss.Color("#F34B7D"),
	".java":  lipgloss.Color("#ED8B00"),
	".rb":    lipgloss.Color("#CC342D"),
	".vue":   lipgloss.Color("#41B883"),
	".swift": lipgloss.Color("#F05138"),
	".kt":    lipgloss.Color("#7F52FF"),
	".sql":   lipgloss.Color("#E38C00"),
	".xml":   lipgloss.Color("#E37933"),
	".php":   lipgloss.Color("#777BB4"),
	".proto": lipgloss.Color("#4285F4"),
}

// Modern folder/file icons (Nerd Font v3 material-style)
const (
	IconFolderOpen   = "\U000f0770" // folder-open
	IconFolderClosed = "\U000f024b" // folder
	IconFileDefault  = "\U000f0214" // file-outline
)

var folderColor = lipgloss.Color("#8CAAEE")
var defaultFileColor color.Color = lipgloss.Color("#C6D0F5")

func iconForEntry(entry Entry) (string, color.Color) {
	if entry.IsDir {
		if entry.Loading {
			return "\U000f0252", folderColor // folder-clock
		}
		if entry.Expanded {
			return IconFolderOpen, folderColor
		}
		return IconFolderClosed, folderColor
	}

	name := entry.Name
	// Check exact filename first
	if icon, ok := filenameIcons[name]; ok {
		ext := filepath.Ext(name)
		if color, ok := iconColors[ext]; ok {
			return icon, color
		}
		return icon, defaultFileColor
	}

	// Check extension
	ext := strings.ToLower(filepath.Ext(name))
	if icon, ok := fileIcons[ext]; ok {
		color := defaultFileColor
		if c, ok := iconColors[ext]; ok {
			color = c
		}
		return icon, color
	}

	return IconFileDefault, defaultFileColor
}
