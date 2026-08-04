package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	agentruntime "teak/internal/agent/runtime"
	"teak/internal/toolpath"
)

func TestHeadlessRESTRequiresTokenAndServesBoundedContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newHeadlessRESTHandler(root, "test-token")

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/context", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/context?depth=0", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("context status = %d, body = %s", response.Code, response.Body.String())
	}
	var contextResponse headlessContextResponse
	if err := json.Unmarshal(response.Body.Bytes(), &contextResponse); err != nil {
		t.Fatalf("decode context response: %v; body = %s", err, response.Body.String())
	}
	if contextResponse.Workspace != root || len(contextResponse.Entries) != 1 || contextResponse.Entries[0].Path != "main.go" {
		t.Fatalf("context response = %#v, want root and main.go", contextResponse)
	}
	var envelope struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.SchemaVersion != headlessSchemaVersion {
		t.Fatalf("REST schema_version = %d, want %d (err=%v)", envelope.SchemaVersion, headlessSchemaVersion, err)
	}

	// The server workspace is fixed at startup; a caller cannot escape it by
	// supplying a different root in the request query.
	escape := httptest.NewRequest(http.MethodGet, "/v1/context?root=/", nil)
	escape.Header.Set("Authorization", "Bearer test-token")
	escapeResponse := httptest.NewRecorder()
	handler.ServeHTTP(escapeResponse, escape)
	var escapedContext headlessContextResponse
	if err := json.Unmarshal(escapeResponse.Body.Bytes(), &escapedContext); err != nil {
		t.Fatalf("decode fixed-root response: %v; body = %s", err, escapeResponse.Body.String())
	}
	if escapedContext.Workspace != root {
		t.Fatalf("fixed workspace = %q, want %q", escapedContext.Workspace, root)
	}
}

func TestHeadlessRESTServerHasBoundedConnectionTimeouts(t *testing.T) {
	server := newHeadlessRESTHTTPServer(http.NotFoundHandler())
	if server.ReadHeaderTimeout != headlessRESTReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, headlessRESTReadHeaderTimeout)
	}
	if server.ReadTimeout != headlessRESTReadTimeout {
		t.Fatalf("ReadTimeout = %s, want %s", server.ReadTimeout, headlessRESTReadTimeout)
	}
	if server.WriteTimeout != headlessRESTWriteTimeout {
		t.Fatalf("WriteTimeout = %s, want %s", server.WriteTimeout, headlessRESTWriteTimeout)
	}
	if server.IdleTimeout != headlessRESTIdleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", server.IdleTimeout, headlessRESTIdleTimeout)
	}
	if server.MaxHeaderBytes != headlessRESTMaxHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, headlessRESTMaxHeaderBytes)
	}
}

func TestHeadlessRESTRejectsRequestsOverServerQuota(t *testing.T) {
	root := t.TempDir()
	server := &headlessRESTServer{
		root:             root,
		token:            "test-token",
		workspaces:       map[string]string{"default": root},
		defaultWorkspace: "default",
		quota:            newHeadlessQuota(1, headlessRESTOperationOutputReservation+headlessRESTErrorOutputLimit),
	}
	release, err := server.quota.acquire(context.Background(), headlessRESTOperationOutputReservation+headlessRESTErrorOutputLimit)
	if err != nil {
		t.Fatalf("reserve REST quota: %v", err)
	}
	defer release()

	request := httptest.NewRequest(http.MethodGet, "/v1/context", nil)
	request.Header.Set("X-Teak-Token", "test-token")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("quota status = %d, body = %s; want %d", response.Code, response.Body.String(), http.StatusTooManyRequests)
	}
	var payload headlessRESTErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode quota response: %v; body = %s", err, response.Body.String())
	}
	if payload.Code != "quota_exceeded" || payload.State != "error" {
		t.Fatalf("quota response = %#v, want quota_exceeded error", payload)
	}
}

func TestHeadlessRESTHealthzReportsQuota(t *testing.T) {
	root := t.TempDir()
	handler := newHeadlessRESTHandler(root, "test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload headlessRESTHealthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode healthz response: %v; body = %s", err, response.Body.String())
	}
	if payload.Quota.MaxConcurrent != headlessMaxConcurrentOperations || payload.Quota.MaxOutputBytes <= 0 || payload.Quota.Active != 0 {
		t.Fatalf("healthz quota = %#v, want configured idle quota", payload.Quota)
	}
}

func TestHeadlessRESTReadOnlyRoutesReuseJSONContracts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newHeadlessRESTHandler(root, "test-token")

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "buffer", path: "/v1/buffer/read?path=notes.txt", want: "needle"},
		{name: "search", path: "/v1/search?q=needle", want: "\"mode\": \"text\""},
		{name: "health", path: "/v1/health", want: "\"summary\""},
		{name: "health dashboard", path: "/v1/health/dashboard?limit=1", want: "\"trend\""},
		{name: "health history", path: "/v1/health/history?limit=1", want: "\"snapshots\""},
		{name: "git", path: "/v1/git/status", want: "\"state\""},
		{name: "tools", path: "/v1/tools/status", want: "\"tools\""},
		{name: "session list", path: "/v1/session/list", want: "\"names\""},
		{name: "session health", path: "/v1/session/health", want: "\"sessions\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.Header.Set("X-Teak-Token", "test-token")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), tt.want) {
				t.Fatalf("body = %s, want substring %q", response.Body.String(), tt.want)
			}
		})
	}
}

func TestHeadlessRESTDoesNotReportStructuredOperationFailureAsOK(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "api.http"), []byte("GET https://example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A broken hitspec binary still produces a useful structured headless
	// response, but the operation exits non-zero. REST must preserve that
	// failure at the transport layer instead of returning HTTP 200.
	hitspec := filepath.Join(t.TempDir(), "broken-hitspec")
	if err := os.WriteFile(hitspec, []byte("#!/bin/sh\nprintf '%s\\n' 'not-json'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"hitspec": hitspec})
	t.Cleanup(func() { toolpath.Configure(nil) })

	request := httptest.NewRequest(http.MethodGet, "/v1/hitspec/validate?path=api.http", nil)
	request.Header.Set("X-Teak-Token", "test-token")
	response := httptest.NewRecorder()
	newHeadlessRESTHandler(root, "test-token").ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("structured failure status = %d, body = %s; want %d", response.Code, response.Body.String(), http.StatusUnprocessableEntity)
	}
	var payload headlessHitspecValidationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode structured failure: %v; body = %s", err, response.Body.String())
	}
	if payload.State != "failed" {
		t.Fatalf("structured failure state = %q, want failed", payload.State)
	}
}

func TestHeadlessRESTRouteArgsExposeReadOnlyCLIContracts(t *testing.T) {
	server := &headlessRESTServer{root: "/workspace"}
	tests := []struct {
		name string
		path string
		want []string
	}{
		{name: "session list", path: "/v1/session/list", want: []string{"session", "list", "--json", "--root", "/workspace"}},
		{name: "session health", path: "/v1/session/health?name=review", want: []string{"session", "health", "--json", "--root", "/workspace", "--name", "review"}},
		{name: "agent show", path: "/v1/agent/show?run_id=run-1", want: []string{"agent", "show", "--json", "--root", "/workspace", "run-1"}},
		{name: "agent cancel", path: "/v1/agent/cancel?run_id=run-1", want: []string{"agent", "cancel", "--json", "--root", "/workspace", "--confirm", "run-1"}},
		{name: "agent reap stale", path: "/v1/agent/reap-stale?max_silence=1m", want: []string{"agent", "reap-stale", "--json", "--root", "/workspace", "--confirm", "--max-silence", "1m"}},
		{name: "lsp status", path: "/v1/lsp/status", want: []string{"lsp", "status", "--json", "--root", "/workspace"}},
		{name: "confirmed semantic index", path: "/v1/search?q=meaning&semantic=true&index=true", want: []string{"search", "--json", "--root", "/workspace", "--semantic", "--index", "meaning"}},
		{name: "lsp status protocol probe", path: "/v1/lsp/status?probe=true", want: []string{"lsp", "status", "--json", "--root", "/workspace", "--probe"}},
		{name: "lsp diagnostics", path: "/v1/lsp/diagnostics?path=main.go", want: []string{"lsp", "diagnostics", "--json", "--root", "/workspace", "main.go"}},
		{name: "lsp format", path: "/v1/lsp/format?path=main.go", want: []string{"lsp", "format", "--json", "--root", "/workspace", "main.go"}},
		{name: "lsp symbols", path: "/v1/lsp/symbols?path=main.go", want: []string{"lsp", "symbols", "--json", "--root", "/workspace", "main.go"}},
		{name: "lsp hover", path: "/v1/lsp/hover?path=main.go&line=2&column=4", want: []string{"lsp", "hover", "--json", "--root", "/workspace", "--line", "2", "--column", "4", "main.go"}},
		{name: "lsp definition", path: "/v1/lsp/definition?path=main.go&line=2&column=4", want: []string{"lsp", "definition", "--json", "--root", "/workspace", "--line", "2", "--column", "4", "main.go"}},
		{name: "lsp references", path: "/v1/lsp/references?path=main.go&line=2&column=4", want: []string{"lsp", "references", "--json", "--root", "/workspace", "--line", "2", "--column", "4", "main.go"}},
		{name: "codemap symbols", path: "/v1/codemap/symbols?path=main.go", want: []string{"codemap", "symbols", "--json", "--root", "/workspace", "main.go"}},
		{name: "codemap symbol at", path: "/v1/codemap/symbol-at?path=main.go&line=3", want: []string{"codemap", "symbol-at", "--json", "--root", "/workspace", "--line", "3", "main.go"}},
		{name: "dap probe", path: "/v1/dap/probe?adapter=dlv", want: []string{"dap", "probe", "--json", "--root", "/workspace", "--adapter", "dlv"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			got, err := server.routeArgs(req, req.URL.Path, server.root)
			if err != nil {
				t.Fatalf("routeArgs() error = %v", err)
			}
			if strings.Join(got, "\x00") != strings.Join(tt.want, "\x00") {
				t.Fatalf("routeArgs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestHeadlessRESTSemanticIndexRequiresConfirmation(t *testing.T) {
	root := t.TempDir()
	handler := newHeadlessRESTHandler(root, "test-token")
	request := httptest.NewRequest(http.MethodGet, "/v1/search?q=meaning&semantic=true&index=true", nil)
	request.Header.Set("X-Teak-Token", "test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusPreconditionRequired {
		t.Fatalf("semantic index without confirmation status = %d, body = %s; want %d", response.Code, response.Body.String(), http.StatusPreconditionRequired)
	}
	var payload headlessRESTErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode semantic index confirmation error: %v; body = %s", err, response.Body.String())
	}
	if payload.Code != "confirmation_required" {
		t.Fatalf("semantic index confirmation code = %q, want confirmation_required", payload.Code)
	}
}

func TestHeadlessRESTCancelledHealthStopsCollectors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses POSIX process-group behavior")
	}
	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "codemap")
	fixtureSource := "#!/bin/sh\ncase \"$1\" in\n--version) printf '%s\\n' 'codemap fixture' ;;\nstructural-manifest|context) sleep 5 ;;\n*) exit 0 ;;\nesac\n"
	if err := os.WriteFile(fixture, []byte(fixtureSource), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	previousTimeout := headlessHealthTimeout
	headlessHealthTimeout = 5 * time.Second
	t.Cleanup(func() { headlessHealthTimeout = previousTimeout })

	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/v1/health", nil).WithContext(requestContext)
	request.Header.Set("X-Teak-Token", "test-token")
	handler := newHeadlessRESTHandler(root, "test-token")
	response := httptest.NewRecorder()
	started := time.Now()
	handler.ServeHTTP(response, request)

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancelled health request took %s; collector ignored request cancellation", elapsed)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("cancelled health status = %d, body = %s", response.Code, response.Body.String())
	}
	var health headlessHealthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode cancelled health response: %v; body = %s", err, response.Body.String())
	}
	if health.State != "cancelled" {
		t.Fatalf("cancelled health state = %q, want cancelled; body = %s", health.State, response.Body.String())
	}

	codemapContext, codemapCancel := context.WithCancel(context.Background())
	codemapCancel()
	codemapRequest := httptest.NewRequest(http.MethodGet, "/v1/codemap/context?symbol=Greeter", nil).WithContext(codemapContext)
	codemapRequest.Header.Set("X-Teak-Token", "test-token")
	codemapResponse := httptest.NewRecorder()
	codemapStarted := time.Now()
	handler.ServeHTTP(codemapResponse, codemapRequest)
	if elapsed := time.Since(codemapStarted); elapsed > time.Second {
		t.Fatalf("cancelled codemap request took %s; collector ignored request cancellation", elapsed)
	}
	if codemapResponse.Code != http.StatusBadRequest {
		t.Fatalf("cancelled codemap status = %d, body = %s", codemapResponse.Code, codemapResponse.Body.String())
	}
	var codemapError headlessRESTErrorResponse
	if err := json.Unmarshal(codemapResponse.Body.Bytes(), &codemapError); err != nil {
		t.Fatalf("decode cancelled codemap response: %v; body = %s", err, codemapResponse.Body.String())
	}
	if codemapError.Code != "request_cancelled" {
		t.Fatalf("cancelled codemap code = %q, want request_cancelled", codemapError.Code)
	}

	toolsContext, toolsCancel := context.WithCancel(context.Background())
	toolsCancel()
	toolsRequest := httptest.NewRequest(http.MethodGet, "/v1/tools/status", nil).WithContext(toolsContext)
	toolsRequest.Header.Set("X-Teak-Token", "test-token")
	toolsResponse := httptest.NewRecorder()
	toolsStarted := time.Now()
	handler.ServeHTTP(toolsResponse, toolsRequest)
	if elapsed := time.Since(toolsStarted); elapsed > time.Second {
		t.Fatalf("cancelled tools request took %s; collector ignored request cancellation", elapsed)
	}
	if toolsResponse.Code != http.StatusOK || !strings.Contains(toolsResponse.Body.String(), `"tools"`) {
		t.Fatalf("cancelled tools response = %d %s", toolsResponse.Code, toolsResponse.Body.String())
	}

	searchContext, searchCancel := context.WithCancel(context.Background())
	searchCancel()
	searchRequest := httptest.NewRequest(http.MethodGet, "/v1/search?q=needle", nil).WithContext(searchContext)
	searchRequest.Header.Set("X-Teak-Token", "test-token")
	searchResponse := httptest.NewRecorder()
	handler.ServeHTTP(searchResponse, searchRequest)
	if searchResponse.Code != http.StatusBadRequest {
		t.Fatalf("cancelled search status = %d, body = %s", searchResponse.Code, searchResponse.Body.String())
	}
	var searchError headlessRESTErrorResponse
	if err := json.Unmarshal(searchResponse.Body.Bytes(), &searchError); err != nil {
		t.Fatalf("decode cancelled search response: %v; body = %s", err, searchResponse.Body.String())
	}
	if searchError.Code != "request_cancelled" {
		t.Fatalf("cancelled search code = %q, want request_cancelled", searchError.Code)
	}
}

func TestHeadlessRESTCancelledReadRoutesReturnTypedErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newHeadlessRESTHandler(root, "test-token")
	routes := []string{
		"/v1/context?depth=2",
		"/v1/project/list?depth=2",
		"/v1/buffer/read?path=notes.txt",
		"/v1/health/history",
		"/v1/hitspec/validate?path=notes.txt",
		"/v1/git/status",
		"/v1/session/show",
		"/v1/session/list",
		"/v1/session/health",
		"/v1/lsp/diagnostics?path=notes.txt",
		"/v1/lsp/format?path=notes.txt",
		"/v1/dap/probe?adapter=dlv",
		"/v1/agent/list",
		"/v1/agent/show?run_id=missing",
	}
	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			requestContext, cancel := context.WithCancel(context.Background())
			cancel()
			request := httptest.NewRequest(http.MethodGet, route, nil).WithContext(requestContext)
			request.Header.Set("X-Teak-Token", "test-token")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if strings.HasPrefix(route, "/v1/dap/probe") {
				if response.Code != http.StatusUnprocessableEntity {
					t.Fatalf("cancelled DAP probe status = %d, body = %s", response.Code, response.Body.String())
				}
				var probe headlessDAPProbeResponse
				if err := json.Unmarshal(response.Body.Bytes(), &probe); err != nil {
					t.Fatalf("decode cancelled DAP probe: %v; body = %s", err, response.Body.String())
				}
				if probe.State != "cancelled" || probe.Ready {
					t.Fatalf("cancelled DAP probe = %#v, want cancelled and not ready", probe)
				}
				return
			}
			if response.Code != http.StatusBadRequest {
				t.Fatalf("cancelled route status = %d, body = %s", response.Code, response.Body.String())
			}
			var errorResponse headlessRESTErrorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &errorResponse); err != nil {
				t.Fatalf("decode cancelled route response: %v; body = %s", err, response.Body.String())
			}
			if errorResponse.Code != "request_cancelled" {
				t.Fatalf("cancelled route code = %q, want request_cancelled", errorResponse.Code)
			}
		})
	}
}

func TestHeadlessRESTRejectsMutationsAndUnknownRoutes(t *testing.T) {
	root := t.TempDir()
	handler := newHeadlessRESTHandler(root, "test-token")

	mutation := httptest.NewRequest(http.MethodPost, "/v1/buffer/write", strings.NewReader("changed"))
	mutation.Header.Set("Authorization", "Bearer test-token")
	mutationResponse := httptest.NewRecorder()
	handler.ServeHTTP(mutationResponse, mutation)
	if mutationResponse.Code != http.StatusPreconditionRequired {
		t.Fatalf("mutation status = %d, want %d", mutationResponse.Code, http.StatusPreconditionRequired)
	}

	unknown := httptest.NewRequest(http.MethodGet, "/v1/unknown", nil)
	unknown.Header.Set("Authorization", "Bearer test-token")
	unknownResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownResponse, unknown)
	if unknownResponse.Code != http.StatusNotFound {
		t.Fatalf("unknown status = %d, want %d", unknownResponse.Code, http.StatusNotFound)
	}

	invalidQuery := httptest.NewRequest(http.MethodGet, "/v1/search?q=needle&regex=maybe", nil)
	invalidQuery.Header.Set("Authorization", "Bearer test-token")
	invalidQueryResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidQueryResponse, invalidQuery)
	if invalidQueryResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid query status = %d, want %d", invalidQueryResponse.Code, http.StatusBadRequest)
	}
}

func TestHeadlessRESTAgentCancelRequiresConfirmationAndPersists(t *testing.T) {
	root := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	store := agentruntime.FileStore{Path: headlessAgentStorePath(root)}
	if err := store.Save([]agentruntime.RunRecord{{
		ID:     "remote-run",
		Status: agentruntime.RunRunning,
		Spec: agentruntime.RunSpec{
			Objective: "remote cancellation",
			Workspace: root,
		},
		StartedAt:       time.Now().Add(-time.Minute),
		LastHeartbeatAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	handler := newHeadlessRESTHandler(root, "test-token")

	withoutConfirmation := httptest.NewRequest(http.MethodPost, "/v1/agent/cancel?run_id=remote-run", nil)
	withoutConfirmation.Header.Set("Authorization", "Bearer test-token")
	withoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(withoutResponse, withoutConfirmation)
	if withoutResponse.Code != http.StatusPreconditionRequired {
		t.Fatalf("unconfirmed cancel status = %d, want %d", withoutResponse.Code, http.StatusPreconditionRequired)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/agent/cancel?run_id=remote-run", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("X-Teak-Confirm", "true")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("confirmed cancel status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload headlessAgentCancelResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode cancel response: %v; body = %s", err, response.Body.String())
	}
	if payload.State != "cancelled" || payload.RunID != "remote-run" {
		t.Fatalf("cancel response = %#v, want cancelled remote-run", payload)
	}
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != agentruntime.RunCancelled {
		t.Fatalf("stored records = %#v, want cancelled run", records)
	}
}

func TestHeadlessRESTAgentReapStaleSupportsNamedWorkspace(t *testing.T) {
	root := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	store := agentruntime.FileStore{Path: headlessAgentStorePath(root)}
	old := time.Now().Add(-time.Hour)
	if err := store.Save([]agentruntime.RunRecord{{
		ID:              "stale-remote-run",
		Status:          agentruntime.RunRunning,
		Spec:            agentruntime.RunSpec{Objective: "remote reap", Workspace: root},
		StartedAt:       old,
		LastHeartbeatAt: old,
	}}); err != nil {
		t.Fatal(err)
	}
	handler := newHeadlessRESTHandlerForWorkspaces(map[string]string{"review": root}, "review", "test-token")
	request := httptest.NewRequest(http.MethodPost, "/v1/workspaces/review/agent/reap-stale?max_silence=1m", nil)
	request.Header.Set("X-Teak-Token", "test-token")
	request.Header.Set("X-Teak-Confirm", "true")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("reap status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload headlessAgentReapResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode reap response: %v; body = %s", err, response.Body.String())
	}
	if payload.State != "reaped" || len(payload.Reaped) != 1 || payload.Reaped[0] != "stale-remote-run" {
		t.Fatalf("reap response = %#v, want stale run reaped", payload)
	}
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != agentruntime.RunInterrupted {
		t.Fatalf("stored records = %#v, want interrupted run", records)
	}
}

func TestHeadlessRESTRejectsOversizedQueryArguments(t *testing.T) {
	root := t.TempDir()
	handler := newHeadlessRESTHandler(root, "test-token")
	request := httptest.NewRequest(http.MethodGet, "/v1/search?q="+strings.Repeat("x", headlessRESTMaxQueryValueBytes+1), nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestURITooLong {
		t.Fatalf("oversized query status = %d, body = %s; want %d", response.Code, response.Body.String(), http.StatusRequestURITooLong)
	}
	var errorResponse headlessRESTErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &errorResponse); err != nil {
		t.Fatalf("decode oversized query response: %v; body = %s", err, response.Body.String())
	}
	if errorResponse.Code != "request_too_long" {
		t.Fatalf("oversized query code = %q, want request_too_long", errorResponse.Code)
	}
}

func TestHeadlessRESTBufferWriteRequiresConfirmationAndSHA(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newHeadlessRESTHandler(root, "test-token")

	readRequest := httptest.NewRequest(http.MethodGet, "/v1/buffer/read?path=notes.txt", nil)
	readRequest.Header.Set("Authorization", "Bearer test-token")
	readResponse := httptest.NewRecorder()
	handler.ServeHTTP(readResponse, readRequest)
	if readResponse.Code != http.StatusOK {
		t.Fatalf("read status = %d, body = %s", readResponse.Code, readResponse.Body.String())
	}
	var before headlessBufferResponse
	if err := json.Unmarshal(readResponse.Body.Bytes(), &before); err != nil {
		t.Fatalf("decode initial buffer: %v", err)
	}

	writeBody := `{"path":"notes.txt","expected_sha256":"` + before.SHA256 + `","content":"after\n"}`
	withoutConfirmation := httptest.NewRequest(http.MethodPost, "/v1/buffer/write", strings.NewReader(writeBody))
	withoutConfirmation.Header.Set("Authorization", "Bearer test-token")
	withoutConfirmation.Header.Set("Content-Type", "application/json")
	withoutConfirmationResponse := httptest.NewRecorder()
	handler.ServeHTTP(withoutConfirmationResponse, withoutConfirmation)
	if withoutConfirmationResponse.Code != http.StatusPreconditionRequired {
		t.Fatalf("without confirmation status = %d, body = %s", withoutConfirmationResponse.Code, withoutConfirmationResponse.Body.String())
	}

	stale := httptest.NewRequest(http.MethodPost, "/v1/buffer/write", strings.NewReader(`{"path":"notes.txt","expected_sha256":"`+strings.Repeat("0", 64)+`","content":"after\n"}`))
	stale.Header.Set("Authorization", "Bearer test-token")
	stale.Header.Set("X-Teak-Confirm", "true")
	stale.Header.Set("Content-Type", "application/json")
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, stale)
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("stale status = %d, body = %s", staleResponse.Code, staleResponse.Body.String())
	}
	if data, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	} else if string(data) != "before\n" {
		t.Fatalf("stale write changed file to %q", data)
	}

	confirmed := httptest.NewRequest(http.MethodPost, "/v1/buffer/write", strings.NewReader(writeBody))
	confirmed.Header.Set("Authorization", "Bearer test-token")
	confirmed.Header.Set("X-Teak-Confirm", "true")
	confirmed.Header.Set("Content-Type", "application/json")
	confirmedResponse := httptest.NewRecorder()
	handler.ServeHTTP(confirmedResponse, confirmed)
	if confirmedResponse.Code != http.StatusOK {
		t.Fatalf("confirmed status = %d, body = %s", confirmedResponse.Code, confirmedResponse.Body.String())
	}
	var after headlessBufferResponse
	if err := json.Unmarshal(confirmedResponse.Body.Bytes(), &after); err != nil {
		t.Fatalf("decode confirmed buffer: %v", err)
	}
	if after.Content != "after\n" || after.SHA256 == before.SHA256 {
		t.Fatalf("confirmed response = %#v, want updated content and hash", after)
	}
	if data, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	} else if string(data) != "after\n" {
		t.Fatalf("confirmed write persisted %q", data)
	}
}

func TestHeadlessRESTCancelledBufferWriteDoesNotCommit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	before := []byte("before\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(before)
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/v1/buffer/write", strings.NewReader(`{"path":"notes.txt","expected_sha256":"`+hex.EncodeToString(digest[:])+`","content":"after\n"}`)).WithContext(requestContext)
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("X-Teak-Confirm", "true")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newHeadlessRESTHandler(root, "test-token").ServeHTTP(response, request)
	if response.Code == http.StatusOK {
		t.Fatalf("cancelled buffer write returned success: %s", response.Body.String())
	}
	var errorResponse headlessRESTErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &errorResponse); err != nil {
		t.Fatalf("decode cancelled buffer response: %v; body = %s", err, response.Body.String())
	}
	if errorResponse.Code != "request_cancelled" {
		t.Fatalf("cancelled buffer code = %q, want request_cancelled", errorResponse.Code)
	}
	if data, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	} else if string(data) != string(before) {
		t.Fatalf("cancelled buffer write changed file to %q", data)
	}
}

func TestHeadlessRESTProjectMutationsAreConfirmedRootConfinedAndObservable(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "source", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source", "nested", "file.txt"), []byte("payload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newHeadlessRESTHandler(root, "test-token")
	request := func(route, body string, confirmed bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
		if confirmed {
			req.Header.Set("X-Teak-Confirm", "true")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}

	withoutConfirmation := request("/v1/project/mkdir", `{"source":"created"}`, false)
	if withoutConfirmation.Code != http.StatusPreconditionRequired {
		t.Fatalf("mkdir without confirmation status = %d, body = %s", withoutConfirmation.Code, withoutConfirmation.Body.String())
	}

	created := request("/v1/project/mkdir", `{"source":"created"}`, true)
	if created.Code != http.StatusOK {
		t.Fatalf("mkdir status = %d, body = %s", created.Code, created.Body.String())
	}
	var mkdirResponse headlessProjectResponse
	if err := json.Unmarshal(created.Body.Bytes(), &mkdirResponse); err != nil {
		t.Fatalf("decode mkdir response: %v", err)
	}
	if !mkdirResponse.Committed || mkdirResponse.Operation != "mkdir" || mkdirResponse.DurationMS < 0 {
		t.Fatalf("mkdir response = %#v, want committed typed result", mkdirResponse)
	}

	rename := request("/v1/project/rename", `{"source":"source","destination":"renamed"}`, true)
	if rename.Code != http.StatusOK {
		t.Fatalf("rename status = %d, body = %s", rename.Code, rename.Body.String())
	}
	var renameResponse headlessProjectResponse
	if err := json.Unmarshal(rename.Body.Bytes(), &renameResponse); err != nil {
		t.Fatalf("decode rename response: %v", err)
	}
	if renameResponse.Operation != "rename" || renameResponse.Source != "source" || renameResponse.Destination != "renamed" {
		t.Fatalf("rename response = %#v, want source/destination metadata", renameResponse)
	}

	copyResponse := request("/v1/project/copy", `{"source":"renamed","destination":"copy"}`, true)
	if copyResponse.Code != http.StatusOK {
		t.Fatalf("copy status = %d, body = %s", copyResponse.Code, copyResponse.Body.String())
	}
	if data, err := os.ReadFile(filepath.Join(root, "copy", "nested", "file.txt")); err != nil {
		t.Fatalf("read copied file: %v", err)
	} else if string(data) != "payload\n" {
		t.Fatalf("copied file = %q, want payload", data)
	}

	removeResponse := request("/v1/project/remove", `{"source":"copy"}`, true)
	if removeResponse.Code != http.StatusOK {
		t.Fatalf("remove status = %d, body = %s", removeResponse.Code, removeResponse.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "copy")); !os.IsNotExist(err) {
		t.Fatalf("copy still exists after remove, stat error = %v", err)
	}

	traversal := request("/v1/project/mkdir", `{"source":"../outside"}`, true)
	if traversal.Code != http.StatusBadRequest {
		t.Fatalf("traversal status = %d, body = %s", traversal.Code, traversal.Body.String())
	}
}

func TestHeadlessRESTCancelledProjectMutationDoesNotCommit(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "payload.txt"), []byte("payload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newHeadlessRESTHandler(root, "test-token")
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/v1/project/copy", strings.NewReader(`{"source":"source","destination":"copy"}`)).WithContext(requestContext)
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("X-Teak-Confirm", "true")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code == http.StatusOK {
		t.Fatalf("cancelled project mutation returned success: %s", response.Body.String())
	}
	var errorResponse headlessRESTErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &errorResponse); err != nil {
		t.Fatalf("decode cancelled mutation response: %v; body = %s", err, response.Body.String())
	}
	if errorResponse.Code != "request_cancelled" {
		t.Fatalf("cancelled mutation code = %q, want request_cancelled", errorResponse.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "copy")); !os.IsNotExist(err) {
		t.Fatalf("cancelled project mutation committed destination, stat error = %v", err)
	}
}

func TestHeadlessRESTCancelledProjectMutationStopsBeforeBodyDecode(t *testing.T) {
	root := t.TempDir()
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/v1/project/copy", strings.NewReader("not-json")).WithContext(requestContext)
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("X-Teak-Confirm", "true")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newHeadlessRESTHandler(root, "test-token").ServeHTTP(response, request)

	if response.Code != http.StatusRequestTimeout {
		t.Fatalf("cancelled project status = %d, body = %s; want %d", response.Code, response.Body.String(), http.StatusRequestTimeout)
	}
	var errorResponse headlessRESTErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &errorResponse); err != nil {
		t.Fatalf("decode cancelled project response: %v; body = %s", err, response.Body.String())
	}
	if errorResponse.Code != "request_cancelled" {
		t.Fatalf("cancelled project code = %q, want request_cancelled", errorResponse.Code)
	}
}

func TestHeadlessRESTMultiWorkspaceRoutesStayFixedAndNamespaced(t *testing.T) {
	alpha := t.TempDir()
	beta := t.TempDir()
	if err := os.WriteFile(filepath.Join(alpha, "alpha.txt"), []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beta, "beta.txt"), []byte("beta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newHeadlessRESTHandlerForWorkspaces(map[string]string{
		"alpha": alpha,
		"beta":  beta,
	}, "alpha", "test-token")

	request := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer test-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	requestBody := func(method, path, body string, confirm bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
		if confirm {
			req.Header.Set("X-Teak-Confirm", "true")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}

	workspaces := request(http.MethodGet, "/v1/workspaces")
	if workspaces.Code != http.StatusOK || !strings.Contains(workspaces.Body.String(), `"name":"alpha"`) || !strings.Contains(workspaces.Body.String(), `"name":"beta"`) {
		t.Fatalf("workspace listing status/body = %d/%s", workspaces.Code, workspaces.Body.String())
	}

	defaultContext := request(http.MethodGet, "/v1/context?root=/")
	if defaultContext.Code != http.StatusOK {
		t.Fatalf("default context status = %d, body = %s", defaultContext.Code, defaultContext.Body.String())
	}
	var defaultReport headlessContextResponse
	if err := json.Unmarshal(defaultContext.Body.Bytes(), &defaultReport); err != nil {
		t.Fatal(err)
	}
	if defaultReport.Workspace != alpha || len(defaultReport.Entries) != 1 || defaultReport.Entries[0].Path != "alpha.txt" {
		t.Fatalf("default context = %#v, want alpha workspace", defaultReport)
	}

	betaContext := request(http.MethodGet, "/v1/workspaces/beta/context?root=/")
	if betaContext.Code != http.StatusOK {
		t.Fatalf("beta context status = %d, body = %s", betaContext.Code, betaContext.Body.String())
	}
	var betaReport headlessContextResponse
	if err := json.Unmarshal(betaContext.Body.Bytes(), &betaReport); err != nil {
		t.Fatal(err)
	}
	if betaReport.Workspace != beta || len(betaReport.Entries) != 1 || betaReport.Entries[0].Path != "beta.txt" {
		t.Fatalf("beta context = %#v, want beta workspace", betaReport)
	}

	betaRead := request(http.MethodGet, "/v1/workspaces/beta/buffer/read?path=beta.txt")
	if betaRead.Code != http.StatusOK {
		t.Fatalf("beta read status = %d, body = %s", betaRead.Code, betaRead.Body.String())
	}
	var betaBefore headlessBufferResponse
	if err := json.Unmarshal(betaRead.Body.Bytes(), &betaBefore); err != nil {
		t.Fatalf("decode beta buffer: %v", err)
	}
	betaWrite := requestBody(http.MethodPost, "/v1/workspaces/beta/buffer/write", `{"path":"beta.txt","expected_sha256":"`+betaBefore.SHA256+`","content":"beta-updated\n"}`, true)
	if betaWrite.Code != http.StatusOK {
		t.Fatalf("beta write status = %d, body = %s", betaWrite.Code, betaWrite.Body.String())
	}
	if data, err := os.ReadFile(filepath.Join(beta, "beta.txt")); err != nil {
		t.Fatal(err)
	} else if string(data) != "beta-updated\n" {
		t.Fatalf("namespaced write changed beta to %q", data)
	}
	if data, err := os.ReadFile(filepath.Join(alpha, "alpha.txt")); err != nil {
		t.Fatal(err)
	} else if string(data) != "alpha\n" {
		t.Fatalf("namespaced write changed default workspace to %q", data)
	}

	unknown := request(http.MethodGet, "/v1/workspaces/gamma/context")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown workspace status = %d, body = %s", unknown.Code, unknown.Body.String())
	}
}

func TestResolveHeadlessServeWorkspacesValidatesExplicitRoots(t *testing.T) {
	alpha := t.TempDir()
	beta := t.TempDir()

	workspaces, defaultName, err := resolveHeadlessServeWorkspaces(alpha, []string{"beta=" + beta})
	if err != nil {
		t.Fatalf("resolve workspaces with default root: %v", err)
	}
	if defaultName != "default" || workspaces["default"] != alpha || workspaces["beta"] != beta {
		t.Fatalf("workspaces = %#v default=%q, want explicit roots", workspaces, defaultName)
	}

	workspaces, defaultName, err = resolveHeadlessServeWorkspaces("", []string{"alpha=" + alpha, "beta=" + beta})
	if err != nil {
		t.Fatalf("resolve named workspaces: %v", err)
	}
	if defaultName != "alpha" || len(workspaces) != 2 {
		t.Fatalf("named workspaces = %#v default=%q, want first named workspace as default", workspaces, defaultName)
	}

	for _, specs := range [][]string{
		{"=missing"},
		{"bad.name=" + alpha},
		{"alpha=" + alpha, "alpha=" + beta},
		{"alpha=" + alpha, "beta=" + alpha},
	} {
		if _, _, err := resolveHeadlessServeWorkspaces("", specs); err == nil {
			t.Fatalf("resolve workspaces(%v) succeeded, want validation error", specs)
		}
	}
}

func TestValidateHeadlessListenAddressOnlyAllowsLoopback(t *testing.T) {
	tests := []struct {
		address string
		valid   bool
	}{
		{address: "127.0.0.1:0", valid: true},
		{address: "[::1]:0", valid: true},
		{address: "localhost:0", valid: true},
		{address: ":0", valid: false},
		{address: "0.0.0.0:8080", valid: false},
		{address: "192.0.2.1:8080", valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			err := validateHeadlessListenAddress(tt.address)
			if (err == nil) != tt.valid {
				t.Fatalf("validateHeadlessListenAddress(%q) error = %v, valid = %t", tt.address, err, tt.valid)
			}
		})
	}
}

func TestRunHeadlessServeRequiresExplicitLoopbackAndToken(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := runHeadlessServe([]string{"--listen", "127.0.0.1:0", "--root", root}, &stdout, &stderr); code == 0 {
		t.Fatal("serve without token succeeded")
	}
	if !strings.Contains(stderr.String(), "requires --token") {
		t.Fatalf("serve error = %q, want token requirement", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runHeadlessServe([]string{"--listen", "0.0.0.0:0", "--token", "secret", "--root", root}, &stdout, &stderr); code == 0 {
		t.Fatal("serve on wildcard address succeeded")
	}
	if !strings.Contains(stderr.String(), "not loopback") {
		t.Fatalf("serve address error = %q, want loopback rejection", stderr.String())
	}
}
