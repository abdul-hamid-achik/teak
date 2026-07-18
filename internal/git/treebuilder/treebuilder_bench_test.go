package treebuilder

import (
	"testing"

	"teak/internal/git"
)

func BenchmarkBuildEmpty(b *testing.B) {
	entries := []git.StatusEntry{}
	for i := 0; i < b.N; i++ {
		Build(entries, true)
	}
}

func BenchmarkBuildSingleFile(b *testing.B) {
	entries := []git.StatusEntry{
		{Path: "file.txt", IndexStatus: 'M', WorkStatus: ' '},
	}
	for i := 0; i < b.N; i++ {
		Build(entries, true)
	}
}

func BenchmarkBuildMultipleFiles(b *testing.B) {
	entries := []git.StatusEntry{
		{Path: "a.txt", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "b.txt", IndexStatus: 'A', WorkStatus: ' '},
		{Path: "c.txt", IndexStatus: 'D', WorkStatus: ' '},
		{Path: "d.txt", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "e.txt", IndexStatus: 'A', WorkStatus: ' '},
	}
	for i := 0; i < b.N; i++ {
		Build(entries, true)
	}
}

func BenchmarkBuildDeepNesting(b *testing.B) {
	entries := []git.StatusEntry{
		{Path: "a/b/c/d/e/f/g/nested.txt", IndexStatus: 'M', WorkStatus: ' '},
	}
	for i := 0; i < b.N; i++ {
		Build(entries, true)
	}
}

func BenchmarkBuildManyFilesSameDir(b *testing.B) {
	entries := make([]git.StatusEntry, 100)
	for i := range entries {
		entries[i] = git.StatusEntry{
			Path:        entryName(i),
			IndexStatus: 'M',
			WorkStatus:  ' ',
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Build(entries, true)
	}
}

func BenchmarkBuildMixedStructure(b *testing.B) {
	entries := []git.StatusEntry{
		{Path: "cmd/app/main.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "cmd/app/app.go", IndexStatus: 'A', WorkStatus: ' '},
		{Path: "cmd/app/handler.go", IndexStatus: 'D', WorkStatus: ' '},
		{Path: "internal/pkg/util.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "internal/pkg/types.go", IndexStatus: 'A', WorkStatus: ' '},
		{Path: "vendor/dep/file.go", IndexStatus: ' ', WorkStatus: 'M'},
		{Path: "docs/readme.md", IndexStatus: '?', WorkStatus: '?'},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Build(entries, true)
	}
}

func BenchmarkBuildWithDirs(b *testing.B) {
	entries := []git.StatusEntry{
		{Path: "src/", IndexStatus: ' ', WorkStatus: ' ', IsDir: true},
		{Path: "src/main.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "src/util.go", IndexStatus: 'A', WorkStatus: ' '},
		{Path: "tests/", IndexStatus: ' ', WorkStatus: ' ', IsDir: true},
		{Path: "tests/main_test.go", IndexStatus: '?', WorkStatus: '?'},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Build(entries, true)
	}
}

func entryName(n int) string {
	return string([]byte{'f', byte('0' + n%10)}) + ".txt"
}
