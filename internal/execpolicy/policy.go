// Package execpolicy builds bounded child-process commands for agent tools.
//
// Filesystem containment and capability authorization still belong to the ACP
// handler. This package adds an OS-level boundary where the host provides one:
// macOS Seatbelt is used for auto/required policies, while required mode fails
// closed on systems without a supported backend. Auto mode is intentionally
// observable through its returned status and does not pretend that a missing
// backend is isolation.
package execpolicy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"teak/internal/toolpath"
)

type Mode string

const (
	ModeOff      Mode = "off"
	ModeAuto     Mode = "auto"
	ModeRequired Mode = "required"
)

type Status string

const (
	StatusDisabled    Status = "disabled"
	StatusApplied     Status = "applied"
	StatusUnavailable Status = "unavailable"
)

var (
	ErrInvalidPolicy      = errors.New("invalid execution policy")
	ErrSandboxUnavailable = errors.New("OS sandbox backend unavailable")
)

// Policy controls the OS-level wrapper for one workspace's child commands.
// SandboxExecutable is primarily useful for deterministic tests and explicit
// installations; an empty value resolves the platform backend.
type Policy struct {
	Root              string
	Mode              Mode
	SandboxExecutable string
}

func New(root string, mode Mode) (Policy, error) {
	if mode != ModeOff && mode != ModeAuto && mode != ModeRequired {
		return Policy{}, fmt.Errorf("%w: unsupported mode %q", ErrInvalidPolicy, mode)
	}
	if strings.TrimSpace(root) == "" {
		return Policy{}, fmt.Errorf("%w: workspace root is empty", ErrInvalidPolicy)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Policy{}, fmt.Errorf("%w: resolve workspace root: %v", ErrInvalidPolicy, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Policy{}, fmt.Errorf("%w: resolve workspace root: %v", ErrInvalidPolicy, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return Policy{}, fmt.Errorf("%w: stat workspace root: %v", ErrInvalidPolicy, err)
	}
	if !info.IsDir() {
		return Policy{}, fmt.Errorf("%w: workspace root is not a directory", ErrInvalidPolicy)
	}
	return Policy{Root: filepath.Clean(resolved), Mode: mode}, nil
}

// Default returns the explicit default for an interactive agent. The app may
// override it from config; embedded test handlers use ModeOff deliberately.
func Default(root string) Policy {
	return Policy{Root: root, Mode: ModeAuto}
}

// Command returns a command with the requested capabilities reflected in the
// OS sandbox profile. allowWrite and allowNetwork must come from the already
// authorized agent run; they are not user-controlled command flags.
func (p Policy) Command(ctx context.Context, executable string, args []string, allowWrite, allowNetwork bool) (*exec.Cmd, Status, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p.Mode != ModeOff && p.Mode != ModeAuto && p.Mode != ModeRequired {
		return nil, "", fmt.Errorf("%w: unsupported mode %q", ErrInvalidPolicy, p.Mode)
	}
	if strings.TrimSpace(executable) == "" {
		return nil, "", fmt.Errorf("%w: executable is empty", ErrInvalidPolicy)
	}
	if p.Mode == ModeOff {
		cmd, err := toolpath.Command(ctx, executable, args...)
		return cmd, StatusDisabled, err
	}

	backend, err := p.backend()
	if err != nil {
		if p.Mode == ModeRequired {
			return nil, StatusUnavailable, err
		}
		cmd, commandErr := toolpath.Command(ctx, executable, args...)
		return cmd, StatusUnavailable, commandErr
	}
	profile, err := seatbeltProfile(p.Root, allowWrite, allowNetwork)
	if err != nil {
		return nil, StatusUnavailable, err
	}
	wrappedArgs := make([]string, 0, len(args)+4)
	wrappedArgs = append(wrappedArgs, "-p", profile, executable)
	wrappedArgs = append(wrappedArgs, args...)
	cmd, err := toolpath.Command(ctx, backend, wrappedArgs...)
	if err != nil {
		return nil, StatusUnavailable, fmt.Errorf("resolve sandbox backend: %w", err)
	}
	return cmd, StatusApplied, nil
}

func (p Policy) backend() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("%w: no Seatbelt backend for %s", ErrSandboxUnavailable, runtime.GOOS)
	}
	if p.SandboxExecutable != "" {
		if info, err := os.Stat(p.SandboxExecutable); err == nil && info.Mode()&0o111 != 0 {
			return p.SandboxExecutable, nil
		}
		return "", fmt.Errorf("%w: configured backend %q is not executable", ErrSandboxUnavailable, p.SandboxExecutable)
	}
	if info, err := os.Stat("/usr/bin/sandbox-exec"); err == nil && info.Mode()&0o111 != 0 {
		return "/usr/bin/sandbox-exec", nil
	}
	if resolved, err := toolpath.Resolve("sandbox-exec"); err == nil {
		return resolved, nil
	}
	return "", fmt.Errorf("%w: sandbox-exec was not found", ErrSandboxUnavailable)
}

func seatbeltProfile(root string, allowWrite, allowNetwork bool) (string, error) {
	if strings.TrimSpace(root) == "" || !filepath.IsAbs(root) {
		return "", fmt.Errorf("%w: sandbox root must be absolute", ErrInvalidPolicy)
	}
	root = filepath.Clean(root)
	quotedRoot := seatbeltQuote(root)
	var b strings.Builder
	b.WriteString("(version 1)\n")
	// system.sb supplies the dyld, syscall, /dev, and metadata rules required
	// for an ordinary macOS command to start. The workspace policy below then
	// adds the narrower project-specific rules; omitting this import causes
	// harmless commands such as /bin/sh to abort before their argv is run.
	b.WriteString(`(import "/System/Library/Sandbox/Profiles/system.sb")` + "\n")
	b.WriteString("(deny default)\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow signal)\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow mach-lookup)\n")
	b.WriteString("(allow ipc-posix-shm)\n")
	for _, path := range []string{"/usr", "/System", "/Library", "/private", "/opt", "/tmp"} {
		fmt.Fprintf(&b, "(allow file-read* (subpath %q))\n", path)
	}
	fmt.Fprintf(&b, "(allow file-read* (subpath %s))\n", quotedRoot)
	for _, path := range []string{"/dev/null", "/dev/random", "/dev/urandom"} {
		fmt.Fprintf(&b, "(allow file-read* (literal %q))\n", path)
	}
	fmt.Fprintln(&b, `(allow file-write* (literal "/dev/null"))`)
	if allowWrite {
		fmt.Fprintf(&b, "(allow file-write* (subpath %s))\n", quotedRoot)
	}
	if allowNetwork {
		b.WriteString("(allow network-outbound)\n")
	}
	return b.String(), nil
}

func seatbeltQuote(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}
