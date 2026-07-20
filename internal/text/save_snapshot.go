package text

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
)

// WriteRopeAtomically persists an immutable rope without changing any Buffer
// state. Callers can therefore run it from an async command and reconcile the
// saved snapshot on the UI goroutine when it completes.
func WriteRopeAtomically(path string, rope *Rope) (retErr error) {
	if rope == nil {
		return fmt.Errorf("write rope atomically: nil rope")
	}

	mode := os.FileMode(0o600)
	destinationExists := false
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("destination is a symbolic link")
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("destination is not a regular file")
		}
		destinationExists = true
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect destination: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) && retErr == nil {
			retErr = fmt.Errorf("remove temp file: %w", err)
		}
	}()

	if destinationExists {
		if err := tmp.Chmod(mode); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("set temp permissions: %w", err)
		}
	}
	writer := bufio.NewWriterSize(tmp, 64<<10)
	if _, err := rope.WriteTo(writer); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := writer.Flush(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("flush temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := replaceFileAtomically(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	// Directory sync is best-effort: after a successful rename the new content
	// is already visible, so an unsupported metadata sync must not be reported
	// as if the save itself failed.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// MarkSavedSnapshot updates Buffer save state on the UI goroutine after an
// async snapshot write succeeds. If the buffer changed since the snapshot was
// captured, it intentionally remains dirty.
func (b *Buffer) MarkSavedSnapshot(path string, snapshot *Rope) {
	if snapshot == nil {
		return
	}
	b.FilePath = path
	b.savedRope = snapshot
	b.dirty = b.rope != snapshot
}
