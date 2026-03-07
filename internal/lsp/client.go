package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	log "github.com/charmbracelet/log"
)

// Client manages communication with a single LSP server process.
type Client struct {
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	mu           sync.RWMutex // RWMutex for better concurrent reads
	requestID    int
	pending      map[int]chan callResult
	rootURI      string
	openDocs     map[string]int // uri -> version
	running      bool
	initialized  bool
	msgChan      chan<- any
	cancelRead   context.CancelFunc
	capabilities ServerCapabilities // server capabilities from initialize
	syncKind     SyncKind           // document sync mode (negotiated)
}

// IsReady returns whether the client has completed initialization.
func (c *Client) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.initialized
}

// SupportsHover returns whether the server supports hover requests.
func (c *Client) SupportsHover() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.capabilities.HoverProvider
}

// SupportsCompletion returns whether the server supports completion requests.
func (c *Client) SupportsCompletion() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.capabilities.CompletionProvider != nil
}

// SupportsDefinition returns whether the server supports go-to-definition.
func (c *Client) SupportsDefinition() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.capabilities.DefinitionProvider
}

// SupportsReferences returns whether the server supports find-references.
func (c *Client) SupportsReferences() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.capabilities.ReferencesProvider
}

// SupportsRename returns whether the server supports rename.
func (c *Client) SupportsRename() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.capabilities.RenameProvider
}

// GetCompletionTriggerCharacters returns the trigger characters for completion.
func (c *Client) GetCompletionTriggerCharacters() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.capabilities.CompletionProvider != nil {
		return c.capabilities.CompletionProvider.TriggerCharacters
	}
	return nil
}

// GetSyncKind returns the negotiated document sync mode.
func (c *Client) GetSyncKind() SyncKind {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.syncKind
}

type callResult struct {
	Result json.RawMessage
	Error  *jsonrpcError
}

type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"` // Optional additional error information
}

// NewClient creates a new LSP client and starts the server process.
func NewClient(cfg ServerConfig, rootDir string, msgChan chan<- any) (*Client, error) {
	_, err := exec.LookPath(cfg.Command)
	if err != nil {
		return nil, fmt.Errorf("language server %q not found: %w", cfg.Command, err)
	}

	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Dir = rootDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cfg.Command, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		pending:    make(map[int]chan callResult),
		rootURI:    FileURI(rootDir),
		openDocs:   make(map[string]int),
		running:    true,
		msgChan:    msgChan,
		cancelRead: cancel,
	}

	go c.readLoop(ctx)

	return c, nil
}

// Initialize sends the initialize request to the server.
func (c *Client) Initialize() error {
	// Get actual process ID instead of nil
	processID := os.Getpid()

	params := map[string]any{
		"processId": processID,
		"rootUri":   c.rootURI,
		"clientInfo": map[string]string{
			"name":    "teak",
			"version": "1.0.0",
		},
		"workspaceFolders": []map[string]string{
			{
				"uri":  c.rootURI,
				"name": "workspace",
			},
		},
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"completion": map[string]any{
					"completionItem": map[string]any{
						"snippetSupport": false,
					},
				},
				"hover": map[string]any{
					"contentFormat": []string{"plaintext"},
				},
				"synchronization": map[string]any{
					"didSave":             true,
					"dynamicRegistration": false,
					"willSave":            false,
					"willSaveWaitUntil":   false,
				},
				"references": map[string]any{},
				"rename": map[string]any{
					"prepareSupport": false,
				},
			},
			"workspace": map[string]any{
				"applyEdit":        true,
				"workspaceFolders": true,
				"configuration":    true,
			},
			"window": map[string]any{
				"workDoneProgress": true,
				"showMessage": map[string]any{
					"messageActionItem": map[string]any{
						"additionalPropertiesSupport": false,
					},
				},
			},
			"general": map[string]any{
				"positionEncodings": []string{"utf-16", "utf-8", "utf-32"},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get initialize result and store capabilities
	result, err := c.call(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// Parse and store server capabilities
	var initResult InitializeResult
	if err := json.Unmarshal(result, &initResult); err != nil {
		return fmt.Errorf("parse initialize result: %w", err)
	}

	c.mu.Lock()
	c.capabilities = initResult.Capabilities
	// Negotiate sync kind from server capabilities
	c.syncKind = SyncFull // default to full sync
	if sync := initResult.Capabilities.TextDocumentSync; sync != nil {
		switch v := sync.(type) {
		case float64:
			c.syncKind = SyncKind(int(v))
		case int:
			c.syncKind = SyncKind(v)
		case map[string]any:
			// TextDocumentSyncOptions object form: { "change": 2, ... }
			if change, ok := v["change"]; ok {
				if f, ok := change.(float64); ok {
					c.syncKind = SyncKind(int(f))
				}
			}
		}
	}
	c.mu.Unlock()

	// Send initialized notification
	if err := c.notify("initialized", map[string]any{}); err != nil {
		return err
	}
	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return nil
}

// Shutdown gracefully shuts down the server.
func (c *Client) Shutdown() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c.call(ctx, "shutdown", nil)
	c.notify("exit", nil)
	c.cancelRead()
	c.stdout.Close() // unblocks readLoop's Read() call
	c.stdin.Close()
	c.cmd.Wait()
}

// DidOpen notifies the server that a document was opened.
func (c *Client) DidOpen(uri, languageID string, version int, content string) {
	c.mu.Lock()
	c.openDocs[uri] = version
	c.mu.Unlock()

	c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": languageID,
			"version":    version,
			"text":       content,
		},
	})
}

// DidChange notifies the server of a document change (full sync).
func (c *Client) DidChange(uri string, version int, content string) {
	c.mu.Lock()
	c.openDocs[uri] = version
	c.mu.Unlock()

	c.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{
			"uri":     uri,
			"version": version,
		},
		"contentChanges": []map[string]any{
			{"text": content},
		},
	})
}

// DidChangeIncremental notifies the server of an incremental document change.
// The range (startLine:startCol to endLine:endCol) describes the region in the
// old document that was replaced by text. All positions are 0-based.
func (c *Client) DidChangeIncremental(uri string, version int, startLine, startCol, endLine, endCol int, text string) {
	c.mu.Lock()
	c.openDocs[uri] = version
	c.mu.Unlock()

	c.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{
			"uri":     uri,
			"version": version,
		},
		"contentChanges": []map[string]any{
			{
				"range": map[string]any{
					"start": map[string]any{"line": startLine, "character": startCol},
					"end":   map[string]any{"line": endLine, "character": endCol},
				},
				"text": text,
			},
		},
	})
}

// DidSave notifies the server that a document was saved.
func (c *Client) DidSave(uri string) {
	c.notify("textDocument/didSave", map[string]any{
		"textDocument": map[string]any{
			"uri": uri,
		},
	})
}

// DidClose notifies the server that a document was closed.
func (c *Client) DidClose(uri string) {
	c.mu.Lock()
	delete(c.openDocs, uri)
	c.mu.Unlock()

	c.notify("textDocument/didClose", map[string]any{
		"textDocument": map[string]any{
			"uri": uri,
		},
	})
}

// Completion requests completions at the given position.
func (c *Client) Completion(uri string, line, character int) ([]CompletionItem, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := c.call(ctx, "textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line, "character": character},
	})
	if err != nil {
		return nil, err
	}

	// Parse result — could be CompletionList or []CompletionItem
	var items []CompletionItem

	// Try CompletionList first
	var list struct {
		Items []struct {
			Label      string `json:"label"`
			Detail     string `json:"detail"`
			InsertText string `json:"insertText"`
			Kind       int    `json:"kind"`
		} `json:"items"`
	}
	if err := json.Unmarshal(result, &list); err == nil && len(list.Items) > 0 {
		for _, item := range list.Items {
			insertText := item.InsertText
			if insertText == "" {
				insertText = item.Label
			}
			items = append(items, CompletionItem{
				Label:      item.Label,
				Detail:     item.Detail,
				InsertText: insertText,
				Kind:       item.Kind,
			})
		}
		return items, nil
	}

	// Try plain array
	var plainItems []struct {
		Label      string `json:"label"`
		Detail     string `json:"detail"`
		InsertText string `json:"insertText"`
		Kind       int    `json:"kind"`
	}
	if err := json.Unmarshal(result, &plainItems); err == nil {
		for _, item := range plainItems {
			insertText := item.InsertText
			if insertText == "" {
				insertText = item.Label
			}
			items = append(items, CompletionItem{
				Label:      item.Label,
				Detail:     item.Detail,
				InsertText: insertText,
				Kind:       item.Kind,
			})
		}
	}

	return items, nil
}

// Hover requests hover info at the given position.
func (c *Client) Hover(uri string, line, character int) (*HoverResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := c.call(ctx, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line, "character": character},
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var hover struct {
		Contents any `json:"contents"`
	}
	if err := json.Unmarshal(result, &hover); err != nil {
		return nil, err
	}

	content := extractHoverContent(hover.Contents)
	if content == "" {
		return nil, nil
	}

	return &HoverResult{Content: content}, nil
}

func extractHoverContent(contents any) string {
	switch v := contents.(type) {
	case string:
		return v
	case map[string]any:
		if val, ok := v["value"]; ok {
			return fmt.Sprintf("%v", val)
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				return s
			}
			if m, ok := item.(map[string]any); ok {
				if val, mok := m["value"]; mok {
					return fmt.Sprintf("%v", val)
				}
			}
		}
	}
	return ""
}

// Definition requests go-to-definition at the given position.
func (c *Client) Definition(uri string, line, character int) ([]Location, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := c.call(ctx, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line, "character": character},
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var locations []Location

	// Try array of locations
	var locs []struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
	}
	if err := json.Unmarshal(result, &locs); err == nil {
		for _, loc := range locs {
			locations = append(locations, Location{
				URI:       loc.URI,
				StartLine: loc.Range.Start.Line,
				StartCol:  loc.Range.Start.Character,
				EndLine:   loc.Range.End.Line,
				EndCol:    loc.Range.End.Character,
			})
		}
		return locations, nil
	}

	// Try single location
	var single struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
	}
	if err := json.Unmarshal(result, &single); err == nil && single.URI != "" {
		locations = append(locations, Location{
			URI:       single.URI,
			StartLine: single.Range.Start.Line,
			StartCol:  single.Range.Start.Character,
			EndLine:   single.Range.End.Line,
			EndCol:    single.Range.End.Character,
		})
	}

	return locations, nil
}

// References requests find-references at the given position.
func (c *Client) References(uri string, line, character int) ([]Location, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := c.call(ctx, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line, "character": character},
		"context":      map[string]any{"includeDeclaration": true},
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var locs []struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
	}
	if err := json.Unmarshal(result, &locs); err != nil {
		return nil, err
	}

	var locations []Location
	for _, loc := range locs {
		locations = append(locations, Location{
			URI:       loc.URI,
			StartLine: loc.Range.Start.Line,
			StartCol:  loc.Range.Start.Character,
			EndLine:   loc.Range.End.Line,
			EndCol:    loc.Range.End.Character,
		})
	}
	return locations, nil
}

// Rename requests a rename at the given position.
func (c *Client) Rename(uri string, line, character int, newName string) (map[string][]TextEdit, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := c.call(ctx, "textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line, "character": character},
		"newName":      newName,
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var wsEdit struct {
		Changes map[string][]struct {
			Range struct {
				Start struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"start"`
				End struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"end"`
			} `json:"range"`
			NewText string `json:"newText"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(result, &wsEdit); err != nil {
		return nil, err
	}

	edits := make(map[string][]TextEdit)
	for uri, changes := range wsEdit.Changes {
		for _, ch := range changes {
			edits[uri] = append(edits[uri], TextEdit{
				StartLine: ch.Range.Start.Line,
				StartCol:  ch.Range.Start.Character,
				EndLine:   ch.Range.End.Line,
				EndCol:    ch.Range.End.Character,
				NewText:   ch.NewText,
			})
		}
	}
	return edits, nil
}

// SignatureHelp requests signature help at the given position.
func (c *Client) SignatureHelp(uri string, line, character int) (*SignatureHelp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := c.call(ctx, "textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line, "character": character},
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var help SignatureHelp
	if err := json.Unmarshal(result, &help); err != nil {
		return nil, err
	}

	return &help, nil
}

// Formatting requests formatting for a document.
func (c *Client) Formatting(uri string) ([]TextEdit, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := c.call(ctx, "textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options": map[string]any{
			"tabSize":      4,
			"insertSpaces": true,
		},
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var edits []struct {
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
		NewText string `json:"newText"`
	}
	if err := json.Unmarshal(result, &edits); err != nil {
		return nil, err
	}

	var textEdits []TextEdit
	for _, ed := range edits {
		textEdits = append(textEdits, TextEdit{
			StartLine: ed.Range.Start.Line,
			StartCol:  ed.Range.Start.Character,
			EndLine:   ed.Range.End.Line,
			EndCol:    ed.Range.End.Character,
			NewText:   ed.NewText,
		})
	}
	return textEdits, nil
}

// FoldingRange requests folding ranges for a document.
func (c *Client) FoldingRange(uri string) ([]FoldingRange, error) {
	c.mu.Lock()
	supported := c.capabilities.FoldingRangeProvider
	c.mu.Unlock()
	if !supported {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := c.call(ctx, "textDocument/foldingRange", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if err != nil {
		return nil, err
	}
	if string(result) == "null" {
		return nil, nil
	}

	var ranges []FoldingRange
	if err := json.Unmarshal(result, &ranges); err != nil {
		return nil, err
	}
	return ranges, nil
}

// CodeAction requests code actions for a range with diagnostics.
func (c *Client) CodeAction(uri string, startLine, startCol, endLine, endCol int, diagnostics []Diagnostic) ([]CodeAction, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Convert diagnostics to LSP format
	var lspDiags []map[string]any
	for _, d := range diagnostics {
		lspDiags = append(lspDiags, map[string]any{
			"range": map[string]any{
				"start": map[string]any{"line": d.Range.Start.Line, "character": d.Range.Start.Character},
				"end":   map[string]any{"line": d.Range.End.Line, "character": d.Range.End.Character},
			},
			"severity": d.Severity,
			"message":  d.Message,
			"source":   d.Source,
		})
	}

	result, err := c.call(ctx, "textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": startLine, "character": startCol},
			"end":   map[string]any{"line": endLine, "character": endCol},
		},
		"context": map[string]any{
			"diagnostics": lspDiags,
		},
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var actions []CodeAction
	if err := json.Unmarshal(result, &actions); err != nil {
		return nil, err
	}

	return actions, nil
}

// DocumentSymbol requests document symbols.
func (c *Client) DocumentSymbol(uri string) ([]DocumentSymbol, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := c.call(ctx, "textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if err != nil {
		return nil, err
	}

	if string(result) == "null" {
		return nil, nil
	}

	var symbols []DocumentSymbol
	if err := json.Unmarshal(result, &symbols); err != nil {
		return nil, err
	}

	return symbols, nil
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil, errors.New("client not running")
	}
	c.requestID++
	id := c.requestID
	ch := make(chan callResult, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := c.send(req); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case res := <-ch:
		if res.Error != nil {
			return nil, fmt.Errorf("LSP error %d: %s", res.Error.Code, res.Error.Message)
		}
		return res.Result, nil
	case <-ctx.Done():
		// Cancel the pending request on the server side
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		// Send cancellation notification to LSP server
		c.notify("$/cancelRequest", map[string]any{"id": id})
		return nil, ctx.Err()
	}
}

func (c *Client) notify(method string, params any) error {
	notif := jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.send(notif)
}

func (c *Client) send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	_, err = c.stdin.Write(data)
	return err
}

func (c *Client) readLoop(ctx context.Context) {
	buf := make([]byte, 4096)
	var accumulated []byte

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := c.stdout.Read(buf)
		if err != nil {
			return
		}
		accumulated = append(accumulated, buf[:n]...)

		for {
			msg, rest, ok := parseMessage(accumulated)
			if !ok {
				break
			}
			accumulated = rest
			c.handleMessage(msg)
		}
	}
}

const (
	maxMessageSize = 10 * 1024 * 1024 // 10MB maximum message size
)

func parseMessage(data []byte) (json.RawMessage, []byte, bool) {
	// Look for Content-Length header using bytes.Index (efficient)
	header := []byte("Content-Length:")
	idx := bytes.Index(data, header)
	if idx < 0 {
		return nil, data, false
	}

	// Parse content length
	numStart := idx + len(header)
	// Skip any whitespace after the colon
	for numStart < len(data) && (data[numStart] == ' ' || data[numStart] == '\t') {
		numStart++
	}

	numEnd := numStart
	for numEnd < len(data) && data[numEnd] >= '0' && data[numEnd] <= '9' {
		numEnd++
	}
	if numEnd == numStart {
		return nil, data, false
	}

	contentLength, err := strconv.Atoi(string(data[numStart:numEnd]))
	if err != nil {
		return nil, data, false
	}

	// Reject excessively large messages to prevent memory exhaustion
	if contentLength > maxMessageSize {
		log.Warn("lsp: rejecting message (too large)", "size", contentLength, "max", maxMessageSize)
		return nil, data, false
	}

	// Find end of headers (\r\n\r\n)
	headerEnd := bytes.Index(data[numEnd:], []byte("\r\n\r\n"))
	if headerEnd < 0 {
		return nil, data, false
	}
	headerEnd += numEnd + 4 // offset + length of header terminator

	// Check if we have enough data
	if headerEnd+contentLength > len(data) {
		return nil, data, false
	}

	content := data[headerEnd : headerEnd+contentLength]
	rest := data[headerEnd+contentLength:]
	return json.RawMessage(content), rest, true
}

func (c *Client) handleMessage(data json.RawMessage) {
	// Check if it's a response (has "id" and "result" or "error")
	var peek struct {
		ID     *int            `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  *jsonrpcError   `json:"error"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return
	}

	// Response to our request
	if peek.ID != nil && peek.Method == "" {
		c.mu.Lock()
		ch, ok := c.pending[*peek.ID]
		if ok {
			delete(c.pending, *peek.ID)
		}
		c.mu.Unlock()

		if ok {
			ch <- callResult{Result: peek.Result, Error: peek.Error}
		}
		return
	}

	// Server notification
	switch peek.Method {
	case "textDocument/publishDiagnostics":
		c.handleDiagnostics(peek.Params)
	case "window/showMessage":
		c.handleShowMessage(peek.Params)
	case "window/logMessage":
		c.handleLogMessage(peek.Params)
	case "$/progress":
		c.handleProgress(peek.Params)
	case "workspace/configuration":
		c.handleWorkspaceConfiguration(peek.ID, peek.Params)
	case "workspace/workspaceFolders":
		c.handleWorkspaceFolders(peek.ID)
	}
}

func (c *Client) handleShowMessage(params json.RawMessage) {
	var p struct {
		Type    int    `json:"type"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	// Send to message channel for display in status bar
	if c.msgChan != nil {
		c.msgChan <- LspShowMessageMsg{
			Type:    p.Type,
			Message: p.Message,
		}
	}
}

func (c *Client) handleLogMessage(params json.RawMessage) {
	var p struct {
		Type    int    `json:"type"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	log.Info("lsp server message", "type", p.Type, "message", p.Message)
}

func (c *Client) handleProgress(params json.RawMessage) {
	var p struct {
		Token any `json:"token"`
		Value any `json:"value"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	// Progress reporting - can be extended to show in UI
	if c.msgChan != nil {
		c.msgChan <- LspProgressMsg{Token: p.Token, Value: p.Value}
	}
}

func (c *Client) handleWorkspaceConfiguration(id *int, params json.RawMessage) {
	// Respond with empty configuration for now
	// Can be extended to read from settings
	if id != nil && c.msgChan != nil {
		c.sendResponse(*id, []any{})
	}
}

func (c *Client) handleWorkspaceFolders(id *int) {
	// Respond with current workspace folder
	if id != nil {
		folders := []map[string]string{
			{"uri": c.rootURI, "name": "workspace"},
		}
		c.sendResponse(*id, folders)
	}
}

func (c *Client) sendResponse(id int, result any) {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		log.Error("lsp: failed to marshal response", "err", err)
		return
	}
	resp := jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  resultJSON,
	}
	c.send(resp)
}

func (c *Client) handleDiagnostics(params json.RawMessage) {
	var p struct {
		URI         string `json:"uri"`
		Diagnostics []struct {
			Range struct {
				Start struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"start"`
				End struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"end"`
			} `json:"range"`
			Severity int    `json:"severity"`
			Message  string `json:"message"`
			Source   string `json:"source"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		log.Error("lsp: failed to parse diagnostics", "err", err)
		return
	}

	diags := make([]Diagnostic, len(p.Diagnostics))
	for i, d := range p.Diagnostics {
		diags[i] = Diagnostic{
			Range: DiagRange{
				Start: DiagPosition{Line: d.Range.Start.Line, Character: d.Range.Start.Character},
				End:   DiagPosition{Line: d.Range.End.Line, Character: d.Range.End.Character},
			},
			Severity: DiagSeverity(d.Severity),
			Message:  d.Message,
			Source:   d.Source,
		}
	}

	if c.msgChan != nil {
		c.msgChan <- DiagnosticsMsg{
			URI:         p.URI,
			Diagnostics: diags,
		}
	}
}
