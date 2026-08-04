package execpolicy

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestPolicyOffDoesNotWrapCommand(t *testing.T) {
	policy, err := New(t.TempDir(), ModeOff)
	if err != nil {
		t.Fatal(err)
	}
	cmd, status, err := policy.Command(context.Background(), "echo", []string{"ok"}, true, true)
	if err != nil {
		t.Fatalf("Command() error = %v", err)
	}
	if status != StatusDisabled {
		t.Fatalf("Command() status = %q, want %q", status, StatusDisabled)
	}
	if len(cmd.Args) < 1 || strings.Contains(strings.Join(cmd.Args, "\x00"), "sandbox-exec") {
		t.Fatalf("off policy command = %#v, must not use sandbox-exec", cmd.Args)
	}
}

func TestPolicyRequiredFailsClosedWithoutBackend(t *testing.T) {
	policy, err := New(t.TempDir(), ModeRequired)
	if err != nil {
		t.Fatal(err)
	}
	policy.SandboxExecutable = "/path/that/does/not/exist"
	if _, _, err := policy.Command(context.Background(), "echo", []string{"nope"}, false, false); !errors.Is(err, ErrSandboxUnavailable) {
		t.Fatalf("required Command() error = %v, want ErrSandboxUnavailable", err)
	}
}

func TestPolicyRejectsUnknownMode(t *testing.T) {
	if _, err := New(t.TempDir(), Mode("unknown")); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("New() error = %v, want ErrInvalidPolicy", err)
	}
}

func TestSeatbeltProfileHonorsWorkspaceWriteAndNetwork(t *testing.T) {
	profile, err := seatbeltProfile(t.TempDir(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(profile, "(allow file-write* (subpath") {
		t.Fatalf("write-enabled profile = %q, missing workspace write rule", profile)
	}
	if strings.Contains(profile, "(allow network-outbound)") {
		t.Fatalf("network-disabled profile = %q, unexpectedly allows network", profile)
	}

	profile, err = seatbeltProfile(t.TempDir(), false, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(profile, "(allow file-write* (subpath") || !strings.Contains(profile, "(allow network-outbound)") {
		t.Fatalf("read/network profile = %q, want network-only capability", profile)
	}
}

func TestPolicyRequiredUsesSeatbeltOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Seatbelt backend is macOS-specific")
	}
	policy, err := New(t.TempDir(), ModeRequired)
	if err != nil {
		t.Fatal(err)
	}
	cmd, status, err := policy.Command(context.Background(), "/bin/echo", []string{"ok"}, false, false)
	if err != nil {
		t.Fatalf("required Darwin Command() error = %v", err)
	}
	if status != StatusApplied || len(cmd.Args) < 4 || cmd.Args[1] != "-p" {
		t.Fatalf("required Darwin command = %#v status=%q, want Seatbelt wrapper", cmd.Args, status)
	}
}

func TestPolicySeatbeltRunsAWorkspaceCommandOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Seatbelt backend is macOS-specific")
	}
	root := t.TempDir()
	policy, err := New(root, ModeRequired)
	if err != nil {
		t.Fatal(err)
	}
	allowed := root + "/allowed.txt"
	cmd, status, err := policy.Command(context.Background(), "/bin/sh", []string{"-c", "printf sandbox-ok > " + allowed + "; /bin/cat " + allowed}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusApplied {
		t.Fatalf("status = %q, want applied", status)
	}
	cmd.Dir = root
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		t.Fatalf("Seatbelt command failed: %v; output=%q", err, output.String())
	}
	if output.String() != "sandbox-ok" {
		t.Fatalf("Seatbelt output = %q, want sandbox-ok", output.String())
	}
	if data, err := os.ReadFile(allowed); err != nil || string(data) != "sandbox-ok" {
		t.Fatalf("workspace write = %q, err=%v; want sandbox-ok", data, err)
	}
}

func TestPolicySeatbeltDeniesWorkspaceEscapeOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Seatbelt backend is macOS-specific")
	}
	root := t.TempDir()
	outside := t.TempDir()
	policy, err := New(root, ModeRequired)
	if err != nil {
		t.Fatal(err)
	}
	cmd, _, err := policy.Command(context.Background(), "/bin/sh", []string{"-c", "printf blocked > " + outside + "/escape.txt"}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Dir = root
	if err := cmd.Run(); err == nil {
		t.Fatal("Seatbelt command escaped workspace write boundary")
	}
	if _, err := os.Stat(outside + "/escape.txt"); !os.IsNotExist(err) {
		t.Fatalf("outside escape file stat error = %v, want file absent", err)
	}
}

func TestPolicyCommandReturnsExecutableCommand(t *testing.T) {
	policy, err := New(t.TempDir(), ModeOff)
	if err != nil {
		t.Fatal(err)
	}
	cmd, _, err := policy.Command(context.Background(), "true", nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath(cmd.Path); err != nil {
		t.Fatalf("policy command path %q is not executable: %v", cmd.Path, err)
	}
}
