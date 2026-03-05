package clipboard

import (
	"os/exec"
	"runtime"
	"strings"
)

// internal fallback when OS clipboard is unavailable
var internal string

// Copy copies text to the OS clipboard (with internal fallback).
func Copy(text string) {
	internal = text
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		cmd.Run()
	case "linux":
		cmd := exec.Command("xclip", "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
			cmd.Stdin = strings.NewReader(text)
			cmd.Run()
		}
	}
}

// Paste returns text from the OS clipboard (with internal fallback).
func Paste() string {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("pbpaste").Output()
		if err == nil {
			return string(out)
		}
	case "linux":
		out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
		if err == nil {
			return string(out)
		}
		out, err = exec.Command("xsel", "--clipboard", "--output").Output()
		if err == nil {
			return string(out)
		}
	}
	return internal
}
