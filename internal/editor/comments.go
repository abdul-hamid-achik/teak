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
