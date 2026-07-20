package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenPrivateLogFileUsesPrivateModes(t *testing.T) {
	home := t.TempDir()
	logDir := filepath.Join(home, ".local", "state", "teak")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("create legacy log dir: %v", err)
	}
	logPath := filepath.Join(logDir, "teak.log")
	if err := os.WriteFile(logPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("create legacy log file: %v", err)
	}

	f, err := openPrivateLogFile(home)
	if err != nil {
		t.Fatalf("openPrivateLogFile() error = %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close log file: %v", err)
	}

	dirInfo, err := os.Stat(logDir)
	if err != nil {
		t.Fatalf("stat log dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("log dir mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("log file mode = %o, want 600", got)
	}
}

func TestOpenPrivateLogFileRejectsSymlinkDestination(t *testing.T) {
	home := t.TempDir()
	logDir := filepath.Join(home, ".local", "state", "teak")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("create log dir: %v", err)
	}
	target := filepath.Join(t.TempDir(), "outside.log")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatalf("create outside target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(logDir, "teak.log")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if f, err := openPrivateLogFile(home); err == nil {
		_ = f.Close()
		t.Fatal("openPrivateLogFile() accepted a symlink log destination")
	}
}
