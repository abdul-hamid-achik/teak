package secrets

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"teak/internal/toolpath"
)

func newAgentFixture(t *testing.T) (*Resolver, net.Listener) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("tvault agent fixture uses a Unix socket")
	}
	dir, err := os.MkdirTemp("/tmp", "teak-tv-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	listener, err := net.Listen("unix", filepath.Join(dir, "agent.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return &Resolver{agentDir: dir}, listener
}

func serveAgentResponse(t *testing.T, listener net.Listener, response string) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = bufio.NewReader(conn).ReadString('\n')
		_, _ = io.WriteString(conn, response+"\n")
	}()
	return done
}

func TestGetUsesPromptFreeAgentResponse(t *testing.T) {
	resolver, listener := newAgentFixture(t)
	done := serveAgentResponse(t, listener, `{"value":"secret-value"}`)

	value, err := resolver.Get(context.Background(), "project", "API_KEY")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if value != "secret-value" {
		t.Fatalf("Get() = %q, want secret-value", value)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("agent fixture did not finish")
	}
}

func TestGetAllAndResolveEnvUseAgentResponse(t *testing.T) {
	resolver, listener := newAgentFixture(t)
	done := serveAgentResponse(t, listener, `{"secrets":{"API_KEY":"value","IGNORED":"other"}}`)

	env, err := resolver.ResolveEnv(context.Background(), "project", []string{"API_KEY"})
	if err != nil {
		t.Fatalf("ResolveEnv() error = %v", err)
	}
	if env["API_KEY"] != "value" || len(env) != 1 {
		t.Fatalf("ResolveEnv() = %#v, want only API_KEY", env)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("agent fixture did not finish")
	}
}

func TestGetHonorsContextWhenAgentDoesNotRespond(t *testing.T) {
	resolver, listener := newAgentFixture(t)
	connected := make(chan struct{})
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		close(connected)
		<-stop
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := resolver.Get(ctx, "project", "API_KEY")
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("Get() error = nil, want an error when the agent does not respond")
	}
	// The agent never responds, so Get must be bounded by the context
	// deadline — not the agent request timeout (1s) or the CLI timeout (5s).
	// Assert on elapsed time rather than the exact sentinel: at the deadline
	// boundary the socket read timeout and context expiry race, so Get may
	// return context.DeadlineExceeded directly or fall through to the CLI
	// (which also honors the context). Both honor the deadline.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Get() took %s, expected to honor the 100ms context deadline", elapsed)
	}
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("agent fixture never accepted connection")
	}
}

func TestAgentResponseIsBounded(t *testing.T) {
	resolver, listener := newAgentFixture(t)
	response := fmt.Sprintf(`{"value":"%s"}`, strings.Repeat("x", maxAgentResponse+10))
	done := serveAgentResponse(t, listener, response)

	_, err := resolver.agentGet(context.Background(), "project", "API_KEY")
	if err == nil {
		t.Fatal("agentGet() error = nil, want bounded response error")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("agent fixture did not finish")
	}
}

func writeTvaultFixture(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	path := filepath.Join(t.TempDir(), "tvault")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func configureTvault(t *testing.T, fixture string) {
	t.Helper()
	toolpath.Configure(map[string]string{"tvault": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })
}

func TestResolverFallsBackToCLIForGetAndGetAll(t *testing.T) {
	fixture := writeTvaultFixture(t, `
case "$1" in
  get) printf '%s\n' 'from-cli' ;;
  env) printf '%s\n' '{"API_KEY":"value","OTHER":"ignored"}' ;;
  *) exit 2 ;;
esac
`)
	configureTvault(t, fixture)
	resolver := &Resolver{agentDir: t.TempDir()}

	value, err := resolver.Get(context.Background(), "project", "API_KEY")
	if err != nil || value != "from-cli\n" {
		t.Fatalf("Get() = %q, %v, want CLI value", value, err)
	}
	env, err := resolver.ResolveEnv(context.Background(), "project", []string{"API_KEY", "MISSING"})
	if err != nil {
		t.Fatalf("ResolveEnv() error = %v", err)
	}
	if len(env) != 1 || env["API_KEY"] != "value" {
		t.Fatalf("ResolveEnv() = %#v, want only API_KEY", env)
	}
}

func TestResolverReportsCLIAndJSONErrors(t *testing.T) {
	fixture := writeTvaultFixture(t, `
case "$1" in
  get) printf '%s\n' 'permission denied' >&2; exit 7 ;;
  env) printf '%s\n' 'not-json' ;;
esac
`)
	configureTvault(t, fixture)
	resolver := &Resolver{agentDir: t.TempDir()}

	if _, err := resolver.Get(context.Background(), "project", "API_KEY"); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Get() error = %v, want CLI stderr detail", err)
	}
	if _, err := resolver.GetAll(context.Background(), "project"); err == nil || !strings.Contains(err.Error(), "parse tvault env") {
		t.Fatalf("GetAll() error = %v, want JSON parse error", err)
	}
}

func TestResolverAvailabilityAndNilInputs(t *testing.T) {
	configureTvault(t, filepath.Join(t.TempDir(), "missing-tvault"))
	resolver := &Resolver{agentDir: t.TempDir()}
	if resolver.Available() {
		t.Fatal("Available() = true for missing agent and CLI")
	}
	if env, err := resolver.ResolveEnv(context.Background(), "", []string{"KEY"}); err != nil || env != nil {
		t.Fatalf("ResolveEnv(empty project) = %#v, %v, want nil, nil", env, err)
	}
	if env, err := resolver.ResolveEnv(context.Background(), "project", nil); err != nil || env != nil {
		t.Fatalf("ResolveEnv(empty keys) = %#v, %v, want nil, nil", env, err)
	}
}
