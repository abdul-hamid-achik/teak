package editor

import (
	"path/filepath"
	"strings"
)

var commentPrefixes = map[string]string{
	".go":      "//",
	".js":      "//",
	".jsx":     "//",
	".ts":      "//",
	".tsx":     "//",
	".c":       "//",
	".h":       "//",
	".cpp":     "//",
	".hpp":     "//",
	".java":    "//",
	".rs":      "//",
	".swift":   "//",
	".kt":      "//",
	".scala":   "//",
	".cs":      "//",
	".php":     "//",
	".dart":    "//",
	".proto":   "//",
	".zig":     "//",
	".v":       "//",
	".py":      "#",
	".rb":      "#",
	".sh":      "#",
	".bash":    "#",
	".zsh":     "#",
	".yaml":    "#",
	".yml":     "#",
	".toml":    "#",
	".http":    "#",
	".hitspec": "#",
	".bp":      "#",
	".r":       "#",
	".pl":      "#",
	".pm":      "#",
	".tcl":     "#",
	".lua":     "--",
	".hs":      "--",
	".sql":     "--",
	".elm":     "--",
	".html":    "<!--",
	".xml":     "<!--",
	".svg":     "<!--",
	".css":     "/*",
	".scss":    "//",
	".less":    "//",
	".vim":     "\"",
	".el":      ";",
	".lisp":    ";",
	".clj":     ";",
	".bat":     "REM",
	".ps1":     "#",
	".tex":     "%",
}

// CommentPrefixForFile returns the line comment prefix for a given file path.
func CommentPrefixForFile(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if prefix, ok := commentPrefixes[ext]; ok {
		return prefix
	}
	return ""
}

var languageLabels = map[string]string{
	".go":   "Go",
	".mod":  "Go",
	".rs":   "Rust",
	".py":   "Python",
	".js":   "JavaScript",
	".jsx":  "JavaScript",
	".ts":   "TypeScript",
	".tsx":  "TypeScript",
	".json": "JSON",
	".md":   "Markdown",
	".toml": "TOML",
	".yaml": "YAML",
	".yml":  "YAML",
	".html": "HTML",
	".css":  "CSS",
	".c":    "C",
	".h":    "C",
	".cpp":  "C++",
	".cc":   "C++",
	".java": "Java",
	".rb":   "Ruby",
	".sh":   "Shell",
	".lua":  "Lua",
	".sql":  "SQL",
}

// LanguageLabelForFile is the short language name shown in the status bar.
func LanguageLabelForFile(path string) string {
	if path == "" {
		return "Plain Text"
	}
	ext := strings.ToLower(filepath.Ext(path))
	if label, ok := languageLabels[ext]; ok {
		return label
	}
	if ext != "" {
		return strings.TrimPrefix(strings.ToUpper(ext), ".")
	}
	return "Plain Text"
}
