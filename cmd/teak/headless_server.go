package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	headlessRESTReadHeaderTimeout  = 5 * time.Second
	headlessRESTReadTimeout        = 30 * time.Second
	headlessRESTWriteTimeout       = 30 * time.Second
	headlessRESTIdleTimeout        = 60 * time.Second
	headlessRESTMaxHeaderBytes     = 16 << 10
	headlessRESTMaxRequestURIBytes = 64 << 10
	headlessRESTMaxQueryValueBytes = 16 << 10
	headlessRESTMaxWorkspaces      = 32
	headlessRESTMaxWorkspaceName   = 64
	headlessRESTMaxProjectBody     = 16 << 10
	// JSON escaping can make a request larger than the UTF-8 buffer itself;
	// leave a bounded envelope for the path, hash, and JSON syntax.
	headlessRESTMaxWriteBody = headlessMaxBufferBytes + 64<<10
)

type headlessRESTServer struct {
	root             string
	token            string
	workspaces       map[string]string
	defaultWorkspace string
	quota            *headlessQuota
}

type headlessRESTHealthResponse struct {
	State     string                `json:"state"`
	Workspace string                `json:"workspace"`
	Quota     headlessQuotaSnapshot `json:"quota"`
}

type headlessRESTWorkspaceEntry struct {
	Name      string `json:"name"`
	Workspace string `json:"workspace"`
}

type headlessRESTWorkspacesResponse struct {
	State   string                       `json:"state"`
	Default string                       `json:"default"`
	Items   []headlessRESTWorkspaceEntry `json:"workspaces"`
}

type headlessRESTErrorResponse struct {
	State   string `json:"state"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type headlessRESTBufferWriteRequest struct {
	Path        string `json:"path"`
	ExpectedSHA string `json:"expected_sha256"`
	Content     string `json:"content"`
}

type headlessRESTProjectMutationRequest struct {
	Source      string `json:"source"`
	Destination string `json:"destination,omitempty"`
}

type headlessRESTServeResponse struct {
	State      string                       `json:"state"`
	Workspace  string                       `json:"workspace"`
	Default    string                       `json:"default"`
	Address    string                       `json:"address"`
	Workspaces []headlessRESTWorkspaceEntry `json:"workspaces"`
}

// newHeadlessRESTHandler returns the local control-plane adapter.
// The workspace is fixed when the server starts; requests cannot override it
// through a query parameter. Explicit mutations require a bearer token and a
// confirmation header, and delegate to the same bounded root-confined CLI
// implementation used by local and MCP callers.
func newHeadlessRESTHandler(root, token string) http.Handler {
	return newHeadlessRESTHandlerForWorkspaces(map[string]string{"default": root}, "default", token)
}

func newHeadlessRESTHandlerForWorkspaces(workspaces map[string]string, defaultName, token string) http.Handler {
	copyWorkspaces := make(map[string]string, len(workspaces))
	for name, root := range workspaces {
		copyWorkspaces[name] = root
	}
	root := copyWorkspaces[defaultName]
	return &headlessRESTServer{
		root:             root,
		token:            token,
		workspaces:       copyWorkspaces,
		defaultWorkspace: defaultName,
		quota:            newHeadlessQuota(headlessMaxConcurrentOperations, (headlessRESTOperationOutputReservation+headlessRESTErrorOutputLimit)*headlessMaxConcurrentOperations),
	}
}

func (s *headlessRESTServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet {
			headlessRESTError(w, http.StatusMethodNotAllowed, "method_not_allowed", "healthz only accepts GET")
			return
		}
		headlessRESTJSON(w, http.StatusOK, headlessRESTHealthResponse{State: "ready", Workspace: s.root, Quota: s.quotaSnapshot()})
		return
	}

	if !s.authorized(r) {
		headlessRESTError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	if err := validateHeadlessRESTRequest(r); err != nil {
		headlessRESTError(w, http.StatusRequestURITooLong, "request_too_long", err.Error())
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces" {
		s.handleWorkspaces(w)
		return
	}
	workspaceName, workspaceRoot, routePath, err := s.resolveRouteWorkspace(r.URL.Path)
	if err != nil {
		var notFound *headlessRESTNotFoundError
		if errors.As(err, &notFound) {
			headlessRESTError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		headlessRESTError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	quota := s.quota
	if quota == nil {
		quota = newHeadlessQuota(headlessMaxConcurrentOperations, (headlessRESTOperationOutputReservation+headlessRESTErrorOutputLimit)*headlessMaxConcurrentOperations)
	}
	release, quotaErr := quota.acquire(r.Context(), headlessRESTOperationOutputReservation+headlessRESTErrorOutputLimit)
	if quotaErr != nil {
		if errors.Is(quotaErr, context.Canceled) || errors.Is(quotaErr, context.DeadlineExceeded) {
			// Preserve each operation's structured cancellation envelope. The
			// downstream collector already avoids starting work for a canceled
			// request, while an early transport error would erase useful state
			// such as health=cancelled or a typed probe cancellation.
			release = nil
		} else {
			headlessRESTError(w, http.StatusTooManyRequests, "quota_exceeded", quotaErr.Error())
			return
		}
	}
	if release != nil {
		defer release()
	}
	_ = workspaceName
	if r.Method == http.MethodPost && routePath == "/v1/buffer/write" {
		s.handleBufferWrite(w, r, workspaceRoot)
		return
	}
	if r.Method == http.MethodPost {
		if operation, ok := headlessRESTProjectMutation(routePath); ok {
			s.handleProjectMutation(w, r, workspaceRoot, operation)
			return
		}
		if operation, ok := headlessRESTAgentMutation(routePath); ok {
			s.handleAgentMutation(w, r, workspaceRoot, operation)
			return
		}
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST")
		headlessRESTError(w, http.StatusMethodNotAllowed, "method_not_allowed", "the REST control plane only accepts GET and confirmed mutations")
		return
	}
	if routePath == "/v1/search" && strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("index")), "true") && !strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Teak-Confirm")), "true") {
		headlessRESTError(w, http.StatusPreconditionRequired, "confirmation_required", "semantic indexing requires X-Teak-Confirm: true")
		return
	}

	args, err := s.routeArgs(r, routePath, workspaceRoot)
	if err != nil {
		var notFound *headlessRESTNotFoundError
		if errors.As(err, &notFound) {
			headlessRESTError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		headlessRESTError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	s.dispatch(w, r, args)
}

// validateHeadlessRESTRequest keeps URL-derived arguments within the same
// bounded control-plane contract as MCP arguments. MaxHeaderBytes does not
// provide a useful per-value limit for callers using httptest or an embedded
// handler, and a very large search/symbol/path value would otherwise be copied
// into command arguments before the downstream operation can reject it.
func validateHeadlessRESTRequest(r *http.Request) error {
	if r == nil || r.URL == nil {
		return errors.New("request URL is missing")
	}
	if len(r.URL.EscapedPath())+len(r.URL.RawQuery) > headlessRESTMaxRequestURIBytes {
		return fmt.Errorf("request URI exceeds %d bytes", headlessRESTMaxRequestURIBytes)
	}
	for key, values := range r.URL.Query() {
		if len(key) > headlessRESTMaxQueryValueBytes {
			return fmt.Errorf("query parameter name %q exceeds %d bytes", key, headlessRESTMaxQueryValueBytes)
		}
		for _, value := range values {
			if len(value) > headlessRESTMaxQueryValueBytes {
				return fmt.Errorf("query parameter %q exceeds %d bytes", key, headlessRESTMaxQueryValueBytes)
			}
		}
	}
	return nil
}

func headlessRESTProjectMutation(routePath string) (string, bool) {
	const prefix = "/v1/project/"
	if !strings.HasPrefix(routePath, prefix) {
		return "", false
	}
	operation := strings.TrimPrefix(routePath, prefix)
	switch operation {
	case "mkdir", "rename", "copy", "remove":
		return operation, true
	default:
		return "", false
	}
}

func headlessRESTAgentMutation(routePath string) (string, bool) {
	if !strings.HasPrefix(routePath, "/v1/agent/") {
		return "", false
	}
	operation := strings.TrimPrefix(routePath, "/v1/agent/")
	switch operation {
	case "cancel", "reap-stale":
		return operation, true
	default:
		return "", false
	}
}

func (s *headlessRESTServer) handleWorkspaces(w http.ResponseWriter) {
	items := headlessRESTWorkspaceEntries(s.workspaces)
	headlessRESTJSON(w, http.StatusOK, headlessRESTWorkspacesResponse{
		State:   "ready",
		Default: s.defaultWorkspace,
		Items:   items,
	})
}

func headlessRESTWorkspaceEntries(workspaces map[string]string) []headlessRESTWorkspaceEntry {
	names := make([]string, 0, len(workspaces))
	for name := range workspaces {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]headlessRESTWorkspaceEntry, 0, len(names))
	for _, name := range names {
		items = append(items, headlessRESTWorkspaceEntry{Name: name, Workspace: workspaces[name]})
	}
	return items
}

func (s *headlessRESTServer) resolveRouteWorkspace(route string) (name, root, routePath string, err error) {
	if !strings.HasPrefix(route, "/v1/workspaces/") {
		root, ok := s.workspaces[s.defaultWorkspace]
		if !ok {
			return "", "", "", fmt.Errorf("default workspace %q is not registered", s.defaultWorkspace)
		}
		return s.defaultWorkspace, root, route, nil
	}
	rest := strings.TrimPrefix(route, "/v1/workspaces/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", &headlessRESTNotFoundError{path: route}
	}
	name = parts[0]
	root, ok := s.workspaces[name]
	if !ok {
		return "", "", "", &headlessRESTNotFoundError{path: route}
	}
	return name, root, "/v1/" + parts[1], nil
}

func (s *headlessRESTServer) handleBufferWrite(w http.ResponseWriter, r *http.Request, workspaceRoot string) {
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Teak-Confirm")), "true") {
		headlessRESTError(w, http.StatusPreconditionRequired, "confirmation_required", "buffer writes require X-Teak-Confirm: true")
		return
	}
	if writeHeadlessRESTContextError(w, r) {
		return
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, headlessRESTMaxWriteBody))
	decoder.DisallowUnknownFields()
	var request headlessRESTBufferWriteRequest
	if err := decoder.Decode(&request); err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "request body too large") {
			code = http.StatusRequestEntityTooLarge
		}
		headlessRESTError(w, code, "invalid_request", fmt.Sprintf("decode buffer write request: %v", err))
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			headlessRESTError(w, http.StatusBadRequest, "invalid_request", "buffer write request must contain one JSON object")
		} else {
			headlessRESTError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("decode buffer write request: %v", err))
		}
		return
	}
	if strings.TrimSpace(request.Path) == "" {
		headlessRESTError(w, http.StatusBadRequest, "invalid_request", "buffer write request requires path")
		return
	}
	if strings.TrimSpace(request.ExpectedSHA) == "" {
		headlessRESTError(w, http.StatusBadRequest, "invalid_request", "buffer write request requires expected_sha256")
		return
	}
	root, path, err := resolveHeadlessBufferTarget(workspaceRoot, request.Path)
	if err != nil {
		headlessRESTError(w, http.StatusBadRequest, "invalid_path", err.Error())
		return
	}
	response, err := writeHeadlessBufferContext(r.Context(), root, path, request.ExpectedSHA, []byte(request.Content))
	if err != nil {
		status := http.StatusUnprocessableEntity
		if headlessErrorCode(err) == "stale_write" {
			status = http.StatusConflict
		}
		headlessRESTError(w, status, headlessErrorCode(err), err.Error())
		return
	}
	headlessRESTJSON(w, http.StatusOK, response)
}

func (s *headlessRESTServer) handleProjectMutation(w http.ResponseWriter, r *http.Request, workspaceRoot, operation string) {
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Teak-Confirm")), "true") {
		headlessRESTError(w, http.StatusPreconditionRequired, "confirmation_required", "project mutations require X-Teak-Confirm: true")
		return
	}
	if writeHeadlessRESTContextError(w, r) {
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, headlessRESTMaxProjectBody))
	decoder.DisallowUnknownFields()
	var request headlessRESTProjectMutationRequest
	if err := decoder.Decode(&request); err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "request body too large") {
			code = http.StatusRequestEntityTooLarge
		}
		headlessRESTError(w, code, "invalid_request", fmt.Sprintf("decode project mutation request: %v", err))
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			headlessRESTError(w, http.StatusBadRequest, "invalid_request", "project mutation request must contain one JSON object")
		} else {
			headlessRESTError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("decode project mutation request: %v", err))
		}
		return
	}
	if strings.TrimSpace(request.Source) == "" {
		headlessRESTError(w, http.StatusBadRequest, "invalid_request", "project mutation request requires source")
		return
	}
	needsDestination := operation == "rename" || operation == "copy"
	if needsDestination && strings.TrimSpace(request.Destination) == "" {
		headlessRESTError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("project %s requires destination", operation))
		return
	}
	if !needsDestination && strings.TrimSpace(request.Destination) != "" {
		headlessRESTError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("project %s does not accept destination", operation))
		return
	}

	args := []string{"project", operation, "--confirm", "--json", "--root", workspaceRoot, request.Source}
	if needsDestination {
		args = append(args, request.Destination)
	}
	var stdout headlessLimitedBuffer
	stdout.limit = headlessRESTOperationOutputReservation
	var stderr headlessLimitedBuffer
	stderr.limit = headlessRESTErrorOutputLimit
	code := runHeadlessProjectContext(r.Context(), args[1:], &stdout, &stderr)
	if stdout.Exceeded() || stderr.Exceeded() {
		headlessRESTError(w, http.StatusUnprocessableEntity, "output_limit", "project operation exceeded its response output limit")
		return
	}
	if stdout.Len() == 0 && stderr.Len() > 0 {
		status := http.StatusUnprocessableEntity
		if code == 2 {
			status = http.StatusBadRequest
		}
		headlessRESTRawJSON(w, status, stderr.Bytes())
		return
	}
	if stdout.Len() == 0 {
		headlessRESTError(w, http.StatusInternalServerError, "empty_response", "project mutation returned no JSON response")
		return
	}
	status := headlessRESTOperationStatus(code, stdout.Bytes())
	headlessRESTRawJSON(w, status, stdout.Bytes())
}

func (s *headlessRESTServer) handleAgentMutation(w http.ResponseWriter, r *http.Request, workspaceRoot, operation string) {
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Teak-Confirm")), "true") {
		headlessRESTError(w, http.StatusPreconditionRequired, "confirmation_required", "agent mutations require X-Teak-Confirm: true")
		return
	}
	if writeHeadlessRESTContextError(w, r) {
		return
	}
	args, err := s.routeArgs(r, "/v1/agent/"+operation, workspaceRoot)
	if err != nil {
		var notFound *headlessRESTNotFoundError
		if errors.As(err, &notFound) {
			headlessRESTError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		headlessRESTError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	s.dispatch(w, r, args)
}

func (s *headlessRESTServer) authorized(r *http.Request) bool {
	provided := strings.TrimSpace(r.Header.Get("X-Teak-Token"))
	if provided == "" {
		const prefix = "Bearer "
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(authorization, prefix) {
			provided = strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
		}
	}
	if s.token == "" || provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) == 1
}

func (s *headlessRESTServer) routeArgs(r *http.Request, routePath, workspaceRoot string) ([]string, error) {
	query := r.URL.Query()
	base := func(command string, operation ...string) []string {
		args := []string{command}
		args = append(args, operation...)
		args = append(args, "--json", "--root", workspaceRoot)
		return args
	}
	withDepth := func(args []string) []string {
		if value := strings.TrimSpace(query.Get("depth")); value != "" {
			args = append(args, "--depth", value)
		}
		return args
	}
	requireQuery := func(name string) (string, error) {
		value := strings.TrimSpace(query.Get(name))
		if value == "" {
			return "", fmt.Errorf("query parameter %q is required", name)
		}
		return value, nil
	}
	requirePosition := func(name string) (string, error) {
		value, err := requireQuery(name)
		if err != nil {
			return "", err
		}
		position, parseErr := strconv.Atoi(value)
		if parseErr != nil || position < 0 || position > maxHeadlessLSPPosition {
			return "", fmt.Errorf("query parameter %q must be an integer between 0 and %d", name, maxHeadlessLSPPosition)
		}
		return strconv.Itoa(position), nil
	}
	appendBool := func(args []string, queryName, flag string) ([]string, error) {
		value := strings.TrimSpace(query.Get(queryName))
		if value == "" {
			return args, nil
		}
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("query parameter %q must be boolean", queryName)
		}
		if parsed {
			args = append(args, flag)
		}
		return args, nil
	}
	if strings.HasPrefix(routePath, "/v1/codemap/") {
		operation := strings.TrimPrefix(routePath, "/v1/codemap/")
		switch operation {
		case "symbols":
			file, err := requireQuery("path")
			if err != nil {
				return nil, err
			}
			return append(base("codemap", "symbols"), file), nil
		case "symbol-at":
			file, err := requireQuery("path")
			if err != nil {
				return nil, err
			}
			line, err := requirePosition("line")
			if err != nil {
				return nil, err
			}
			args := base("codemap", "symbol-at")
			args = append(args, "--line", line, file)
			return args, nil
		case "context", "callers", "callees", "find", "impact":
		default:
			return nil, &headlessRESTNotFoundError{path: routePath}
		}
		symbol, err := requireQuery("symbol")
		if err != nil {
			return nil, err
		}
		args := base("codemap", operation)
		args = withDepth(args)
		return append(args, symbol), nil
	}

	switch routePath {
	case "/v1/context":
		return withDepth(base("context")), nil
	case "/v1/project/list":
		return withDepth(base("project", "list")), nil
	case "/v1/project/stat":
		args := withDepth(base("project", "stat"))
		if path := strings.TrimSpace(query.Get("path")); path != "" {
			args = append(args, path)
		}
		return args, nil
	case "/v1/buffer/read":
		path, err := requireQuery("path")
		if err != nil {
			return nil, err
		}
		return append(base("buffer", "read"), path), nil
	case "/v1/search":
		queryText, err := requireQuery("q")
		if err != nil {
			return nil, err
		}
		args := base("search")
		if args, err = appendBool(args, "regex", "--regex"); err != nil {
			return nil, err
		}
		if args, err = appendBool(args, "case_sensitive", "--case-sensitive"); err != nil {
			return nil, err
		}
		semantic := false
		if value := strings.TrimSpace(query.Get("semantic")); value != "" {
			parsed, parseErr := strconv.ParseBool(value)
			if parseErr != nil {
				return nil, fmt.Errorf("query parameter %q must be boolean", "semantic")
			}
			semantic = parsed
			if parsed {
				args = append(args, "--semantic")
			}
		}
		if value := strings.TrimSpace(query.Get("index")); value != "" {
			index, parseErr := strconv.ParseBool(value)
			if parseErr != nil {
				return nil, fmt.Errorf("query parameter %q must be boolean", "index")
			}
			if index {
				if !semantic {
					return nil, errors.New("query parameter \"index\" requires semantic=true")
				}
				args = append(args, "--index")
			}
		}
		return append(args, queryText), nil
	case "/v1/tools/status":
		return base("tools", "status"), nil
	case "/v1/health":
		return base("health"), nil
	case "/v1/health/dashboard":
		args := base("health", "dashboard")
		if limit := strings.TrimSpace(query.Get("limit")); limit != "" {
			args = append(args, "--limit", limit)
		}
		return args, nil
	case "/v1/health/history":
		args := base("health", "history")
		if limit := strings.TrimSpace(query.Get("limit")); limit != "" {
			args = append(args, "--limit", limit)
		}
		return args, nil
	case "/v1/hitspec/validate":
		path, err := requireQuery("path")
		if err != nil {
			return nil, err
		}
		return append(base("hitspec", "validate"), path), nil
	case "/v1/git/status":
		return base("git", "status"), nil
	case "/v1/session/show":
		args := base("session", "show")
		if name := strings.TrimSpace(query.Get("name")); name != "" {
			args = append(args, "--name", name)
		}
		return args, nil
	case "/v1/session/list":
		return base("session", "list"), nil
	case "/v1/session/health":
		args := base("session", "health")
		if name := strings.TrimSpace(query.Get("name")); name != "" {
			args = append(args, "--name", name)
		}
		return args, nil
	case "/v1/lsp/status":
		args := base("lsp", "status")
		return appendBool(args, "probe", "--probe")
	case "/v1/lsp/diagnostics", "/v1/lsp/format", "/v1/lsp/symbols":
		path, err := requireQuery("path")
		if err != nil {
			return nil, err
		}
		operation := strings.TrimPrefix(routePath, "/v1/lsp/")
		return append(base("lsp", operation), path), nil
	case "/v1/lsp/hover", "/v1/lsp/definition", "/v1/lsp/references":
		path, err := requireQuery("path")
		if err != nil {
			return nil, err
		}
		line, err := requirePosition("line")
		if err != nil {
			return nil, err
		}
		column, err := requirePosition("column")
		if err != nil {
			return nil, err
		}
		operation := strings.TrimPrefix(routePath, "/v1/lsp/")
		args := append(base("lsp", operation), "--line", line, "--column", column)
		return append(args, path), nil
	case "/v1/dap/status":
		return base("dap", "status"), nil
	case "/v1/dap/probe":
		args := base("dap", "probe")
		if adapter := strings.TrimSpace(query.Get("adapter")); adapter != "" {
			args = append(args, "--adapter", adapter)
		}
		if adapterArgs := query["arg"]; len(adapterArgs) > 0 {
			args = append(args, "--")
			for _, value := range adapterArgs {
				if strings.TrimSpace(value) != "" {
					args = append(args, value)
				}
			}
		}
		return args, nil
	case "/v1/agent/list":
		return base("agent", "list"), nil
	case "/v1/agent/show":
		runID, err := requireQuery("run_id")
		if err != nil {
			return nil, err
		}
		return append(base("agent", "show"), runID), nil
	case "/v1/agent/cancel":
		runID, err := requireQuery("run_id")
		if err != nil {
			return nil, err
		}
		args := append(base("agent", "cancel"), "--confirm")
		return append(args, runID), nil
	case "/v1/agent/reap-stale":
		maxSilence, err := requireQuery("max_silence")
		if err != nil {
			return nil, err
		}
		args := append(base("agent", "reap-stale"), "--confirm", "--max-silence", maxSilence)
		return args, nil
	case "/v1/health/tools":
		return base("tools", "status"), nil
	default:
		return nil, &headlessRESTNotFoundError{path: routePath}
	}
}

type headlessRESTNotFoundError struct {
	path string
}

func (e *headlessRESTNotFoundError) Error() string {
	return fmt.Sprintf("unknown REST route %q", e.path)
}

func (s *headlessRESTServer) dispatch(w http.ResponseWriter, r *http.Request, args []string) {
	var stdout headlessLimitedBuffer
	stdout.limit = headlessRESTOperationOutputReservation
	var stderr headlessLimitedBuffer
	stderr.limit = headlessRESTErrorOutputLimit
	code := runHeadlessCLIContext(r.Context(), args, strings.NewReader(""), &stdout, &stderr)
	if stdout.Exceeded() || stderr.Exceeded() {
		headlessRESTError(w, http.StatusUnprocessableEntity, "output_limit", "headless operation exceeded its response output limit")
		return
	}
	if stdout.Len() == 0 && stderr.Len() > 0 {
		status := http.StatusUnprocessableEntity
		if code == 2 {
			status = http.StatusBadRequest
		}
		headlessRESTRawJSON(w, status, stderr.Bytes())
		return
	}
	if stdout.Len() == 0 {
		headlessRESTError(w, http.StatusInternalServerError, "empty_response", "headless operation returned no JSON response")
		return
	}
	status := headlessRESTOperationStatus(code, stdout.Bytes())
	headlessRESTRawJSON(w, status, stdout.Bytes())
}

func (s *headlessRESTServer) quotaSnapshot() headlessQuotaSnapshot {
	if s == nil || s.quota == nil {
		return headlessQuotaSnapshot{}
	}
	return s.quota.snapshot()
}

// headlessRESTOperationStatus preserves the distinction between a valid JSON
// capability/health envelope and a successful operation. Headless commands
// intentionally emit structured responses for both actionable availability
// states and real failures; only the latter must not be flattened into HTTP
// 200 merely because stdout contains JSON.
func headlessRESTOperationStatus(code int, body []byte) int {
	switch code {
	case 0:
		return http.StatusOK
	case 2:
		return http.StatusBadRequest
	}

	// A capability/health response is still a valid representation when the
	// optional tool is absent or its index is stale. Preserve those states as
	// HTTP 200 so clients can inspect and remediate them. A command-level
	// failure that managed to emit JSON remains a transport failure.
	var envelope struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		switch envelope.State {
		case "missing", "unavailable", "unsupported", "stale":
			return http.StatusOK
		}
	}
	return http.StatusUnprocessableEntity
}

func headlessRESTJSON(w http.ResponseWriter, status int, value any) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(value); err != nil {
		headlessRESTError(w, http.StatusInternalServerError, "encode_response", err.Error())
		return
	}
	headlessRESTRawJSON(w, status, body.Bytes())
}

func headlessRESTRawJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(addHeadlessSchemaVersion(body))
}

func headlessRESTError(w http.ResponseWriter, status int, code, message string) {
	headlessRESTJSON(w, status, headlessRESTErrorResponse{State: "error", Code: code, Message: message})
}

func writeHeadlessRESTContextError(w http.ResponseWriter, r *http.Request) bool {
	if r == nil || r.Context() == nil {
		return false
	}
	err := r.Context().Err()
	if err == nil {
		return false
	}
	code := "request_cancelled"
	if errors.Is(err, context.DeadlineExceeded) {
		code = "timeout"
	}
	headlessRESTError(w, http.StatusRequestTimeout, code, err.Error())
	return true
}

func resolveHeadlessServeWorkspaces(root string, specs []string) (map[string]string, string, error) {
	workspaceCount := len(specs)
	if strings.TrimSpace(root) != "" {
		workspaceCount++
	}
	if workspaceCount > headlessRESTMaxWorkspaces {
		return nil, "", fmt.Errorf("serve accepts at most %d workspaces", headlessRESTMaxWorkspaces)
	}

	workspaces := make(map[string]string, workspaceCount)
	seenRoots := make(map[string]string, workspaceCount)
	defaultName := ""
	add := func(name, requestedRoot string) error {
		if err := validateHeadlessWorkspaceName(name); err != nil {
			return err
		}
		if _, exists := workspaces[name]; exists {
			return fmt.Errorf("workspace name %q is registered more than once", name)
		}
		resolved, err := doctorWorkspace(requestedRoot)
		if err != nil {
			return fmt.Errorf("workspace %q: %w", name, err)
		}
		canonical, err := filepath.EvalSymlinks(resolved)
		if err != nil {
			return fmt.Errorf("canonicalize workspace %q: %w", resolved, err)
		}
		canonical, err = filepath.Abs(canonical)
		if err != nil {
			return fmt.Errorf("resolve canonical workspace %q: %w", canonical, err)
		}
		key := filepath.Clean(canonical)
		if previous, exists := seenRoots[key]; exists {
			return fmt.Errorf("workspace %q reuses the directory already registered as %q", name, previous)
		}
		seenRoots[key] = name
		// Keep the user-visible absolute spelling (for example /var on macOS)
		// while using the symlink-resolved path only for duplicate detection.
		workspaces[name] = resolved
		if defaultName == "" {
			defaultName = name
		}
		return nil
	}

	if strings.TrimSpace(root) != "" {
		if err := add("default", root); err != nil {
			return nil, "", err
		}
	}
	for _, spec := range specs {
		name, requestedRoot, ok := strings.Cut(strings.TrimSpace(spec), "=")
		if !ok || strings.TrimSpace(requestedRoot) == "" {
			return nil, "", fmt.Errorf("--workspace requires <name>=<directory>, got %q", spec)
		}
		if err := add(strings.TrimSpace(name), strings.TrimSpace(requestedRoot)); err != nil {
			return nil, "", err
		}
	}
	if len(workspaces) == 0 {
		if err := add("default", ""); err != nil {
			return nil, "", err
		}
	}
	return workspaces, defaultName, nil
}

func validateHeadlessWorkspaceName(name string) error {
	if name == "" {
		return fmt.Errorf("workspace name cannot be empty")
	}
	if len(name) > headlessRESTMaxWorkspaceName {
		return fmt.Errorf("workspace name %q exceeds %d bytes", name, headlessRESTMaxWorkspaceName)
	}
	for index, char := range []byte(name) {
		valid := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-'
		if index == 0 {
			valid = (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
		}
		if !valid {
			return fmt.Errorf("workspace name %q must use only letters, digits, '_' or '-' and start with a letter or digit", name)
		}
	}
	return nil
}

func runHeadlessServe(args []string, stdout, stderr io.Writer) int {
	opts, positional, help, err := parseHeadlessArgs(args)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if len(positional) != 0 {
		return writeHeadlessError(stderr, fmt.Errorf("serve does not accept positional arguments"))
	}
	if strings.TrimSpace(opts.listen) == "" {
		return writeHeadlessError(stderr, fmt.Errorf("serve requires --listen <loopback-address>"))
	}
	if strings.TrimSpace(opts.token) == "" {
		return writeHeadlessError(stderr, fmt.Errorf("serve requires --token"))
	}
	if err := validateHeadlessListenAddress(opts.listen); err != nil {
		return writeHeadlessError(stderr, err)
	}
	workspaces, defaultWorkspace, err := resolveHeadlessServeWorkspaces(opts.root, opts.workspaces)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	root := workspaces[defaultWorkspace]
	listener, err := net.Listen("tcp", opts.listen)
	if err != nil {
		return writeHeadlessError(stderr, fmt.Errorf("listen for REST control plane: %w", err))
	}
	defer listener.Close()

	server := newHeadlessRESTHTTPServer(newHeadlessRESTHandlerForWorkspaces(workspaces, defaultWorkspace, opts.token))
	if opts.json {
		if code := writeHeadlessJSON(stdout, headlessRESTServeResponse{
			State:      "ready",
			Workspace:  root,
			Default:    defaultWorkspace,
			Address:    listener.Addr().String(),
			Workspaces: headlessRESTWorkspaceEntries(workspaces),
		}); code != 0 {
			return code
		}
	} else {
		fmt.Fprintf(stdout, "REST control plane listening on %s\nDefault workspace (%s): %s\n", listener.Addr(), defaultWorkspace, root)
		for _, entry := range headlessRESTWorkspaceEntries(workspaces) {
			fmt.Fprintf(stdout, "Workspace %s: %s\n", entry.Name, entry.Workspace)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownDone := make(chan struct{})
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
		close(shutdownDone)
	}()
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		<-shutdownDone
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "Error: REST control plane: %v\n", err)
		return 1
	}
	return 0
}

func newHeadlessRESTHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: headlessRESTReadHeaderTimeout,
		ReadTimeout:       headlessRESTReadTimeout,
		WriteTimeout:      headlessRESTWriteTimeout,
		IdleTimeout:       headlessRESTIdleTimeout,
		MaxHeaderBytes:    headlessRESTMaxHeaderBytes,
	}
}

func validateHeadlessListenAddress(address string) error {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("invalid --listen address %q: %w", address, err)
	}
	if host == "" {
		return fmt.Errorf("--listen must name a loopback host; refusing wildcard bind")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("--listen address %q is not loopback; refusing network exposure", address)
	}
	return nil
}
