package editor

import (
	"testing"
)

func TestCommentPrefixForFile(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		// Go
		{"main.go", "//"},
		{"package/test.go", "//"},
		{"test.GO", "//"}, // Case insensitive

		// JavaScript/TypeScript
		{"app.js", "//"},
		{"component.jsx", "//"},
		{"index.ts", "//"},
		{"component.tsx", "//"},

		// C family
		{"main.c", "//"},
		{"header.h", "//"},
		{"main.cpp", "//"},
		{"header.hpp", "//"},
		{"Main.java", "//"},
		{"lib.rs", "//"},
		{"main.swift", "//"},
		{"Main.kt", "//"},
		{"Main.scala", "//"},
		{"Program.cs", "//"},
		{"index.php", "//"},
		{"main.dart", "//"},
		{"message.proto", "//"},
		{"main.zig", "//"},
		{"shader.v", "//"},

		// Python family
		{"script.py", "#"},
		{"script.rb", "#"},
		{"script.sh", "#"},
		{"script.bash", "#"},
		{"script.zsh", "#"},
		{"config.yaml", "#"},
		{"config.yml", "#"},
		{"config.toml", "#"},
		{"script.R", "#"},
		{"script.pl", "#"},
		{"script.pm", "#"},
		{"script.tcl", "#"},
		{"script.ps1", "#"},

		// Lua/Haskell/SQL
		{"script.lua", "--"},
		{"module.hs", "--"},
		{"query.sql", "--"},
		{"module.elm", "--"},

		// HTML/XML
		{"index.html", "<!--"},
		{"config.xml", "<!--"},
		{"icon.svg", "<!--"},

		// CSS
		{"styles.css", "/*"},
		{"styles.scss", "//"},
		{"styles.less", "//"},

		// Lisp family
		{"init.vim", "\""},
		{"init.el", ";"},
		{"main.lisp", ";"},
		{"core.clj", ";"},

		// Batch/Tex
		{"script.bat", "REM"},
		{"document.tex", "%"},

		// Unknown
		{"unknown.xyz", ""},
		{"", ""},
		{"noextension", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := CommentPrefixForFile(tt.path)
			if got != tt.want {
				t.Errorf("CommentPrefixForFile(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestCommentPrefixForFileCaseInsensitive(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"MAIN.GO", "//"},
		{"Script.PY", "#"},
		{"Index.HTML", "<!--"},
		{"Styles.CSS", "/*"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := CommentPrefixForFile(tt.path)
			if got != tt.want {
				t.Errorf("CommentPrefixForFile(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestCommentPrefixForFileWithDirectories(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/home/user/project/main.go", "//"},
		{"C:\\Users\\project\\main.go", "//"},
		{"./src/main.go", "//"},
		{"../lib/test.py", "#"},
		{"/var/www/index.html", "<!--"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := CommentPrefixForFile(tt.path)
			if got != tt.want {
				t.Errorf("CommentPrefixForFile(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestCommentPrefixesMap(t *testing.T) {
	// Test that the commentPrefixes map has expected entries
	expectedExtensions := []string{
		".go", ".js", ".ts", ".py", ".rb", ".java", ".rs",
		".html", ".xml", ".css", ".scss", ".lua", ".hs", ".sql",
		".vim", ".el", ".lisp", ".clj", ".bat", ".tex", ".yaml",
		".yml", ".toml", ".sh", ".bash", ".zsh",
	}

	for _, ext := range expectedExtensions {
		prefix, ok := commentPrefixes[ext]
		if !ok {
			t.Errorf("expected comment prefix for extension %q", ext)
		}
		if prefix == "" {
			t.Errorf("expected non-empty comment prefix for extension %q", ext)
		}
	}
}

func TestCommentPrefixesDoubleSlash(t *testing.T) {
	// Languages using // for comments
	doubleSlash := []string{
		".go", ".js", ".jsx", ".ts", ".tsx", ".c", ".h", ".cpp", ".hpp",
		".java", ".rs", ".swift", ".kt", ".scala", ".cs", ".php", ".dart",
		".proto", ".zig", ".v", ".scss", ".less",
	}

	for _, ext := range doubleSlash {
		prefix := commentPrefixes[ext]
		if prefix != "//" {
			t.Errorf("expected // for %q, got %q", ext, prefix)
		}
	}
}

func TestCommentPrefixesHash(t *testing.T) {
	// Languages using # for comments
	hash := []string{
		".py", ".rb", ".sh", ".bash", ".zsh", ".yaml", ".yml", ".toml",
		".r", ".pl", ".pm", ".tcl", ".ps1",
	}

	for _, ext := range hash {
		prefix := commentPrefixes[ext]
		if prefix != "#" {
			t.Errorf("expected # for %q, got %q", ext, prefix)
		}
	}
}

func TestCommentPrefixesDoubleDash(t *testing.T) {
	// Languages using -- for comments
	doubleDash := []string{
		".lua", ".hs", ".sql", ".elm",
	}

	for _, ext := range doubleDash {
		prefix := commentPrefixes[ext]
		if prefix != "--" {
			t.Errorf("expected -- for %q, got %q", ext, prefix)
		}
	}
}

func TestCommentPrefixesHTML(t *testing.T) {
	// Languages using <!-- for comments
	html := []string{
		".html", ".xml", ".svg",
	}

	for _, ext := range html {
		prefix := commentPrefixes[ext]
		if prefix != "<!--" {
			t.Errorf("expected <!-- for %q, got %q", ext, prefix)
		}
	}
}

func TestCommentPrefixesSlashStar(t *testing.T) {
	// Languages using /* for comments
	slashStar := []string{
		".css",
	}

	for _, ext := range slashStar {
		prefix := commentPrefixes[ext]
		if prefix != "/*" {
			t.Errorf("expected /* for %q, got %q", ext, prefix)
		}
	}
}

func TestCommentPrefixesSemicolon(t *testing.T) {
	// Languages using ; for comments
	semicolon := []string{
		".el", ".lisp", ".clj",
	}

	for _, ext := range semicolon {
		prefix := commentPrefixes[ext]
		if prefix != ";" {
			t.Errorf("expected ; for %q, got %q", ext, prefix)
		}
	}
}

func TestCommentPrefixesOther(t *testing.T) {
	// Other comment styles
	tests := []struct {
		ext    string
		prefix string
	}{
		{".vim", "\""},
		{".bat", "REM"},
		{".tex", "%"},
	}

	for _, tt := range tests {
		prefix := commentPrefixes[tt.ext]
		if prefix != tt.prefix {
			t.Errorf("expected %q for %q, got %q", tt.prefix, tt.ext, prefix)
		}
	}
}

func TestCommentPrefixCount(t *testing.T) {
	// Should have a reasonable number of comment prefixes
	count := len(commentPrefixes)
	if count < 30 {
		t.Errorf("expected at least 30 comment prefixes, got %d", count)
	}
	if count > 100 {
		t.Errorf("expected at most 100 comment prefixes, got %d", count)
	}
}
