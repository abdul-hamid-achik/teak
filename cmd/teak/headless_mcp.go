package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"teak/internal/toolpath"
)

const (
	headlessMCPProtocolVersion      = "2024-11-05"
	headlessMCPMaxMessageBytes      = 256 << 10
	headlessMCPMaxOutputBytes       = 512 << 10
	headlessMCPMaxErrorBytes        = 64 << 10
	headlessMCPMaxArgumentBytes     = 16 << 10
	headlessMCPDisconnectGrace      = 2 * time.Second
	headlessMCPRequestCancelledCode = -32800
	headlessMCPQuotaExceededCode    = -32029
	headlessMCPParseErrorCode       = -32700
	headlessMCPInvalidRequestCode   = -32600
	headlessMCPMethodNotFoundCode   = -32601
	headlessMCPInvalidParamsCode    = -32602
	headlessMCPInternalErrorCode    = -32603
)

type headlessMCPCommandRunner func(context.Context, []string) ([]byte, []byte, int, error)
type headlessMCPInputRunner func(context.Context, []string, []byte) ([]byte, []byte, int, error)

type headlessMCPServer struct {
	root     string
	run      headlessMCPCommandRunner
	runInput headlessMCPInputRunner
	write    sync.Mutex
	activeMu sync.Mutex
	active   map[string]context.CancelFunc
	quota    *headlessQuota
	wg       sync.WaitGroup
}

type headlessMCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type headlessMCPResponse struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id"`
	Result  any               `json:"result,omitempty"`
	Error   *headlessMCPError `json:"error,omitempty"`
}

type headlessMCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type headlessMCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type headlessMCPCallResult struct {
	Content []headlessMCPContent `json:"content"`
	IsError bool                 `json:"isError,omitempty"`
}

type headlessMCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func newHeadlessMCPServer(root string, runner headlessMCPCommandRunner) *headlessMCPServer {
	var inputRunner headlessMCPInputRunner
	if runner == nil {
		runner = runHeadlessMCPSubprocess(root)
		inputRunner = runHeadlessMCPSubprocessWithInput(root)
	} else {
		// Unit tests and embedders can continue to provide the original runner;
		// confirmed writes still use it, with input intentionally unavailable to
		// that compatibility callback.
		inputRunner = func(ctx context.Context, args []string, _ []byte) ([]byte, []byte, int, error) {
			return runner(ctx, args)
		}
	}
	return &headlessMCPServer{
		root:     root,
		run:      runner,
		runInput: inputRunner,
		active:   make(map[string]context.CancelFunc),
		quota:    newHeadlessQuota(headlessMaxConcurrentOperations, (headlessMCPMaxOutputBytes+headlessMCPMaxErrorBytes)*headlessMaxConcurrentOperations),
	}
}

func (s *headlessMCPServer) serve(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	stopInputWatch := watchHeadlessMCPInput(ctx, stdin)
	defer stopInputWatch()
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 4096), headlessMCPMaxMessageBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var request headlessMCPRequest
		if err := json.Unmarshal(line, &request); err != nil {
			if writeErr := s.writeResponse(stdout, headlessMCPResponse{
				JSONRPC: "2.0",
				ID:      json.RawMessage("null"),
				Error:   &headlessMCPError{Code: headlessMCPParseErrorCode, Message: err.Error()},
			}); writeErr != nil {
				return writeErr
			}
			continue
		}
		if mcpRequestIDPresent(request.ID) && !hasMCPRequestID(request.ID) {
			if err := s.writeError(stdout, nil, headlessMCPInvalidRequestCode, "request id must be a string or number"); err != nil {
				return err
			}
			continue
		}
		if request.JSONRPC != "2.0" || strings.TrimSpace(request.Method) == "" {
			if hasMCPRequestID(request.ID) {
				if err := s.writeError(stdout, request.ID, headlessMCPInvalidRequestCode, "invalid JSON-RPC request"); err != nil {
					return err
				}
			}
			continue
		}

		switch request.Method {
		case "notifications/initialized":
			continue
		case "notifications/cancelled":
			s.cancelRequest(request.Params)
			continue
		}
		if !hasMCPRequestID(request.ID) {
			continue
		}

		requestContext, cancel := context.WithCancel(ctx)
		requestKey := string(request.ID)
		s.activeMu.Lock()
		_, duplicate := s.active[requestKey]
		if !duplicate {
			s.active[requestKey] = cancel
		}
		s.activeMu.Unlock()
		if duplicate {
			cancel()
			if err := s.writeError(stdout, request.ID, headlessMCPInvalidRequestCode, "request id is already active"); err != nil {
				return err
			}
			continue
		}
		quota := s.quota
		if quota == nil {
			quota = newHeadlessQuota(headlessMaxConcurrentOperations, (headlessMCPMaxOutputBytes+headlessMCPMaxErrorBytes)*headlessMaxConcurrentOperations)
			s.quota = quota
		}
		release, quotaErr := quota.acquire(requestContext, headlessMCPMaxOutputBytes+headlessMCPMaxErrorBytes)
		if quotaErr != nil {
			parentCancelled := ctx.Err() != nil
			cancel()
			s.activeMu.Lock()
			delete(s.active, requestKey)
			s.activeMu.Unlock()
			code := headlessMCPQuotaExceededCode
			if parentCancelled {
				code = headlessMCPRequestCancelledCode
			}
			if err := s.writeError(stdout, request.ID, code, quotaErr.Error()); err != nil {
				return err
			}
			continue
		}
		s.wg.Add(1)
		go func(req headlessMCPRequest, key string, requestContext context.Context, cancel context.CancelFunc, release func()) {
			defer s.wg.Done()
			defer cancel()
			defer release()
			defer s.removeRequest(key)
			response := s.handle(requestContext, req)
			if err := s.writeResponse(stdout, response); err != nil {
				_, _ = fmt.Fprintf(stderr, "mcp: write response: %v\n", err)
			}
		}(request, requestKey, requestContext, cancel, release)
	}
	if scanErr := scanner.Err(); scanErr != nil {
		s.cancelAll()
		s.wg.Wait()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("read MCP message: %w", scanErr)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		s.cancelAll()
		s.wg.Wait()
		return ctxErr
	}
	// EOF means the client disconnected. Allow a short bounded grace period so
	// one-shot stdio clients can receive a quick read-only result, then cancel
	// anything that is still running instead of leaking a child command.
	s.waitForActive(headlessMCPDisconnectGrace)
	s.cancelAll()
	s.wg.Wait()
	return nil
}

// watchHeadlessMCPInput closes a closable stdio reader when its session context
// is cancelled. Scanner.Scan cannot observe context cancellation by itself, so
// without this bridge a live client that never sends another line could keep
// the MCP server blocked forever. Non-closable readers retain their existing
// EOF/error behavior because there is no safe way to interrupt their Read.
func watchHeadlessMCPInput(ctx context.Context, input io.Reader) func() {
	closer, ok := input.(io.Closer)
	if !ok {
		return func() {}
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = closer.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

func (s *headlessMCPServer) handle(ctx context.Context, request headlessMCPRequest) headlessMCPResponse {
	response := headlessMCPResponse{JSONRPC: "2.0", ID: request.ID}
	switch request.Method {
	case "initialize":
		response.Result = map[string]any{
			"protocolVersion": headlessMCPProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    "teak",
				"version": version,
			},
		}
	case "tools/list":
		response.Result = map[string]any{"tools": headlessMCPTools()}
	case "tools/call":
		result, mcpErr := s.callTool(ctx, request.Params)
		if mcpErr != nil {
			response.Error = mcpErr
		} else {
			response.Result = result
		}
	default:
		response.Error = &headlessMCPError{Code: headlessMCPMethodNotFoundCode, Message: "method not found: " + request.Method}
	}
	return response
}

func (s *headlessMCPServer) callTool(ctx context.Context, rawParams json.RawMessage) (headlessMCPCallResult, *headlessMCPError) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}
	if len(rawParams) == 0 || string(rawParams) == "null" || json.Unmarshal(rawParams, &params) != nil || strings.TrimSpace(params.Name) == "" {
		return headlessMCPCallResult{}, &headlessMCPError{Code: headlessMCPInvalidParamsCode, Message: "tools/call requires a non-empty name and object arguments"}
	}
	args, err := s.toolArgs(params.Name, params.Arguments)
	if err != nil {
		return headlessMCPCallResult{}, &headlessMCPError{Code: headlessMCPInvalidParamsCode, Message: err.Error()}
	}
	input, withInput, err := mcpToolInput(params.Name, params.Arguments)
	if err != nil {
		return headlessMCPCallResult{}, &headlessMCPError{Code: headlessMCPInvalidParamsCode, Message: err.Error()}
	}
	var stdout, stderr []byte
	var exitCode int
	var runErr error
	if withInput {
		stdout, stderr, exitCode, runErr = s.runInput(ctx, args, input)
	} else {
		stdout, stderr, exitCode, runErr = s.run(ctx, args)
	}
	if ctx.Err() != nil {
		return headlessMCPCallResult{}, &headlessMCPError{Code: headlessMCPRequestCancelledCode, Message: "request cancelled"}
	}
	if runErr != nil && exitCode == 0 {
		return headlessMCPCallResult{}, &headlessMCPError{Code: headlessMCPInternalErrorCode, Message: runErr.Error()}
	}
	text := strings.TrimSpace(string(stdout))
	if text == "" {
		text = strings.TrimSpace(string(stderr))
	}
	if text == "" && runErr != nil {
		text = runErr.Error()
	}
	if text == "" {
		text = "{}"
	}
	return headlessMCPCallResult{
		Content: []headlessMCPContent{{Type: "text", Text: text}},
		IsError: exitCode != 0 || runErr != nil,
	}, nil
}

func (s *headlessMCPServer) toolArgs(name string, raw json.RawMessage) ([]string, error) {
	arguments, err := decodeMCPArguments(raw)
	if err != nil {
		return nil, err
	}
	base := func(command string, operation ...string) []string {
		args := []string{command}
		args = append(args, operation...)
		return append(args, "--json", "--root", s.root)
	}
	depthArgs := func(args []string, arguments map[string]json.RawMessage) ([]string, error) {
		depth, present, err := mcpInt(arguments, "depth", 0, headlessMaxContextDepth)
		if err != nil {
			return nil, err
		}
		if present {
			args = append(args, "--depth", strconv.Itoa(depth))
		}
		return args, nil
	}
	stringArg := func(key string, required bool) (string, error) {
		return mcpString(arguments, key, required)
	}
	projectMutationArgs := func(operation string) ([]string, error) {
		allowed := []string{"confirm"}
		if operation == "rename" || operation == "copy" {
			allowed = append(allowed, "source", "destination")
		} else {
			allowed = append(allowed, "path")
		}
		if err := rejectMCPKeys(arguments, allowed...); err != nil {
			return nil, err
		}
		confirmed, present, err := mcpBool(arguments, "confirm")
		if err != nil {
			return nil, err
		}
		if !present || !confirmed {
			return nil, fmt.Errorf("teak_project_%s requires confirm: true", operation)
		}
		args := base("project", operation)
		args = append(args, "--confirm")
		if operation == "rename" || operation == "copy" {
			source, err := stringArg("source", true)
			if err != nil {
				return nil, err
			}
			destination, err := stringArg("destination", true)
			if err != nil {
				return nil, err
			}
			return append(args, source, destination), nil
		}
		path, err := stringArg("path", true)
		if err != nil {
			return nil, err
		}
		return append(args, path), nil
	}

	switch name {
	case "teak_context":
		if err := rejectMCPKeys(arguments, "depth"); err != nil {
			return nil, err
		}
		return depthArgs(base("context"), arguments)
	case "teak_project_list":
		if err := rejectMCPKeys(arguments, "depth"); err != nil {
			return nil, err
		}
		return depthArgs(base("project", "list"), arguments)
	case "teak_project_stat":
		if err := rejectMCPKeys(arguments, "path"); err != nil {
			return nil, err
		}
		path, err := stringArg("path", true)
		if err != nil {
			return nil, err
		}
		return append(base("project", "stat"), path), nil
	case "teak_project_mkdir":
		return projectMutationArgs("mkdir")
	case "teak_project_rename":
		return projectMutationArgs("rename")
	case "teak_project_copy":
		return projectMutationArgs("copy")
	case "teak_project_remove":
		return projectMutationArgs("remove")
	case "teak_buffer_read":
		if err := rejectMCPKeys(arguments, "path"); err != nil {
			return nil, err
		}
		path, err := stringArg("path", true)
		if err != nil {
			return nil, err
		}
		return append(base("buffer", "read"), path), nil
	case "teak_buffer_write":
		path, expectedSHA, _, err := mcpBufferWriteArguments(arguments)
		if err != nil {
			return nil, err
		}
		args := base("buffer", "write")
		args = append(args, "--expected-sha256", expectedSHA, path)
		return args, nil
	case "teak_search":
		if err := rejectMCPKeys(arguments, "query", "regex", "case_sensitive", "semantic", "index", "confirm"); err != nil {
			return nil, err
		}
		query, err := stringArg("query", true)
		if err != nil {
			return nil, err
		}
		args := base("search")
		for _, option := range []struct {
			key  string
			flag string
		}{
			{key: "regex", flag: "--regex"},
			{key: "case_sensitive", flag: "--case-sensitive"},
		} {
			value, present, err := mcpBool(arguments, option.key)
			if err != nil {
				return nil, err
			}
			if present && value {
				args = append(args, option.flag)
			}
		}
		semantic, semanticPresent, err := mcpBool(arguments, "semantic")
		if err != nil {
			return nil, err
		}
		if semanticPresent && semantic {
			args = append(args, "--semantic")
		}
		index, indexPresent, err := mcpBool(arguments, "index")
		if err != nil {
			return nil, err
		}
		if indexPresent && index {
			if !semantic {
				return nil, errors.New("teak_search index requires semantic: true")
			}
			confirmed, confirmPresent, err := mcpBool(arguments, "confirm")
			if err != nil {
				return nil, err
			}
			if !confirmPresent || !confirmed {
				return nil, errors.New("teak_search semantic indexing requires confirm: true")
			}
			args = append(args, "--index")
		}
		return append(args, query), nil
	case "teak_codemap":
		if err := rejectMCPKeys(arguments, "operation", "symbol", "depth"); err != nil {
			return nil, err
		}
		operation, err := stringArg("operation", true)
		if err != nil {
			return nil, err
		}
		validOperations := map[string]struct{}{"context": {}, "callers": {}, "callees": {}, "find": {}, "impact": {}}
		if _, ok := validOperations[operation]; !ok {
			return nil, fmt.Errorf("operation %q is not supported", operation)
		}
		symbol, err := stringArg("symbol", true)
		if err != nil {
			return nil, err
		}
		args, err := depthArgs(base("codemap", operation), arguments)
		if err != nil {
			return nil, err
		}
		return append(args, symbol), nil
	case "teak_codemap_symbols":
		if err := rejectMCPKeys(arguments, "path"); err != nil {
			return nil, err
		}
		path, err := stringArg("path", true)
		if err != nil {
			return nil, err
		}
		path, err = headlessCodemapFilePath(path)
		if err != nil {
			return nil, err
		}
		return append(base("codemap", "symbols"), path), nil
	case "teak_codemap_symbol_at":
		if err := rejectMCPKeys(arguments, "path", "line"); err != nil {
			return nil, err
		}
		path, err := stringArg("path", true)
		if err != nil {
			return nil, err
		}
		path, err = headlessCodemapFilePath(path)
		if err != nil {
			return nil, err
		}
		line, present, err := mcpInt(arguments, "line", 0, maxHeadlessLSPPosition)
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, errors.New("teak_codemap_symbol_at requires line")
		}
		args := base("codemap", "symbol-at")
		args = append(args, "--line", strconv.Itoa(line), path)
		return args, nil
	case "teak_tools_status":
		if err := rejectMCPKeys(arguments); err != nil {
			return nil, err
		}
		return base("tools", "status"), nil
	case "teak_health":
		if err := rejectMCPKeys(arguments); err != nil {
			return nil, err
		}
		return base("health"), nil
	case "teak_health_dashboard":
		if err := rejectMCPKeys(arguments, "limit"); err != nil {
			return nil, err
		}
		limit, present, err := mcpInt(arguments, "limit", 1, headlessMaxHealthHistory)
		if err != nil {
			return nil, err
		}
		args := base("health", "dashboard")
		if present {
			args = append(args, "--limit", strconv.Itoa(limit))
		}
		return args, nil
	case "teak_health_history":
		if err := rejectMCPKeys(arguments, "limit"); err != nil {
			return nil, err
		}
		limit, present, err := mcpInt(arguments, "limit", 1, headlessMaxHealthHistory)
		if err != nil {
			return nil, err
		}
		args := base("health", "history")
		if present {
			args = append(args, "--limit", strconv.Itoa(limit))
		}
		return args, nil
	case "teak_hitspec_validate":
		if err := rejectMCPKeys(arguments, "path"); err != nil {
			return nil, err
		}
		path, err := stringArg("path", true)
		if err != nil {
			return nil, err
		}
		return append(base("hitspec", "validate"), path), nil
	case "teak_git_status":
		if err := rejectMCPKeys(arguments); err != nil {
			return nil, err
		}
		return base("git", "status"), nil
	case "teak_session_show":
		if err := rejectMCPKeys(arguments, "name"); err != nil {
			return nil, err
		}
		args := base("session", "show")
		name, err := stringArg("name", false)
		if err != nil {
			return nil, err
		}
		if name != "" {
			args = append(args, "--name", name)
		}
		return args, nil
	case "teak_lsp_status":
		if err := rejectMCPKeys(arguments, "probe"); err != nil {
			return nil, err
		}
		args := base("lsp", "status")
		probe, present, err := mcpBool(arguments, "probe")
		if err != nil {
			return nil, err
		}
		if present && probe {
			args = append(args, "--probe")
		}
		return args, nil
	case "teak_lsp_diagnostics", "teak_lsp_format", "teak_lsp_symbols":
		if err := rejectMCPKeys(arguments, "path"); err != nil {
			return nil, err
		}
		path, err := stringArg("path", true)
		if err != nil {
			return nil, err
		}
		operation := strings.TrimPrefix(name, "teak_lsp_")
		return append(base("lsp", operation), path), nil
	case "teak_lsp_hover", "teak_lsp_definition", "teak_lsp_references":
		if err := rejectMCPKeys(arguments, "path", "line", "column"); err != nil {
			return nil, err
		}
		path, err := stringArg("path", true)
		if err != nil {
			return nil, err
		}
		line, present, err := mcpInt(arguments, "line", 0, maxHeadlessLSPPosition)
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, errors.New("tool argument \"line\" is required")
		}
		column, present, err := mcpInt(arguments, "column", 0, maxHeadlessLSPPosition)
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, errors.New("tool argument \"column\" is required")
		}
		operation := strings.TrimPrefix(name, "teak_lsp_")
		args := append(base("lsp", operation), "--line", strconv.Itoa(line), "--column", strconv.Itoa(column))
		return append(args, path), nil
	case "teak_dap_status":
		if err := rejectMCPKeys(arguments); err != nil {
			return nil, err
		}
		return base("dap", "status"), nil
	case "teak_dap_probe":
		if err := rejectMCPKeys(arguments, "adapter"); err != nil {
			return nil, err
		}
		args := base("dap", "probe")
		adapter, err := stringArg("adapter", false)
		if err != nil {
			return nil, err
		}
		if adapter != "" {
			args = append(args, "--adapter", adapter)
		}
		return args, nil
	case "teak_session_list":
		if err := rejectMCPKeys(arguments); err != nil {
			return nil, err
		}
		return base("session", "list"), nil
	case "teak_session_health":
		if err := rejectMCPKeys(arguments, "name"); err != nil {
			return nil, err
		}
		args := base("session", "health")
		name, err := stringArg("name", false)
		if err != nil {
			return nil, err
		}
		if name != "" {
			args = append(args, "--name", name)
		}
		return args, nil
	case "teak_agent_list":
		if err := rejectMCPKeys(arguments); err != nil {
			return nil, err
		}
		return base("agent", "list"), nil
	case "teak_agent_show":
		if err := rejectMCPKeys(arguments, "run_id"); err != nil {
			return nil, err
		}
		runID, err := stringArg("run_id", true)
		if err != nil {
			return nil, err
		}
		return append(base("agent", "show"), runID), nil
	case "teak_agent_cancel":
		if err := rejectMCPKeys(arguments, "run_id", "confirm"); err != nil {
			return nil, err
		}
		confirmed, present, err := mcpBool(arguments, "confirm")
		if err != nil {
			return nil, err
		}
		if !present || !confirmed {
			return nil, errors.New("teak_agent_cancel requires confirm: true")
		}
		runID, err := stringArg("run_id", true)
		if err != nil {
			return nil, err
		}
		args := append(base("agent", "cancel"), "--confirm")
		return append(args, runID), nil
	case "teak_agent_reap_stale":
		if err := rejectMCPKeys(arguments, "max_silence", "confirm"); err != nil {
			return nil, err
		}
		confirmed, present, err := mcpBool(arguments, "confirm")
		if err != nil {
			return nil, err
		}
		if !present || !confirmed {
			return nil, errors.New("teak_agent_reap_stale requires confirm: true")
		}
		maxSilence, err := stringArg("max_silence", true)
		if err != nil {
			return nil, err
		}
		args := append(base("agent", "reap-stale"), "--confirm", "--max-silence", maxSilence)
		return args, nil
	default:
		return nil, fmt.Errorf("tool %q is not available", name)
	}
}

func mcpToolInput(name string, raw json.RawMessage) ([]byte, bool, error) {
	if name != "teak_buffer_write" {
		return nil, false, nil
	}
	arguments, err := decodeMCPArguments(raw)
	if err != nil {
		return nil, false, err
	}
	_, _, content, err := mcpBufferWriteArguments(arguments)
	if err != nil {
		return nil, false, err
	}
	return []byte(content), true, nil
}

func mcpBufferWriteArguments(arguments map[string]json.RawMessage) (path, expectedSHA, content string, err error) {
	if err := rejectMCPKeys(arguments, "path", "expected_sha256", "content", "confirm"); err != nil {
		return "", "", "", err
	}
	confirmed, present, err := mcpBool(arguments, "confirm")
	if err != nil {
		return "", "", "", err
	}
	if !present || !confirmed {
		return "", "", "", errors.New("teak_buffer_write requires confirm: true")
	}
	path, err = mcpString(arguments, "path", true)
	if err != nil {
		return "", "", "", err
	}
	expectedSHA, err = mcpString(arguments, "expected_sha256", true)
	if err != nil {
		return "", "", "", err
	}
	if _, ok := arguments["content"]; !ok {
		return "", "", "", errors.New("tool argument \"content\" is required")
	}
	var contentValue *string
	if err := json.Unmarshal(arguments["content"], &contentValue); err != nil || contentValue == nil {
		return "", "", "", errors.New("tool argument \"content\" must be a string")
	}
	content = *contentValue
	if len(content) > headlessMCPMaxArgumentBytes {
		return "", "", "", fmt.Errorf("tool argument %q exceeds %d bytes", "content", headlessMCPMaxArgumentBytes)
	}
	return path, expectedSHA, content, nil
}

func decodeMCPArguments(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]json.RawMessage{}, nil
	}
	if len(raw) > headlessMCPMaxArgumentBytes {
		return nil, fmt.Errorf("tool arguments exceed %d bytes", headlessMCPMaxArgumentBytes)
	}
	var arguments map[string]json.RawMessage
	if err := json.Unmarshal(raw, &arguments); err != nil || arguments == nil {
		return nil, errors.New("tool arguments must be a JSON object")
	}
	return arguments, nil
}

func rejectMCPKeys(arguments map[string]json.RawMessage, allowed ...string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key := range arguments {
		if _, ok := set[key]; !ok {
			return fmt.Errorf("unknown tool argument %q", key)
		}
	}
	return nil
}

func mcpString(arguments map[string]json.RawMessage, key string, required bool) (string, error) {
	raw, ok := arguments[key]
	if !ok {
		if required {
			return "", fmt.Errorf("tool argument %q is required", key)
		}
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("tool argument %q must be a string", key)
	}
	value = strings.TrimSpace(value)
	if required && value == "" {
		return "", fmt.Errorf("tool argument %q must not be empty", key)
	}
	if len(value) > headlessMCPMaxArgumentBytes {
		return "", fmt.Errorf("tool argument %q exceeds %d bytes", key, headlessMCPMaxArgumentBytes)
	}
	return value, nil
}

func mcpBool(arguments map[string]json.RawMessage, key string) (bool, bool, error) {
	raw, ok := arguments[key]
	if !ok {
		return false, false, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, true, fmt.Errorf("tool argument %q must be boolean", key)
	}
	return value, true, nil
}

func mcpInt(arguments map[string]json.RawMessage, key string, min, max int) (int, bool, error) {
	raw, ok := arguments[key]
	if !ok {
		return 0, false, nil
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil || value < min || value > max {
		return 0, true, fmt.Errorf("tool argument %q must be an integer between %d and %d", key, min, max)
	}
	return value, true, nil
}

func (s *headlessMCPServer) removeRequest(key string) {
	s.activeMu.Lock()
	delete(s.active, key)
	s.activeMu.Unlock()
}

func (s *headlessMCPServer) cancelRequest(raw json.RawMessage) {
	var params struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(raw, &params) != nil || !hasMCPRequestID(params.RequestID) {
		return
	}
	s.activeMu.Lock()
	cancel := s.active[string(params.RequestID)]
	s.activeMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *headlessMCPServer) cancelAll() {
	s.activeMu.Lock()
	cancellations := make([]context.CancelFunc, 0, len(s.active))
	for _, cancel := range s.active {
		cancellations = append(cancellations, cancel)
	}
	s.activeMu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
}

func (s *headlessMCPServer) waitForActive(grace time.Duration) {
	if grace <= 0 {
		return
	}
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.activeMu.Lock()
		active := len(s.active)
		s.activeMu.Unlock()
		if active == 0 {
			return
		}
		select {
		case <-deadline.C:
			return
		case <-ticker.C:
		}
	}
}

func (s *headlessMCPServer) writeError(stdout io.Writer, id json.RawMessage, code int, message string) error {
	return s.writeResponse(stdout, headlessMCPResponse{
		JSONRPC: "2.0",
		ID:      normalizedMCPID(id),
		Error:   &headlessMCPError{Code: code, Message: message},
	})
}

func (s *headlessMCPServer) writeResponse(stdout io.Writer, response headlessMCPResponse) error {
	s.write.Lock()
	defer s.write.Unlock()
	return json.NewEncoder(stdout).Encode(response)
}

func hasMCPRequestID(id json.RawMessage) bool {
	id = bytes.TrimSpace(id)
	if len(id) == 0 || bytes.Equal(id, []byte("null")) {
		return false
	}
	switch id[0] {
	case '"':
		var value string
		return json.Unmarshal(id, &value) == nil
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		var value json.Number
		return json.Unmarshal(id, &value) == nil
	default:
		return false
	}
}

func mcpRequestIDPresent(id json.RawMessage) bool {
	id = bytes.TrimSpace(id)
	return len(id) > 0 && !bytes.Equal(id, []byte("null"))
}

func normalizedMCPID(id json.RawMessage) json.RawMessage {
	if !hasMCPRequestID(id) {
		return json.RawMessage("null")
	}
	return id
}

func headlessMCPTools() []headlessMCPTool {
	objectSchema := func(properties map[string]any) map[string]any {
		if properties == nil {
			properties = map[string]any{}
		}
		return map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	}
	requiredObjectSchema := func(properties map[string]any, required ...string) map[string]any {
		schema := objectSchema(properties)
		schema["required"] = required
		return schema
	}
	stringSchema := map[string]any{"type": "string", "minLength": 1, "maxLength": headlessMCPMaxArgumentBytes}
	depthSchema := map[string]any{"type": "integer", "minimum": 0, "maximum": headlessMaxContextDepth}
	positionSchema := map[string]any{"type": "integer", "minimum": 0, "maximum": maxHeadlessLSPPosition}
	confirmSchema := map[string]any{"type": "boolean", "const": true}
	return []headlessMCPTool{
		{Name: "teak_context", Description: "Read a bounded project context listing.", InputSchema: objectSchema(map[string]any{"depth": depthSchema})},
		{Name: "teak_project_list", Description: "Read a bounded project tree listing.", InputSchema: objectSchema(map[string]any{"depth": depthSchema})},
		{Name: "teak_project_stat", Description: "Read bounded metadata for one project path.", InputSchema: requiredObjectSchema(map[string]any{"path": stringSchema}, "path")},
		{Name: "teak_project_mkdir", Description: "Create one workspace-confined directory after explicit confirmation.", InputSchema: requiredObjectSchema(map[string]any{"path": stringSchema, "confirm": confirmSchema}, "path", "confirm")},
		{Name: "teak_project_rename", Description: "Rename one workspace-confined path after explicit confirmation.", InputSchema: requiredObjectSchema(map[string]any{"source": stringSchema, "destination": stringSchema, "confirm": confirmSchema}, "source", "destination", "confirm")},
		{Name: "teak_project_copy", Description: "Copy one bounded workspace-confined path after explicit confirmation.", InputSchema: requiredObjectSchema(map[string]any{"source": stringSchema, "destination": stringSchema, "confirm": confirmSchema}, "source", "destination", "confirm")},
		{Name: "teak_project_remove", Description: "Remove one bounded workspace-confined path after explicit confirmation.", InputSchema: requiredObjectSchema(map[string]any{"path": stringSchema, "confirm": confirmSchema}, "path", "confirm")},
		{Name: "teak_buffer_read", Description: "Read one workspace-confined buffer.", InputSchema: requiredObjectSchema(map[string]any{"path": stringSchema}, "path")},
		{Name: "teak_buffer_write", Description: "Write one bounded buffer only after explicit confirmation and an optimistic SHA-256 check.", InputSchema: requiredObjectSchema(map[string]any{
			"path":            stringSchema,
			"expected_sha256": stringSchema,
			"content":         map[string]any{"type": "string", "maxLength": headlessMCPMaxArgumentBytes},
			"confirm":         map[string]any{"type": "boolean", "const": true},
		}, "path", "expected_sha256", "content", "confirm")},
		{Name: "teak_search", Description: "Search text or an existing semantic index; semantic indexing requires explicit index and confirmation.", InputSchema: requiredObjectSchema(map[string]any{
			"query":          stringSchema,
			"regex":          map[string]any{"type": "boolean"},
			"case_sensitive": map[string]any{"type": "boolean"},
			"semantic":       map[string]any{"type": "boolean"},
			"index":          map[string]any{"type": "boolean", "description": "Build the semantic index before searching; requires confirm: true."},
			"confirm":        confirmSchema,
		}, "query")},
		{Name: "teak_codemap", Description: "Run a bounded read-only codemap query.", InputSchema: requiredObjectSchema(map[string]any{
			"operation": map[string]any{"type": "string", "enum": []string{"context", "callers", "callees", "find", "impact"}},
			"symbol":    stringSchema,
			"depth":     depthSchema,
		}, "operation", "symbol")},
		{Name: "teak_codemap_symbols", Description: "List bounded symbols defined in one workspace-relative file.", InputSchema: requiredObjectSchema(map[string]any{
			"path": stringSchema,
		}, "path")},
		{Name: "teak_codemap_symbol_at", Description: "Resolve the enclosing symbol at a bounded 0-based line in one workspace-relative file.", InputSchema: requiredObjectSchema(map[string]any{
			"path": stringSchema,
			"line": positionSchema,
		}, "path", "line")},
		{Name: "teak_tools_status", Description: "Inspect tool health without starting indexes.", InputSchema: objectSchema(nil)},
		{Name: "teak_health", Description: "Read a bounded aggregate project health snapshot.", InputSchema: objectSchema(nil)},
		{Name: "teak_health_dashboard", Description: "Read current project health with bounded explicit history and trend deltas.", InputSchema: objectSchema(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": headlessMaxHealthHistory}})},
		{Name: "teak_health_history", Description: "Read explicit bounded health snapshots recorded for this workspace.", InputSchema: objectSchema(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": headlessMaxHealthHistory}})},
		{Name: "teak_hitspec_validate", Description: "Validate a local hitspec contract without making requests.", InputSchema: requiredObjectSchema(map[string]any{"path": stringSchema}, "path")},
		{Name: "teak_git_status", Description: "Read repository status without mutation.", InputSchema: objectSchema(nil)},
		{Name: "teak_session_show", Description: "Read the current or named workspace session.", InputSchema: objectSchema(map[string]any{"name": stringSchema})},
		{Name: "teak_session_list", Description: "List named workspace sessions without mutation.", InputSchema: objectSchema(nil)},
		{Name: "teak_session_health", Description: "Read health for all or one named workspace session.", InputSchema: objectSchema(map[string]any{"name": stringSchema})},
		{Name: "teak_lsp_status", Description: "Inspect configured language servers; protocol probing is explicit and bounded.", InputSchema: objectSchema(map[string]any{"probe": map[string]any{"type": "boolean"}})},
		{Name: "teak_lsp_diagnostics", Description: "Read bounded diagnostics for one workspace file.", InputSchema: requiredObjectSchema(map[string]any{"path": stringSchema}, "path")},
		{Name: "teak_lsp_format", Description: "Preview bounded LSP formatting for one workspace file without applying it.", InputSchema: requiredObjectSchema(map[string]any{"path": stringSchema}, "path")},
		{Name: "teak_lsp_symbols", Description: "Read bounded document symbols for one workspace file.", InputSchema: requiredObjectSchema(map[string]any{"path": stringSchema}, "path")},
		{Name: "teak_lsp_hover", Description: "Read bounded hover information at a 0-based document position.", InputSchema: requiredObjectSchema(map[string]any{"path": stringSchema, "line": positionSchema, "column": positionSchema}, "path", "line", "column")},
		{Name: "teak_lsp_definition", Description: "Read bounded go-to-definition locations at a 0-based document position.", InputSchema: requiredObjectSchema(map[string]any{"path": stringSchema, "line": positionSchema, "column": positionSchema}, "path", "line", "column")},
		{Name: "teak_lsp_references", Description: "Read bounded reference locations at a 0-based document position.", InputSchema: requiredObjectSchema(map[string]any{"path": stringSchema, "line": positionSchema, "column": positionSchema}, "path", "line", "column")},
		{Name: "teak_dap_status", Description: "Inspect configured debug adapters without starting a debuggee.", InputSchema: objectSchema(nil)},
		{Name: "teak_dap_probe", Description: "Probe one debug adapter without starting a debuggee.", InputSchema: objectSchema(map[string]any{"adapter": stringSchema})},
		{Name: "teak_agent_list", Description: "Read durable agent run state without launching or recovering runs.", InputSchema: objectSchema(nil)},
		{Name: "teak_agent_show", Description: "Read one durable agent run without launching or recovering it.", InputSchema: requiredObjectSchema(map[string]any{"run_id": stringSchema}, "run_id")},
		{Name: "teak_agent_cancel", Description: "Cancel one durable agent run and its active descendants after explicit confirmation.", InputSchema: requiredObjectSchema(map[string]any{"run_id": stringSchema, "confirm": confirmSchema}, "run_id", "confirm")},
		{Name: "teak_agent_reap_stale", Description: "Interrupt durable agent runs whose heartbeat is stale after explicit confirmation.", InputSchema: requiredObjectSchema(map[string]any{"max_silence": stringSchema, "confirm": confirmSchema}, "max_silence", "confirm")},
	}
}

func runHeadlessMCPSubprocess(root string) headlessMCPCommandRunner {
	return func(ctx context.Context, args []string) ([]byte, []byte, int, error) {
		return runHeadlessMCPCommand(root, ctx, args, nil)
	}
}

func runHeadlessMCPSubprocessWithInput(root string) headlessMCPInputRunner {
	return func(ctx context.Context, args []string, input []byte) ([]byte, []byte, int, error) {
		return runHeadlessMCPCommand(root, ctx, args, input)
	}
}

func runHeadlessMCPCommand(root string, ctx context.Context, args []string, input []byte) ([]byte, []byte, int, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("resolve teak executable: %w", err)
	}
	commandArgs := append([]string{"headless"}, args...)
	cmd, err := toolpath.Command(ctx, executable, commandArgs...)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("resolve teak executable: %w", err)
	}
	cmd.Dir = root
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	stdout, stderr, runErr := toolpath.RunBounded(cmd, headlessMCPMaxOutputBytes, headlessMCPMaxErrorBytes)
	if runErr == nil {
		return stdout, stderr, 0, nil
	}
	if ctx.Err() != nil {
		return stdout, stderr, 0, ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return stdout, stderr, exitErr.ExitCode(), nil
	}
	return stdout, stderr, 0, runErr
}

func runHeadlessMCP(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runHeadlessMCPContext(context.Background(), args, stdin, stdout, stderr)
}

func runHeadlessMCPContext(parentCtx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	opts, positional, help, err := parseHeadlessArgs(args)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if len(positional) != 0 {
		return writeHeadlessError(stderr, fmt.Errorf("mcp does not accept positional arguments"))
	}
	if opts.json {
		return writeHeadlessError(stderr, fmt.Errorf("mcp owns stdout for JSON-RPC and does not accept --json"))
	}
	if opts.listen != "" || opts.token != "" {
		return writeHeadlessError(stderr, fmt.Errorf("mcp accepts only --root"))
	}
	root, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	server := newHeadlessMCPServer(root, nil)
	if err := server.serve(parentCtx, stdin, stdout, stderr); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Error: MCP transport: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	return 0
}
