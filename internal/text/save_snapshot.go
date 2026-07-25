package text

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrDestinationReadOnly reports that the destination exists but its owner
// write bit is clear. It is distinguished from other save failures so callers
// can offer to override rather than only reporting the error.
var ErrDestinationReadOnly = errors.New("destination is read-only")

// resolveSaveDestination follows a symbolic link to the file it points at, so a
// save replaces the target rather than the link.
//
// NewBufferFromFile already opens through symlinks — see
// TestNewBufferFromFileFollowsSymlinkToRegularFile — so rejecting them here
// meant a symlinked file could be opened and edited but never saved, which is
// the normal case for dotfiles managed by stow or chezmoi. The link is resolved
// rather than written through, and the target must itself be a regular file, so
// a link pointing at a device or a directory is still refused.
func resolveSaveDestination(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, nil // a new file; nothing to resolve
		}
		return "", fmt.Errorf("inspect destination: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve symbolic link: %w", err)
	}
	target, err := os.Lstat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect symbolic link target: %w", err)
	}
	if !target.Mode().IsRegular() {
		return "", fmt.Errorf("symbolic link does not point at a regular file")
	}
	return resolved, nil
}

// WriteRopeAtomically persists an immutable rope without changing any Buffer
// state. Callers can therefore run it from an async command and reconcile the
// saved snapshot on the UI goroutine when it completes.
func WriteRopeAtomically(path string, rope *Rope) (retErr error) {
	if rope == nil {
		return fmt.Errorf("write rope atomically: nil rope")
	}

	path, err := resolveSaveDestination(path)
	if err != nil {
		return err
	}

	mode := os.FileMode(0o600)
	destinationExists := false
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("destination is not a regular file")
		}
		// Replacing a file only needs write permission on its directory, so an
		// atomic rename happily overwrites a file the user deliberately marked
		// read-only. Refuse instead, the way vim requires an explicit :w!.
		if info.Mode().Perm()&0o200 == 0 {
			return fmt.Errorf("%w: %s", ErrDestinationReadOnly, path)
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
