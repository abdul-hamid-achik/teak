package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"teak/internal/lsp"
	"teak/internal/text"
	"teak/internal/toolpath"
)

const (
	maxHeadlessLSPQuerySymbols   = 2_048
	maxHeadlessLSPQueryLocations = 2_048
	maxHeadlessLSPHoverBytes     = 64 << 10
	maxHeadlessLSPQueryDepth     = 32
	maxHeadlessLSPPosition       = 10_000_000
)

type headlessLSPSymbolsResponse struct {
	Workspace    string              `json:"workspace"`
	Path         string              `json:"path"`
	RelativePath string              `json:"relative_path"`
	LanguageID   string              `json:"language_id,omitempty"`
	Server       string              `json:"server,omitempty"`
	State        string              `json:"state"`
	Symbols      []headlessLSPSymbol `json:"symbols"`
	Truncated    bool                `json:"truncated"`
	Detail       string              `json:"detail,omitempty"`
	Hint         string              `json:"hint,omitempty"`
}

type headlessLSPSymbol struct {
	Name      string              `json:"name"`
	Detail    string              `json:"detail,omitempty"`
	Kind      int                 `json:"kind"`
	Line      int                 `json:"line"`
	Column    int                 `json:"column"`
	EndLine   int                 `json:"end_line"`
	EndColumn int                 `json:"end_column"`
	Children  []headlessLSPSymbol `json:"children,omitempty"`
}

type headlessLSPHoverResponse struct {
	Workspace    string `json:"workspace"`
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	LanguageID   string `json:"language_id,omitempty"`
	Server       string `json:"server,omitempty"`
	State        string `json:"state"`
	Line         int    `json:"line"`
	Column       int    `json:"column"`
	Found        bool   `json:"found"`
	Truncated    bool   `json:"truncated"`
	Content      string `json:"content,omitempty"`
	Detail       string `json:"detail,omitempty"`
	Hint         string `json:"hint,omitempty"`
}

type headlessLSPLocationsResponse struct {
	Workspace    string                `json:"workspace"`
	Path         string                `json:"path"`
	RelativePath string                `json:"relative_path"`
	LanguageID   string                `json:"language_id,omitempty"`
	Server       string                `json:"server,omitempty"`
	State        string                `json:"state"`
	Line         int                   `json:"line"`
	Column       int                   `json:"column"`
	Locations    []headlessLSPLocation `json:"locations"`
	Skipped      int                   `json:"skipped,omitempty"`
	Truncated    bool                  `json:"truncated"`
	Detail       string                `json:"detail,omitempty"`
	Hint         string                `json:"hint,omitempty"`
}

type headlessLSPLocation struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"end_line"`
	EndColumn int    `json:"end_column"`
}

type headlessLSPDocumentSession struct {
	ctx      context.Context
	cancel   context.CancelFunc
	root     string
	path     string
	buffer   headlessBufferResponse
	server   lsp.ServerConfig
	client   *lsp.Client
	deadline time.Time
	state    string
	detail   string
	hint     string
}

func runHeadlessLSPIntelligenceContext(ctx context.Context, root, operation string, opts headlessOptions, positional []string, stdout, stderr io.Writer) int {
	if operation == "symbols" {
		if len(positional) != 1 {
			return writeHeadlessError(stderr, errors.New("lsp symbols requires exactly one path"))
		}
		if opts.lineSet || opts.columnSet {
			return writeHeadlessError(stderr, errors.New("lsp symbols does not accept --line or --column"))
		}
	} else {
		if len(positional) != 1 {
			return writeHeadlessError(stderr, fmt.Errorf("lsp %s requires exactly one path", operation))
		}
		if !opts.lineSet || !opts.columnSet {
			return writeHeadlessError(stderr, fmt.Errorf("lsp %s requires both --line and --column", operation))
		}
	}
	workspace, path, err := resolveHeadlessBufferTarget(root, positional[0])
	if err != nil {
		return writeHeadlessError(stderr, err)
	}

	switch operation {
	case "symbols":
		response, collectErr := collectHeadlessLSPSymbolsContext(ctx, workspace, path)
		if collectErr != nil {
			return writeHeadlessError(stderr, collectErr)
		}
		if opts.json {
			if code := writeHeadlessJSON(stdout, response); code != 0 {
				return code
			}
		} else {
			fmt.Fprintf(stdout, "Workspace: %s\nFile: %s\nServer: %s\nState: %s\nSymbols: %d\n", response.Workspace, response.RelativePath, response.Server, response.State, len(response.Symbols))
			writeHeadlessSymbolsText(stdout, response.Symbols, 0)
			if response.Detail != "" {
				fmt.Fprintln(stdout, response.Detail)
			}
		}
		return headlessLSPQueryExitCode(response.State)
	case "hover":
		response, collectErr := collectHeadlessLSPHoverContext(ctx, workspace, path, opts.line, opts.column)
		if collectErr != nil {
			return writeHeadlessError(stderr, collectErr)
		}
		if opts.json {
			if code := writeHeadlessJSON(stdout, response); code != 0 {
				return code
			}
		} else {
			fmt.Fprintf(stdout, "Workspace: %s\nFile: %s\nServer: %s\nState: %s\nFound: %t\n", response.Workspace, response.RelativePath, response.Server, response.State, response.Found)
			if response.Content != "" {
				fmt.Fprintln(stdout, response.Content)
			}
			if response.Detail != "" {
				fmt.Fprintln(stdout, response.Detail)
			}
		}
		return headlessLSPQueryExitCode(response.State)
	case "definition", "references":
		response, collectErr := collectHeadlessLSPLocationsContext(ctx, workspace, path, operation, opts.line, opts.column)
		if collectErr != nil {
			return writeHeadlessError(stderr, collectErr)
		}
		if opts.json {
			if code := writeHeadlessJSON(stdout, response); code != 0 {
				return code
			}
		} else {
			fmt.Fprintf(stdout, "Workspace: %s\nFile: %s\nServer: %s\nState: %s\nLocations: %d\n", response.Workspace, response.RelativePath, response.Server, response.State, len(response.Locations))
			for _, location := range response.Locations {
				fmt.Fprintf(stdout, "%s:%d:%d\n", location.Path, location.Line+1, location.Column+1)
			}
			if response.Detail != "" {
				fmt.Fprintln(stdout, response.Detail)
			}
		}
		return headlessLSPQueryExitCode(response.State)
	default:
		return writeHeadlessError(stderr, fmt.Errorf("unsupported LSP intelligence operation %q", operation))
	}
}

func headlessLSPQueryExitCode(state string) int {
	switch state {
	case "failed", "missing", "unsupported", "timed_out", "cancelled":
		return 1
	default:
		return 0
	}
}

func startHeadlessLSPDocumentContext(parentCtx context.Context, root, path string) (*headlessLSPDocumentSession, error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	operationCtx, cancel := context.WithTimeout(parentCtx, headlessLSPOperationTimeout)
	session := &headlessLSPDocumentSession{ctx: operationCtx, cancel: cancel, root: root, path: path, state: "unsupported"}
	buffer, err := readHeadlessBufferContext(operationCtx, root, path)
	if err != nil {
		cancel()
		return nil, err
	}
	session.buffer = buffer
	session.detail = "no language server is configured for this file type"
	server, configured, err := headlessLSPConfigForFile(path)
	if err != nil {
		cancel()
		return nil, err
	}
	if !configured {
		return session, nil
	}
	session.server = server
	if _, err := toolpath.Resolve(server.Command); err != nil {
		session.state = "missing"
		session.detail = "language server is not installed"
		if missing, ok := err.(*toolpath.MissingToolError); ok {
			session.hint = missing.Hint
		}
		return session, nil
	}
	messages := make(chan any, 32)
	client, err := lsp.NewClient(server, root, messages)
	if err != nil {
		session.state = "failed"
		session.detail = fmt.Sprintf("start language server: %v", err)
		return session, nil
	}
	session.client = client
	session.deadline = time.Now().Add(headlessLSPOperationTimeout)
	if initErr, timedOut := runHeadlessLSPStageContext(operationCtx, time.Until(session.deadline), client.Initialize); timedOut {
		session.state = headlessLSPContextState(initErr)
		session.detail = headlessLSPStageDetail("language server initialization", session.state)
		return session, nil
	} else if initErr != nil {
		session.state = "failed"
		session.detail = fmt.Sprintf("initialize language server: %v", initErr)
		return session, nil
	}
	client.DidOpen(lsp.FileURI(path), server.LanguageID, 1, buffer.Content)
	session.state = "ready"
	session.detail = "language server query ready"
	return session, nil
}

func (s *headlessLSPDocumentSession) close() {
	if s == nil {
		return
	}
	if s.client != nil {
		s.client.Shutdown()
		if s.ctx.Err() != nil {
			s.client.Terminate()
		}
		waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = s.client.WaitForShutdown(waitCtx)
		cancel()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func headlessLSPPositionError(buffer headlessBufferResponse, line, column int) error {
	rope := newHeadlessTextRope(buffer.Content)
	if _, err := headlessTextEditOffset(rope, line, column); err != nil {
		return err
	}
	return nil
}

func newHeadlessTextRope(content string) *text.Rope {
	return text.New([]byte(content))
}

func collectHeadlessLSPSymbolsContext(parentCtx context.Context, root, path string) (headlessLSPSymbolsResponse, error) {
	session, err := startHeadlessLSPDocumentContext(parentCtx, root, path)
	if err != nil {
		return headlessLSPSymbolsResponse{}, err
	}
	defer session.close()
	response := headlessLSPSymbolsResponse{
		Workspace: session.root, Path: session.path, RelativePath: session.buffer.RelativePath,
		LanguageID: session.server.LanguageID, Server: session.server.Command,
		State: session.state, Symbols: make([]headlessLSPSymbol, 0), Detail: session.detail, Hint: session.hint,
	}
	if session.state != "ready" {
		return response, nil
	}
	if !session.client.SupportsDocumentSymbol() {
		response.State = "unsupported"
		response.Detail = "language server does not support document symbols"
		return response, nil
	}
	var symbols []lsp.DocumentSymbol
	err, timedOut := runHeadlessLSPStageContext(session.ctx, time.Until(session.deadline), func() error {
		var queryErr error
		symbols, queryErr = session.client.DocumentSymbol(lsp.FileURI(path))
		return queryErr
	})
	if timedOut {
		response.State = headlessLSPContextState(err)
		response.Detail = headlessLSPStageDetail("language server document symbols request", response.State)
		return response, nil
	}
	if err != nil {
		response.State = "failed"
		response.Detail = fmt.Sprintf("document symbols request failed: %v", err)
		return response, nil
	}
	response.Symbols, response.Truncated = boundedHeadlessLSPSymbols(symbols)
	response.Detail = "document symbols received"
	return response, nil
}

func collectHeadlessLSPHoverContext(parentCtx context.Context, root, path string, line, column int) (headlessLSPHoverResponse, error) {
	session, err := startHeadlessLSPDocumentContext(parentCtx, root, path)
	if err != nil {
		return headlessLSPHoverResponse{}, err
	}
	defer session.close()
	response := headlessLSPHoverResponse{
		Workspace: session.root, Path: session.path, RelativePath: session.buffer.RelativePath,
		LanguageID: session.server.LanguageID, Server: session.server.Command,
		State: session.state, Line: line, Column: column, Detail: session.detail, Hint: session.hint,
	}
	if session.state != "ready" {
		return response, nil
	}
	if err := headlessLSPPositionError(session.buffer, line, column); err != nil {
		response.State = "failed"
		response.Detail = fmt.Sprintf("invalid LSP position: %v", err)
		return response, nil
	}
	if !session.client.SupportsHover() {
		response.State = "unsupported"
		response.Detail = "language server does not support hover"
		return response, nil
	}
	var hover *lsp.HoverResult
	err, timedOut := runHeadlessLSPStageContext(session.ctx, time.Until(session.deadline), func() error {
		var queryErr error
		hover, queryErr = session.client.Hover(lsp.FileURI(path), line, column)
		return queryErr
	})
	if timedOut {
		response.State = headlessLSPContextState(err)
		response.Detail = headlessLSPStageDetail("language server hover request", response.State)
		return response, nil
	}
	if err != nil {
		response.State = "failed"
		response.Detail = fmt.Sprintf("hover request failed: %v", err)
		return response, nil
	}
	response.State = "ready"
	response.Detail = "hover received"
	if hover != nil {
		response.Found = true
		response.Content, response.Truncated = boundedHeadlessLSPContent(hover.Content)
	}
	return response, nil
}

func collectHeadlessLSPLocationsContext(parentCtx context.Context, root, path, operation string, line, column int) (headlessLSPLocationsResponse, error) {
	session, err := startHeadlessLSPDocumentContext(parentCtx, root, path)
	if err != nil {
		return headlessLSPLocationsResponse{}, err
	}
	defer session.close()
	response := headlessLSPLocationsResponse{
		Workspace: session.root, Path: session.path, RelativePath: session.buffer.RelativePath,
		LanguageID: session.server.LanguageID, Server: session.server.Command,
		State: session.state, Line: line, Column: column, Locations: make([]headlessLSPLocation, 0),
		Detail: session.detail, Hint: session.hint,
	}
	if session.state != "ready" {
		return response, nil
	}
	if err := headlessLSPPositionError(session.buffer, line, column); err != nil {
		response.State = "failed"
		response.Detail = fmt.Sprintf("invalid LSP position: %v", err)
		return response, nil
	}
	supports := session.client.SupportsDefinition
	if operation == "references" {
		supports = session.client.SupportsReferences
	}
	if !supports() {
		response.State = "unsupported"
		response.Detail = "language server does not support " + operation
		return response, nil
	}
	var locations []lsp.Location
	err, timedOut := runHeadlessLSPStageContext(session.ctx, time.Until(session.deadline), func() error {
		var queryErr error
		if operation == "references" {
			locations, queryErr = session.client.References(lsp.FileURI(path), line, column)
		} else {
			locations, queryErr = session.client.Definition(lsp.FileURI(path), line, column)
		}
		return queryErr
	})
	if timedOut {
		response.State = headlessLSPContextState(err)
		response.Detail = headlessLSPStageDetail("language server "+operation+" request", response.State)
		return response, nil
	}
	if err != nil {
		response.State = "failed"
		response.Detail = fmt.Sprintf("%s request failed: %v", operation, err)
		return response, nil
	}
	response.State = "ready"
	response.Locations, response.Skipped, response.Truncated = boundedHeadlessLSPLocations(root, locations)
	response.Detail = operation + " locations received"
	return response, nil
}

func boundedHeadlessLSPContent(content string) (string, bool) {
	if len(content) <= maxHeadlessLSPHoverBytes {
		return content, false
	}
	content = content[:maxHeadlessLSPHoverBytes]
	for len(content) > 0 && !utf8.ValidString(content) {
		content = content[:len(content)-1]
	}
	return content, true
}

func boundedHeadlessLSPSymbols(symbols []lsp.DocumentSymbol) ([]headlessLSPSymbol, bool) {
	result := make([]headlessLSPSymbol, 0, min(len(symbols), maxHeadlessLSPQuerySymbols))
	truncated := false
	count := 0
	var convert func([]lsp.DocumentSymbol, int) []headlessLSPSymbol
	convert = func(input []lsp.DocumentSymbol, depth int) []headlessLSPSymbol {
		if depth > maxHeadlessLSPQueryDepth {
			truncated = true
			return nil
		}
		output := make([]headlessLSPSymbol, 0, len(input))
		for _, symbol := range input {
			if count >= maxHeadlessLSPQuerySymbols {
				truncated = true
				break
			}
			count++
			value := headlessLSPSymbol{
				Name: symbol.Name, Detail: symbol.Detail, Kind: symbol.Kind,
				Line: symbol.Range.Start.Line, Column: symbol.Range.Start.Character,
				EndLine: symbol.Range.End.Line, EndColumn: symbol.Range.End.Character,
			}
			value.Children = convert(symbol.Children, depth+1)
			output = append(output, value)
		}
		return output
	}
	result = convert(symbols, 0)
	return result, truncated
}

func boundedHeadlessLSPLocations(root string, locations []lsp.Location) ([]headlessLSPLocation, int, bool) {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, len(locations), false
	}
	result := make([]headlessLSPLocation, 0, min(len(locations), maxHeadlessLSPQueryLocations))
	skipped := 0
	truncated := false
	for _, location := range locations {
		if len(result) >= maxHeadlessLSPQueryLocations {
			truncated = true
			break
		}
		path := lsp.URIToPath(location.URI)
		pathReal, pathErr := filepath.EvalSymlinks(path)
		if pathErr != nil {
			skipped++
			continue
		}
		relative, relErr := filepath.Rel(rootReal, pathReal)
		if relErr != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			skipped++
			continue
		}
		result = append(result, headlessLSPLocation{
			Path: filepath.ToSlash(relative), Line: location.StartLine, Column: location.StartCol,
			EndLine: location.EndLine, EndColumn: location.EndCol,
		})
	}
	if len(locations) > len(result)+skipped {
		truncated = true
	}
	return result, skipped, truncated
}

func writeHeadlessSymbolsText(w io.Writer, symbols []headlessLSPSymbol, depth int) {
	for _, symbol := range symbols {
		fmt.Fprintf(w, "%s%s:%d:%d\n", strings.Repeat("  ", depth), symbol.Name, symbol.Line+1, symbol.Column+1)
		writeHeadlessSymbolsText(w, symbol.Children, depth+1)
	}
}
