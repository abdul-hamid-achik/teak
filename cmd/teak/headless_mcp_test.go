package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHeadlessMCPInitializeAndListTools(t *testing.T) {
	server := newHeadlessMCPServer("/workspace", func(context.Context, []string) ([]byte, []byte, int, error) {
		t.Fatal("tools/list must not execute a headless subprocess")
		return nil, nil, 0, nil
	})
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	}, "\n") + "\n"
	var output, stderr bytes.Buffer
	if err := server.serve(context.Background(), strings.NewReader(input), &output, &stderr); err != nil {
		t.Fatalf("serve() error = %v; stderr = %s", err, stderr.String())
	}

	responses := decodeMCPResponses(t, output.Bytes())
	if len(responses) != 2 {
		t.Fatalf("responses = %d, want initialize and tools/list", len(responses))
	}
	byID := make(map[string]headlessMCPResponse, len(responses))
	for _, response := range responses {
		byID[string(response.ID)] = response
	}
	initializeResponse := byID["1"]
	if got := initializeResponse.Result.(map[string]any)["protocolVersion"]; got != headlessMCPProtocolVersion {
		t.Fatalf("protocolVersion = %#v, want %q", got, headlessMCPProtocolVersion)
	}
	result, ok := byID["2"].Result.(map[string]any)
	if !ok {
		t.Fatalf("tools/list result = %#v", responses[1].Result)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) < 8 {
		t.Fatalf("tools/list tools = %#v, want at least 8 bounded tools", result["tools"])
	}
}

func TestHeadlessMCPToolSchemasDeclareRequiredArguments(t *testing.T) {
	want := map[string][]string{
		"teak_project_stat":      {"path"},
		"teak_project_mkdir":     {"path", "confirm"},
		"teak_project_rename":    {"source", "destination", "confirm"},
		"teak_project_copy":      {"source", "destination", "confirm"},
		"teak_project_remove":    {"path", "confirm"},
		"teak_buffer_read":       {"path"},
		"teak_buffer_write":      {"path", "expected_sha256", "content", "confirm"},
		"teak_search":            {"query"},
		"teak_codemap":           {"operation", "symbol"},
		"teak_codemap_symbols":   {"path"},
		"teak_codemap_symbol_at": {"path", "line"},
		"teak_hitspec_validate":  {"path"},
		"teak_lsp_diagnostics":   {"path"},
		"teak_lsp_format":        {"path"},
		"teak_lsp_symbols":       {"path"},
		"teak_lsp_hover":         {"path", "line", "column"},
		"teak_lsp_definition":    {"path", "line", "column"},
		"teak_lsp_references":    {"path", "line", "column"},
		"teak_agent_show":        {"run_id"},
		"teak_agent_cancel":      {"run_id", "confirm"},
		"teak_agent_reap_stale":  {"max_silence", "confirm"},
	}

	seen := make(map[string]bool)
	for _, tool := range headlessMCPTools() {
		seen[tool.Name] = true
		raw, present := tool.InputSchema["required"]
		if expected, ok := want[tool.Name]; ok {
			if !present {
				t.Fatalf("tool %q has no required schema fields", tool.Name)
			}
			got, ok := raw.([]string)
			if !ok || strings.Join(got, "\x00") != strings.Join(expected, "\x00") {
				t.Errorf("tool %q required = %#v, want %#v", tool.Name, raw, expected)
			}
		} else if present {
			t.Errorf("tool %q declares unexpected required fields %#v", tool.Name, raw)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("required-schema tool %q is missing from tools/list", name)
		}
	}
	for _, tool := range headlessMCPTools() {
		if tool.Name != "teak_search" {
			continue
		}
		properties, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatal("teak_search schema has no properties")
		}
		for _, key := range []string{"index", "confirm"} {
			if _, ok := properties[key]; !ok {
				t.Fatalf("teak_search schema is missing explicit index control %q", key)
			}
		}
		return
	}
	t.Fatal("teak_search schema was not found")
}

func TestHeadlessMCPRejectsNonScalarRequestIDs(t *testing.T) {
	server := newHeadlessMCPServer("/workspace", nil)
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":true,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":[],"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":"valid","method":"initialize","params":{}}`,
	}, "\n") + "\n"
	var output, stderr bytes.Buffer
	if err := server.serve(context.Background(), strings.NewReader(input), &output, &stderr); err != nil {
		t.Fatalf("serve() error = %v; stderr = %s", err, stderr.String())
	}
	responses := decodeMCPResponses(t, output.Bytes())
	if len(responses) != 3 {
		t.Fatalf("responses = %d, want one response per request", len(responses))
	}
	for _, response := range responses[:2] {
		if response.Error == nil || response.Error.Code != headlessMCPInvalidRequestCode {
			t.Fatalf("invalid ID response = %#v, want invalid request", response)
		}
		if string(response.ID) != "null" {
			t.Fatalf("invalid ID response id = %s, want null", response.ID)
		}
	}
	if responses[2].Error != nil || string(responses[2].ID) != `"valid"` {
		t.Fatalf("valid ID response = %#v, want successful string ID", responses[2])
	}
}

func TestHeadlessMCPServeUnblocksWhenContextIsCancelled(t *testing.T) {
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	defer func() { _ = reader.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := newHeadlessMCPServer("/workspace", nil)
	serveDone := make(chan error, 1)
	var output, stderr bytes.Buffer
	go func() { serveDone <- server.serve(ctx, reader, &output, &stderr) }()

	cancel()
	select {
	case err := <-serveDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("serve() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve() stayed blocked after context cancellation")
	}
}

func TestRunHeadlessMCPContextCancellationStopsTransport(t *testing.T) {
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	defer func() { _ = reader.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var output, stderr bytes.Buffer
	result := make(chan int, 1)
	go func() {
		result <- runHeadlessMCPContext(ctx, []string{"--root", t.TempDir()}, reader, &output, &stderr)
	}()

	cancel()
	select {
	case code := <-result:
		if code != 1 {
			t.Fatalf("cancelled MCP exit code = %d, want 1; stderr=%q", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "context canceled") {
			t.Fatalf("cancelled MCP stderr = %q, want context cancellation", stderr.String())
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled MCP transport did not stop promptly")
	}
}

func TestHeadlessMCPToolCallUsesFixedWorkspaceAndBoundedArguments(t *testing.T) {
	var mu sync.Mutex
	var gotArgs []string
	server := newHeadlessMCPServer("/fixed/workspace", func(_ context.Context, args []string) ([]byte, []byte, int, error) {
		mu.Lock()
		gotArgs = append([]string(nil), args...)
		mu.Unlock()
		return []byte(`{"workspace":"/fixed/workspace","state":"ready","entries":[]}`), nil, 0, nil
	})
	input := `{"jsonrpc":"2.0","id":"ctx","method":"tools/call","params":{"name":"teak_context","arguments":{"depth":2}}}` + "\n"
	reader, writer := io.Pipe()
	output := &mcpSignalWriter{ready: make(chan struct{})}
	var stderr bytes.Buffer
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.serve(context.Background(), reader, output, &stderr) }()
	go func() { _, _ = writer.Write([]byte(input)) }()
	select {
	case <-output.ready:
		_ = writer.Close()
	case <-time.After(2 * time.Second):
		_ = writer.Close()
		t.Fatal("timed out waiting for MCP tool response")
	}
	if err := <-serveDone; err != nil {
		t.Fatalf("serve() error = %v; stderr = %s", err, stderr.String())
	}
	responses := decodeMCPResponses(t, output.Bytes())
	if len(responses) != 1 || responses[0].Error != nil {
		t.Fatalf("tool response = %#v, error = %#v", responses, responses[0].Error)
	}
	result, ok := responses[0].Result.(map[string]any)
	if !ok || result["isError"] == true {
		t.Fatalf("tool result = %#v", responses[0].Result)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) != 1 || !strings.Contains(content[0].(map[string]any)["text"].(string), "fixed/workspace") {
		t.Fatalf("tool content = %#v", result["content"])
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"context", "--json", "--root", "/fixed/workspace", "--depth", "2"}
	if strings.Join(gotArgs, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("headless args = %#v, want %#v", gotArgs, want)
	}
}

func TestHeadlessMCPConfirmedBufferWritePassesBoundedInput(t *testing.T) {
	server := newHeadlessMCPServer("/fixed/workspace", func(context.Context, []string) ([]byte, []byte, int, error) {
		t.Fatal("confirmed buffer write must use the input runner")
		return nil, nil, 0, nil
	})
	var gotArgs []string
	var gotInput []byte
	server.runInput = func(_ context.Context, args []string, input []byte) ([]byte, []byte, int, error) {
		gotArgs = append([]string(nil), args...)
		gotInput = append([]byte(nil), input...)
		return []byte(`{"path":"main.go","content":"updated\n"}`), nil, 0, nil
	}

	result, mcpErr := server.callTool(context.Background(), json.RawMessage(`{"name":"teak_buffer_write","arguments":{"path":"main.go","expected_sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","content":"updated\n","confirm":true}}`))
	if mcpErr != nil {
		t.Fatalf("callTool() error = %#v", mcpErr)
	}
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("callTool() result = %#v, want one successful content block", result)
	}
	wantArgs := []string{"buffer", "write", "--json", "--root", "/fixed/workspace", "--expected-sha256", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "main.go"}
	if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("headless args = %#v, want %#v", gotArgs, wantArgs)
	}
	if string(gotInput) != "updated\n" {
		t.Fatalf("input = %q, want updated newline", gotInput)
	}
}

func TestHeadlessMCPRejectsUnknownAndUnconfirmedTools(t *testing.T) {
	called := false
	server := newHeadlessMCPServer("/workspace", func(context.Context, []string) ([]byte, []byte, int, error) {
		called = true
		return nil, nil, 0, nil
	})
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"teak_buffer_write","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"unknown","arguments":{}}}`,
	}, "\n") + "\n"
	var output, stderr bytes.Buffer
	if err := server.serve(context.Background(), strings.NewReader(input), &output, &stderr); err != nil {
		t.Fatalf("serve() error = %v; stderr = %s", err, stderr.String())
	}
	responses := decodeMCPResponses(t, output.Bytes())
	if len(responses) != 2 || responses[0].Error == nil || responses[1].Error == nil {
		t.Fatalf("responses = %#v, want invalid params errors", responses)
	}
	if called {
		t.Fatal("invalid or mutating tools reached the headless runner")
	}
}

func TestHeadlessMCPCancelledCallCancelsSubprocessContext(t *testing.T) {
	started := make(chan struct{})
	server := newHeadlessMCPServer("/workspace", func(ctx context.Context, _ []string) ([]byte, []byte, int, error) {
		close(started)
		<-ctx.Done()
		return nil, nil, 0, ctx.Err()
	})
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"teak_search","arguments":{"query":"needle"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7}}`,
	}, "\n") + "\n"
	var output, stderr bytes.Buffer
	if err := server.serve(context.Background(), strings.NewReader(input), &output, &stderr); err != nil {
		t.Fatalf("serve() error = %v; stderr = %s", err, stderr.String())
	}
	select {
	case <-started:
	default:
		t.Fatal("tool runner did not start")
	}
	responses := decodeMCPResponses(t, output.Bytes())
	if len(responses) != 1 || responses[0].Error == nil || responses[0].Error.Code != headlessMCPRequestCancelledCode {
		t.Fatalf("cancel response = %#v, want cancellation error", responses)
	}
}

func TestHeadlessMCPRejectsDuplicateActiveRequestIDs(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := newHeadlessMCPServer("/workspace", func(ctx context.Context, _ []string) ([]byte, []byte, int, error) {
		close(started)
		select {
		case <-release:
			return []byte(`{"workspace":"/workspace","state":"ready"}`), nil, 0, nil
		case <-ctx.Done():
			return nil, nil, 0, ctx.Err()
		}
	})
	reader, writer := io.Pipe()
	output := &mcpSignalWriter{ready: make(chan struct{})}
	var stderr bytes.Buffer
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.serve(context.Background(), reader, output, &stderr) }()

	first := `{"jsonrpc":"2.0","id":"same","method":"tools/call","params":{"name":"teak_search","arguments":{"query":"first"}}}` + "\n"
	if _, err := writer.Write([]byte(first)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first MCP request did not start")
	}
	duplicate := `{"jsonrpc":"2.0","id":"same","method":"tools/call","params":{"name":"teak_search","arguments":{"query":"duplicate"}}}` + "\n"
	if _, err := writer.Write([]byte(duplicate)); err != nil {
		t.Fatal(err)
	}
	waitForMCPOutput(t, output, "already active")
	close(release)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; err != nil {
		t.Fatalf("serve() error = %v; stderr = %s", err, stderr.String())
	}

	responses := decodeMCPResponses(t, output.Bytes())
	var foundDuplicate bool
	for _, response := range responses {
		if response.Error != nil && response.Error.Code == headlessMCPInvalidRequestCode && strings.Contains(response.Error.Message, "already active") {
			foundDuplicate = true
		}
	}
	if !foundDuplicate {
		t.Fatalf("responses = %#v, want duplicate active ID error", responses)
	}
}

func TestHeadlessMCPRejectsRequestsOverServerQuota(t *testing.T) {
	server := newHeadlessMCPServer("/workspace", func(context.Context, []string) ([]byte, []byte, int, error) {
		t.Fatal("quota-rejected request reached the headless runner")
		return nil, nil, 0, nil
	})
	server.quota = newHeadlessQuota(1, headlessMCPMaxOutputBytes+headlessMCPMaxErrorBytes)
	release, err := server.quota.acquire(context.Background(), headlessMCPMaxOutputBytes+headlessMCPMaxErrorBytes)
	if err != nil {
		t.Fatalf("reserve MCP quota: %v", err)
	}
	defer release()

	input := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"teak_search","arguments":{"query":"needle"}}}` + "\n"
	var output, stderr bytes.Buffer
	if err := server.serve(context.Background(), strings.NewReader(input), &output, &stderr); err != nil {
		t.Fatalf("serve() error = %v; stderr = %s", err, stderr.String())
	}
	responses := decodeMCPResponses(t, output.Bytes())
	if len(responses) != 1 || responses[0].Error == nil {
		t.Fatalf("responses = %#v, want one quota error", responses)
	}
	if responses[0].Error.Code != headlessMCPQuotaExceededCode || !strings.Contains(responses[0].Error.Message, "quota") {
		t.Fatalf("quota response = %#v, want quota error", responses[0].Error)
	}
}

func TestHeadlessMCPToolArgumentsCoverSurface(t *testing.T) {
	server := newHeadlessMCPServer("/workspace", nil)
	tests := []struct {
		name string
		tool string
		args string
		want []string
	}{
		{name: "context", tool: "teak_context", args: `{"depth":4}`, want: []string{"context", "--json", "--root", "/workspace", "--depth", "4"}},
		{name: "project list", tool: "teak_project_list", args: `{}`, want: []string{"project", "list", "--json", "--root", "/workspace"}},
		{name: "project stat", tool: "teak_project_stat", args: `{"path":"internal"}`, want: []string{"project", "stat", "--json", "--root", "/workspace", "internal"}},
		{name: "confirmed project mkdir", tool: "teak_project_mkdir", args: `{"path":"scratch","confirm":true}`, want: []string{"project", "mkdir", "--json", "--root", "/workspace", "--confirm", "scratch"}},
		{name: "confirmed project rename", tool: "teak_project_rename", args: `{"source":"scratch","destination":"moved","confirm":true}`, want: []string{"project", "rename", "--json", "--root", "/workspace", "--confirm", "scratch", "moved"}},
		{name: "confirmed project copy", tool: "teak_project_copy", args: `{"source":"moved","destination":"copied","confirm":true}`, want: []string{"project", "copy", "--json", "--root", "/workspace", "--confirm", "moved", "copied"}},
		{name: "confirmed project remove", tool: "teak_project_remove", args: `{"path":"copied","confirm":true}`, want: []string{"project", "remove", "--json", "--root", "/workspace", "--confirm", "copied"}},
		{name: "buffer", tool: "teak_buffer_read", args: `{"path":"main.go"}`, want: []string{"buffer", "read", "--json", "--root", "/workspace", "main.go"}},
		{name: "confirmed buffer write", tool: "teak_buffer_write", args: `{"path":"main.go","expected_sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","content":"updated\n","confirm":true}`, want: []string{"buffer", "write", "--json", "--root", "/workspace", "--expected-sha256", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "main.go"}},
		{name: "search", tool: "teak_search", args: `{"query":"needle","regex":true,"case_sensitive":true,"semantic":true}`, want: []string{"search", "--json", "--root", "/workspace", "--regex", "--case-sensitive", "--semantic", "needle"}},
		{name: "confirmed semantic index", tool: "teak_search", args: `{"query":"needle","semantic":true,"index":true,"confirm":true}`, want: []string{"search", "--json", "--root", "/workspace", "--semantic", "--index", "needle"}},
		{name: "codemap", tool: "teak_codemap", args: `{"operation":"impact","symbol":"Main","depth":3}`, want: []string{"codemap", "impact", "--json", "--root", "/workspace", "--depth", "3", "Main"}},
		{name: "codemap symbols", tool: "teak_codemap_symbols", args: `{"path":"main.go"}`, want: []string{"codemap", "symbols", "--json", "--root", "/workspace", "main.go"}},
		{name: "codemap symbol at", tool: "teak_codemap_symbol_at", args: `{"path":"main.go","line":3}`, want: []string{"codemap", "symbol-at", "--json", "--root", "/workspace", "--line", "3", "main.go"}},
		{name: "tools", tool: "teak_tools_status", args: `{}`, want: []string{"tools", "status", "--json", "--root", "/workspace"}},
		{name: "health", tool: "teak_health", args: `{}`, want: []string{"health", "--json", "--root", "/workspace"}},
		{name: "health dashboard", tool: "teak_health_dashboard", args: `{"limit":1}`, want: []string{"health", "dashboard", "--json", "--root", "/workspace", "--limit", "1"}},
		{name: "health history", tool: "teak_health_history", args: `{"limit":1}`, want: []string{"health", "history", "--json", "--root", "/workspace", "--limit", "1"}},
		{name: "hitspec", tool: "teak_hitspec_validate", args: `{"path":"api.http"}`, want: []string{"hitspec", "validate", "--json", "--root", "/workspace", "api.http"}},
		{name: "git", tool: "teak_git_status", args: `{}`, want: []string{"git", "status", "--json", "--root", "/workspace"}},
		{name: "session", tool: "teak_session_show", args: `{"name":"review"}`, want: []string{"session", "show", "--json", "--root", "/workspace", "--name", "review"}},
		{name: "lsp", tool: "teak_lsp_status", args: `{}`, want: []string{"lsp", "status", "--json", "--root", "/workspace"}},
		{name: "lsp protocol probe", tool: "teak_lsp_status", args: `{"probe":true}`, want: []string{"lsp", "status", "--json", "--root", "/workspace", "--probe"}},
		{name: "lsp diagnostics", tool: "teak_lsp_diagnostics", args: `{"path":"main.go"}`, want: []string{"lsp", "diagnostics", "--json", "--root", "/workspace", "main.go"}},
		{name: "lsp format", tool: "teak_lsp_format", args: `{"path":"main.go"}`, want: []string{"lsp", "format", "--json", "--root", "/workspace", "main.go"}},
		{name: "lsp symbols", tool: "teak_lsp_symbols", args: `{"path":"main.go"}`, want: []string{"lsp", "symbols", "--json", "--root", "/workspace", "main.go"}},
		{name: "lsp hover", tool: "teak_lsp_hover", args: `{"path":"main.go","line":2,"column":4}`, want: []string{"lsp", "hover", "--json", "--root", "/workspace", "--line", "2", "--column", "4", "main.go"}},
		{name: "lsp definition", tool: "teak_lsp_definition", args: `{"path":"main.go","line":2,"column":4}`, want: []string{"lsp", "definition", "--json", "--root", "/workspace", "--line", "2", "--column", "4", "main.go"}},
		{name: "lsp references", tool: "teak_lsp_references", args: `{"path":"main.go","line":2,"column":4}`, want: []string{"lsp", "references", "--json", "--root", "/workspace", "--line", "2", "--column", "4", "main.go"}},
		{name: "dap", tool: "teak_dap_status", args: `{}`, want: []string{"dap", "status", "--json", "--root", "/workspace"}},
		{name: "dap probe", tool: "teak_dap_probe", args: `{"adapter":"dlv"}`, want: []string{"dap", "probe", "--json", "--root", "/workspace", "--adapter", "dlv"}},
		{name: "session list", tool: "teak_session_list", args: `{}`, want: []string{"session", "list", "--json", "--root", "/workspace"}},
		{name: "session health", tool: "teak_session_health", args: `{"name":"review"}`, want: []string{"session", "health", "--json", "--root", "/workspace", "--name", "review"}},
		{name: "agent", tool: "teak_agent_list", args: `{}`, want: []string{"agent", "list", "--json", "--root", "/workspace"}},
		{name: "agent show", tool: "teak_agent_show", args: `{"run_id":"run-1"}`, want: []string{"agent", "show", "--json", "--root", "/workspace", "run-1"}},
		{name: "agent cancel", tool: "teak_agent_cancel", args: `{"run_id":"run-1","confirm":true}`, want: []string{"agent", "cancel", "--json", "--root", "/workspace", "--confirm", "run-1"}},
		{name: "agent reap stale", tool: "teak_agent_reap_stale", args: `{"max_silence":"1m","confirm":true}`, want: []string{"agent", "reap-stale", "--json", "--root", "/workspace", "--confirm", "--max-silence", "1m"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := server.toolArgs(tt.tool, json.RawMessage(tt.args))
			if err != nil {
				t.Fatalf("toolArgs() error = %v", err)
			}
			if strings.Join(got, "\x00") != strings.Join(tt.want, "\x00") {
				t.Fatalf("toolArgs() = %#v, want %#v", got, tt.want)
			}
		})
	}

	invalid := []struct {
		name string
		tool string
		args string
	}{
		{name: "unconfirmed buffer write", tool: "teak_buffer_write", args: `{"path":"main.go","expected_sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","content":"updated\n"}`},
		{name: "unconfirmed project mkdir", tool: "teak_project_mkdir", args: `{"path":"scratch"}`},
		{name: "project rename missing destination", tool: "teak_project_rename", args: `{"source":"scratch","confirm":true}`},
		{name: "project remove rejects destination", tool: "teak_project_remove", args: `{"path":"scratch","destination":"other","confirm":true}`},
		{name: "unknown argument", tool: "teak_context", args: `{"limit":1}`},
		{name: "invalid depth", tool: "teak_context", args: `{"depth":5}`},
		{name: "missing path", tool: "teak_buffer_read", args: `{}`},
		{name: "invalid bool", tool: "teak_search", args: `{"query":"x","regex":"yes"}`},
		{name: "unconfirmed semantic index", tool: "teak_search", args: `{"query":"x","semantic":true,"index":true}`},
		{name: "index without semantic", tool: "teak_search", args: `{"query":"x","index":true,"confirm":true}`},
		{name: "invalid operation", tool: "teak_codemap", args: `{"operation":"index","symbol":"Main"}`},
		{name: "codemap symbols absolute path", tool: "teak_codemap_symbols", args: `{"path":"/main.go"}`},
		{name: "codemap symbol at missing line", tool: "teak_codemap_symbol_at", args: `{"path":"main.go"}`},
		{name: "lsp hover missing position", tool: "teak_lsp_hover", args: `{"path":"main.go","line":1}`},
		{name: "lsp references negative position", tool: "teak_lsp_references", args: `{"path":"main.go","line":-1,"column":0}`},
		{name: "unconfirmed agent cancel", tool: "teak_agent_cancel", args: `{"run_id":"run-1"}`},
		{name: "unconfirmed agent reap stale", tool: "teak_agent_reap_stale", args: `{"max_silence":"1m"}`},
		{name: "invalid arguments", tool: "teak_search", args: `[]`},
		{name: "oversized argument", tool: "teak_search", args: `{"query":"` + strings.Repeat("x", headlessMCPMaxArgumentBytes) + `"}`},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := server.toolArgs(tt.tool, json.RawMessage(tt.args)); err == nil {
				t.Fatal("toolArgs() succeeded for invalid arguments")
			}
		})
	}
}

func TestHeadlessMCPProtocolErrorsAndCLIValidation(t *testing.T) {
	server := newHeadlessMCPServer("/workspace", nil)
	input := strings.Join([]string{
		"not-json",
		`{"jsonrpc":"1.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":2,"method":"unknown"}`,
	}, "\n") + "\n"
	var output, stderr bytes.Buffer
	if err := server.serve(context.Background(), strings.NewReader(input), &output, &stderr); err != nil {
		t.Fatalf("serve() error = %v; stderr = %s", err, stderr.String())
	}
	responses := decodeMCPResponses(t, output.Bytes())
	if len(responses) != 3 {
		t.Fatalf("protocol error responses = %d, want 3", len(responses))
	}
	if responses[0].Error == nil || responses[0].Error.Code != headlessMCPParseErrorCode {
		t.Fatalf("parse response = %#v", responses[0].Error)
	}
	if responses[1].Error == nil || responses[1].Error.Code != headlessMCPInvalidRequestCode {
		t.Fatalf("invalid request response = %#v", responses[1].Error)
	}
	if responses[2].Error == nil || responses[2].Error.Code != headlessMCPMethodNotFoundCode {
		t.Fatalf("method response = %#v", responses[2].Error)
	}

	for _, args := range [][]string{
		{"--json", "--root", t.TempDir()},
		{"--listen", "127.0.0.1:0", "--root", t.TempDir()},
	} {
		var stdout, stderr bytes.Buffer
		if code := runHeadlessMCP(args, strings.NewReader(""), &stdout, &stderr); code == 0 {
			t.Fatalf("runHeadlessMCP(%v) succeeded", args)
		}
	}
	var finalStdout, finalStderr bytes.Buffer
	if code := runHeadlessMCP([]string{"--root", t.TempDir()}, strings.NewReader(""), &finalStdout, &finalStderr); code != 0 {
		t.Fatalf("empty MCP session code = %d, stderr = %s", code, finalStderr.String())
	}
}

func decodeMCPResponses(t *testing.T, data []byte) []headlessMCPResponse {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var responses []headlessMCPResponse
	for {
		var response headlessMCPResponse
		if err := decoder.Decode(&response); errors.Is(err, context.Canceled) {
			t.Fatal(err)
		} else if err != nil {
			if errors.Is(err, io.EOF) {
				return responses
			}
			t.Fatalf("decode MCP response: %v; data = %s", err, data)
		}
		responses = append(responses, response)
	}
}

type mcpSignalWriter struct {
	mu    sync.Mutex
	data  bytes.Buffer
	ready chan struct{}
}

func (w *mcpSignalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.data.Write(p)
	select {
	case <-w.ready:
	default:
		close(w.ready)
	}
	return n, err
}

func (w *mcpSignalWriter) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.data.Bytes()...)
}

func waitForMCPOutput(t *testing.T, writer *mcpSignalWriter, want string) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if bytes.Contains(writer.Bytes(), []byte(want)) {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for MCP output %q; got %s", want, writer.Bytes())
		case <-ticker.C:
		}
	}
}
