package clipboard

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// internal fallback when OS clipboard is unavailable
var internal string

// Copy copies text to the OS clipboard (with internal fallback).
// Returns an error if the OS clipboard command fails, but the internal
// fallback always succeeds.
func Copy(text string) error {
	internal = text
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("pbcopy: %w", err)
		}
	case "linux":
		cmd := exec.Command("xclip", "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
			cmd.Stdin = strings.NewReader(text)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("clipboard copy: %w", err)
			}
		}
	}
	return nil
}

// Paste returns text from the OS clipboard (with internal fallback).
// Returns the pasted text and an error if the OS clipboard command fails.
// Falls back to the internal buffer on error.
func Paste() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("pbpaste").Output()
		if err == nil {
			return string(out), nil
		}
		return internal, fmt.Errorf("pbpaste: %w", err)
	case "linux":
		out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
		if err == nil {
			return string(out), nil
		}
		out, err = exec.Command("xsel", "--clipboard", "--output").Output()
		if err == nil {
			return string(out), nil
		}
		return internal, fmt.Errorf("clipboard paste: %w", err)
	}
	return internal, nil
}
