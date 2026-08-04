package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseStatusPorcelainZ(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want []StatusEntry
	}{
		{
			name: "preserves spaces quotes unicode and backslashes",
			raw:  []byte("?? leading space/quote\"-caf\xc3\xa9\\name.txt\x00 M trailing space .txt \x00"),
			want: []StatusEntry{
				{IndexStatus: '?', WorkStatus: '?', Path: "leading space/quote\"-caf\u00e9\\name.txt"},
				{IndexStatus: ' ', WorkStatus: 'M', Path: "trailing space .txt "},
			},
		},
		{
			name: "rename keeps destination and original path",
			raw:  []byte("R  new name/\xe6\x96\xb0.txt\x00old name/\\source.txt\x00"),
			want: []StatusEntry{{
				IndexStatus: 'R', WorkStatus: ' ', Path: "new name/\u65b0.txt", OriginalPath: "old name/\\source.txt",
			}},
		},
		{
			name: "copy with worktree status keeps original path",
			raw:  []byte(" C copied file.txt\x00source file.txt\x00"),
			want: []StatusEntry{{
				IndexStatus: ' ', WorkStatus: 'C', Path: "copied file.txt", OriginalPath: "source file.txt",
			}},
		},
		{
			name: "malformed and incomplete records are ignored",
			raw:  []byte("M  valid.txt\x00R  incomplete.txt\x00orphan-without-terminator"),
			want: []StatusEntry{{IndexStatus: 'M', WorkStatus: ' ', Path: "valid.txt"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseStatusPorcelainZ(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("count = %d, want %d: got %+v", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("entry %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseStatusPorcelainZIntegration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "teak-test@example.invalid")
	runGit(t, repo, "config", "user.name", "Teak Test")

	oldPath := "old name/\\source caf\u00e9.txt"
	newPath := "new name/\u65b0 quote\".txt"
	if err := os.MkdirAll(filepath.Join(repo, filepath.Dir(oldPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, oldPath), []byte("same contents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "--", oldPath)
	runGit(t, repo, "commit", "-m", "initial")

	if err := os.MkdirAll(filepath.Join(repo, filepath.Dir(newPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "mv", "--", oldPath, newPath)

	cmd := exec.Command("git", "status", "--porcelain=v1", "-z", "-uall")
	cmd.Dir = repo
	raw, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	entries := ParseStatusPorcelainZ(raw)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want one rename", entries)
	}
	entry := entries[0]
	if entry.IndexStatus != 'R' || entry.Path != newPath || entry.OriginalPath != oldPath {
		t.Errorf("rename = %+v, want status R, destination %q, source %q", entry, newPath, oldPath)
	}

	refresh := refreshAfter(repo)
	if len(refresh.Entries) != 1 || refresh.Entries[0] != entry {
		t.Errorf("refresh entries = %+v, want %+v", refresh.Entries, []StatusEntry{entry})
	}
}

func TestStatusContextReturnsReadOnlySnapshot(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "teak@example.test")
	runGit(t, repo, "config", "user.name", "Teak Test")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "tracked.txt")
	runGit(t, repo, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked file.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshot, err := StatusContext(context.Background(), repo)
	if err != nil {
		t.Fatalf("StatusContext() error = %v", err)
	}
	if snapshot.Branch == "" || len(snapshot.Entries) != 2 {
		t.Fatalf("snapshot = %#v, want branch and two entries", snapshot)
	}
	var tracked, untracked bool
	for _, entry := range snapshot.Entries {
		switch entry.Path {
		case "tracked.txt":
			tracked = entry.IsUnstagedChange()
		case "untracked file.txt":
			untracked = entry.IsUntracked()
		}
	}
	if !tracked || !untracked {
		t.Fatalf("entries = %#v, want tracked and untracked states", snapshot.Entries)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
