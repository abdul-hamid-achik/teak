package app

import (
	"fmt"
	"os"
	"path/filepath"

	log "github.com/charmbracelet/log"
)

func openPrivateLogFile(home string) (*os.File, error) {
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
	}
	logDir := filepath.Join(home, ".local", "state", "teak")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	info, err := os.Lstat(logDir)
	if err != nil {
		return nil, fmt.Errorf("inspect log directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("log directory is not a regular directory")
	}
	if err := os.Chmod(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure log directory: %w", err)
	}

	root, err := os.OpenRoot(logDir)
	if err != nil {
		return nil, fmt.Errorf("open log directory: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()

	if info, err := root.Lstat("teak.log"); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("log destination is not a regular file")
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect log destination: %w", err)
	}

	logFile, err := root.OpenFile("teak.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	if err := logFile.Chmod(0o600); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("secure log file: %w", err)
	}
	return logFile, nil
}

// warmUpStyledLogger forces charmbracelet/log's lazy colour-profile
// detection to run once, synchronously, on the caller's goroutine, before
// any other goroutine exists to race with it.
//
// charmbracelet/log formats every styled line through its own lipgloss
// renderer (github.com/charmbracelet/lipgloss v1.1.0, a transitive
// dependency of charmbracelet/log — a different module from the lipgloss
// v2 renderer teak uses for its own UI, which does no such detection). That
// v1 renderer detects the output's colour capabilities lazily inside a
// sync.Once the first time a styled line is rendered, and detection reads
// the underlying *os.File's descriptor via termenv's isTTY() check.
//
// If that first render happens on a background goroutine (the file
// watcher, LSP, DAP, or ACP all log through this same default logger) at
// the same moment something else touches the same *os.File — most
// concretely, the log file being closed during model/test teardown — the
// Fd() read races with the close. Logging one line here, before
// newFileWatcher or any manager can start a goroutine, guarantees
// detection has already completed (and its result cached) by the time
// anything else logs, so no later call ever touches the descriptor again.
func warmUpStyledLogger(logger *log.Logger, logFile *os.File) {
	if logFile == nil {
		// No real destination: the logger was handed a typed-nil *os.File
		// (falling back to discarding logs). Forcing a render here would
		// dereference that nil pointer inside termenv's Fd() check, so
		// leave detection to whenever (if ever) a log call fires for real.
		return
	}
	logger.Info("teak logger initialized")
}
