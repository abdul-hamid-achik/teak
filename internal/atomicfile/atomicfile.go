// Package atomicfile writes private state files without exposing partially
// written content to readers.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	privateDirMode  = 0o700
	privateFileMode = 0o600
)

// Write writes content to a private temporary file in path's directory, syncs
// it, and replaces path only after write succeeds. Existing symlink and
// non-regular destinations are rejected rather than followed or clobbered.
//
// Replacement is implemented with os.Rename on Unix and MoveFileEx with
// replace/write-through flags on Windows. Keeping the temporary file in the
// destination directory guarantees the replacement stays on one filesystem.
func Write(path string, write func(*os.File) error) error {
	if path == "" {
		return fmt.Errorf("state path is empty")
	}
	if write == nil {
		return fmt.Errorf("state writer is nil")
	}

	dir := filepath.Dir(path)
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	if err := rejectUnsafeDestination(path); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary state file: %w", err)
	}
	tmpPath := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(privateFileMode); err != nil {
		return fmt.Errorf("secure temporary state file: %w", err)
	}
	if err := write(tmp); err != nil {
		return fmt.Errorf("write temporary state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temporary state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary state file: %w", err)
	}
	closed = true

	// Recheck immediately before replacement. A concurrent symlink swap cannot
	// cause a target to be followed by either replacement implementation, but
	// this preserves the explicit no-symlink contract when it is observable.
	if err := rejectUnsafeDestination(path); err != nil {
		return err
	}
	if err := replace(tmpPath, path); err != nil {
		return fmt.Errorf("replace state file: %w", err)
	}
	// Syncing the directory makes the rename durable on Unix. It is best effort
	// because Windows and some network filesystems do not permit opening or
	// syncing directories this way, and replacement already succeeded.
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}

func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, privateDirMode); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("inspect state directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("state directory is not a real directory")
	}
	if err := os.Chmod(dir, privateDirMode); err != nil {
		return fmt.Errorf("secure state directory: %w", err)
	}
	return nil
}

func rejectUnsafeDestination(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect state destination: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("state destination is a symlink")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("state destination is not a regular file")
	}
	return nil
}
