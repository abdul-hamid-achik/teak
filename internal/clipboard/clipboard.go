package clipboard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"teak/internal/toolpath"
)

const (
	clipboardCommandTimeout = 2 * time.Second
	// MaxClipboardBytes bounds clipboard values retained in memory and sent to
	// external clipboard programs. It prevents a paste or selection from
	// monopolizing the editor process.
	MaxClipboardBytes     = 16 << 20
	internalClipboardMode = "internal"
)

var (
	// ErrTooLarge is returned before replacing the fallback with an oversized
	// clipboard payload.
	ErrTooLarge = errors.New("clipboard payload exceeds size limit")
	// ErrInvalidUTF8 protects the terminal renderer from invalid text.
	ErrInvalidUTF8 = errors.New("clipboard payload is not valid UTF-8")
)

// internal is the process-local fallback when an OS clipboard is unavailable.
// Keep the variable for package-level compatibility; all production access is
// synchronized through the helpers below.
var (
	internal   string
	internalMu sync.RWMutex
)

func setInternalClipboard(text string) {
	internalMu.Lock()
	internal = text
	internalMu.Unlock()
}

func internalClipboard() string {
	internalMu.RLock()
	defer internalMu.RUnlock()
	return internal
}

// Store validates text and immediately makes it available through the
// process-local fallback. Callers can use it in the synchronous UI path, then
// schedule CopyToSystem independently.
func Store(text string) error {
	if err := Validate(text); err != nil {
		return err
	}
	setInternalClipboard(text)
	return nil
}

// Validate ensures a clipboard payload is small enough to handle and safe to
// render in a terminal.
func Validate(text string) error {
	if len(text) > MaxClipboardBytes {
		return fmt.Errorf("%w: %d bytes (limit %d)", ErrTooLarge, len(text), MaxClipboardBytes)
	}
	if !utf8.ValidString(text) {
		return ErrInvalidUTF8
	}
	return nil
}

// Copy copies text to the OS clipboard while always updating the internal
// fallback. An OS error is returned to callers that want to surface degraded
// clipboard integration.
func Copy(text string) error {
	if err := Store(text); err != nil {
		return err
	}
	return CopyToSystem(text)
}

// CopyToSystem writes a previously validated value to the OS clipboard. It
// deliberately does not mutate the fallback, so UI callers can store a value
// immediately and defer this potentially blocking integration to a tea.Cmd.
func CopyToSystem(text string) error {
	if err := Validate(text); err != nil {
		return err
	}
	if systemClipboardDisabled() {
		return nil
	}

	candidates := clipboardCopyCommands(runtime.GOOS, os.Getenv("WAYLAND_DISPLAY") != "")
	var lastErr error
	for _, candidate := range candidates {
		if err := runCopyCommand(candidate, text); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return fmt.Errorf("clipboard copy: %w", lastErr)
	}
	return nil
}

// Paste returns text from the OS clipboard. If integration is unavailable or
// fails, it returns the synchronized process-local fallback with the error.
func Paste() (string, error) {
	if systemClipboardDisabled() {
		return internalClipboard(), nil
	}
	candidates := clipboardPasteCommands(runtime.GOOS, os.Getenv("WAYLAND_DISPLAY") != "")
	var lastErr error
	for _, candidate := range candidates {
		output, err := runPasteCommand(candidate)
		if err == nil {
			setInternalClipboard(output)
			return output, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return internalClipboard(), fmt.Errorf("clipboard paste: %w", lastErr)
	}
	return internalClipboard(), nil
}

func systemClipboardDisabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("TEAK_CLIPBOARD")), internalClipboardMode)
}

func clipboardCopyCommands(goos string, wayland bool) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "linux":
		var commands [][]string
		if wayland {
			commands = append(commands, []string{"wl-copy"})
		}
		return append(commands,
			[]string{"xclip", "-selection", "clipboard"},
			[]string{"xsel", "--clipboard", "--input"},
		)
	case "freebsd", "openbsd", "netbsd", "dragonfly":
		return [][]string{
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
	case "windows":
		return [][]string{{"clip.exe"}}
	default:
		return nil
	}
}

func clipboardPasteCommands(goos string, wayland bool) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"pbpaste"}}
	case "linux":
		var commands [][]string
		if wayland {
			commands = append(commands, []string{"wl-paste", "--no-newline"})
		}
		return append(commands,
			[]string{"xclip", "-selection", "clipboard", "-o"},
			[]string{"xsel", "--clipboard", "--output"},
		)
	case "freebsd", "openbsd", "netbsd", "dragonfly":
		return [][]string{
			{"xclip", "-selection", "clipboard", "-o"},
			{"xsel", "--clipboard", "--output"},
		}
	case "windows":
		return [][]string{{
			"powershell.exe",
			"-NoProfile",
			"-NonInteractive",
			"-Command",
			"[Console]::OutputEncoding=[Text.UTF8Encoding]::new(); Get-Clipboard -Raw",
		}}
	default:
		return nil
	}
}

func runCopyCommand(candidate []string, text string) error {
	if len(candidate) == 0 {
		return fmt.Errorf("clipboard command is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), clipboardCommandTimeout)
	defer cancel()

	cmd, err := toolpath.Command(ctx, candidate[0], candidate[1:]...)
	if err != nil {
		return fmt.Errorf("%s: %w", candidate[0], err)
	}
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s timed out: %w", candidate[0], ctx.Err())
		}
		return fmt.Errorf("%s: %w", candidate[0], err)
	}
	return nil
}

func runPasteCommand(candidate []string) (string, error) {
	if len(candidate) == 0 {
		return "", fmt.Errorf("clipboard command is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), clipboardCommandTimeout)
	defer cancel()

	cmd, err := toolpath.Command(ctx, candidate[0], candidate[1:]...)
	if err != nil {
		return "", fmt.Errorf("%s: %w", candidate[0], err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("%s stdout: %w", candidate[0], err)
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("%s: %w", candidate[0], err)
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, MaxClipboardBytes+1))
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return "", fmt.Errorf("%s timed out: %w", candidate[0], ctx.Err())
	}
	if readErr != nil {
		return "", fmt.Errorf("%s output: %w", candidate[0], readErr)
	}
	if waitErr != nil {
		return "", fmt.Errorf("%s: %w", candidate[0], waitErr)
	}
	if len(output) > MaxClipboardBytes {
		return "", fmt.Errorf("%s output exceeds %d-byte limit", candidate[0], MaxClipboardBytes)
	}
	text := string(output)
	if err := Validate(text); err != nil {
		return "", fmt.Errorf("%s output: %w", candidate[0], err)
	}
	return text, nil
}
