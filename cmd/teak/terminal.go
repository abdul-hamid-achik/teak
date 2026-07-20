package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
)

// ensureInteractiveTerminal fails before Bubble Tea changes terminal state.
// Teak is an editor rather than a filter: a pipe or TERM=dumb cannot provide
// the input, cursor movement, and screen control the application requires.
func ensureInteractiveTerminal(stdin, stdout *os.File, getenv func(string) string) error {
	stdinIsTTY := stdin != nil && term.IsTerminal(stdin.Fd())
	stdoutIsTTY := stdout != nil && term.IsTerminal(stdout.Fd())
	return terminalStartupError(stdinIsTTY, stdoutIsTTY, getenv("TERM"))
}

// terminalStartupError contains the policy separately from OS file-descriptor
// probing so it is straightforward to regression test on every platform.
func terminalStartupError(stdinIsTTY, stdoutIsTTY bool, terminalType string) error {
	if !stdinIsTTY {
		return fmt.Errorf("teak requires an interactive terminal on stdin; do not pipe input to the editor")
	}
	if !stdoutIsTTY {
		return fmt.Errorf("teak requires an interactive terminal on stdout; do not redirect editor output")
	}
	if strings.EqualFold(strings.TrimSpace(terminalType), "dumb") {
		return fmt.Errorf("teak cannot run with TERM=dumb; use a terminal emulator with cursor support")
	}
	return nil
}
