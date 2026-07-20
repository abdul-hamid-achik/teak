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
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	log "github.com/charmbracelet/log"
	"teak/internal/lsp/capabilities"
)

// Client manages communication with a single LSP server process.
type Client struct {
	cmd                   *exec.Cmd
	stdin                 io.WriteCloser
	stdout                io.ReadCloser
	mu                    sync.RWMutex // RWMutex for better concurrent reads
	requestID             int
	pending               map[int]chan callResult
	rootURI               string
	openDocs              map[string]int // uri -> version
	documents             map[string]documentSnapshot
	documentSnapshotBytes int
	documentGenerations   map[string]uint64
	suppressedDocs        map[string]struct{} // documents too large for the LSP transport, awaiting a later full reopen
	running               bool
	shuttingDown          bool
	initialized           bool
	msgChan               chan<- any
	cancelRead            context.CancelFunc
	capabilities          ServerCapabilities    // server capabilities from initialize
	syncKind              SyncKind              // document sync mode (negotiated)
	capsChecker           *capabilities.Checker // delegated capability checker
	positionEncoding      positionEncoding      // negotiated LSP character-unit encoding

	// Notifications are delivered independently from the protocol reader. A
	// stalled UI must never prevent JSON-RPC responses from being processed.
	notificationMu      sync.Mutex
	notificationQueue   []*queuedNotification
	pendingNotification map[string]*queuedNotification
	notificationWake    chan struct{}
	writeMu             sync.Mutex
	outboundMu          sync.Mutex
	outboundQueue       []*outboundMessage
	pendingChanges      map[string]*outboundMessage
	outboundBytes       int
	outboundWake        chan struct{}
	outboundStarted     bool
	outboundUnhealthy   bool
	shutdownOnce        sync.Once
	processDone         chan struct{}
	readDone            chan struct{}
	applyEditMu         sync.Mutex
	pendingApplyEdits   int
}

const (
	lspShutdownRequestTimeout = 750 * time.Millisecond
	lspShutdownReapTimeout    = 2 * time.Second
	applyEditResponseTimeout  = 5 * time.Second
	maxPendingApplyEdits      = 16
	maxPendingOutbound        = 128
	maxPendingOutboundBytes   = 72 << 20
	maxDocumentEnvelopeBytes  = 8 << 10
)

// Teak accepts editor files up to 64 MiB. Give an ordinary full-sync document
// one MiB of JSON-RPC envelope room. The waiting queue may hold
// 72 MiB; together with the at-most-one 65 MiB message owned by the writer,
// outbound transport data is bounded at 137 MiB. Content whose JSON
// representation exceeds this explicit LSP transport policy is rejected
// rather than allowing an unbounded stdin backlog behind a stuck server.
//
// Tests temporarily lower this package-private value to exercise escaping
// boundaries without allocating a 65 MiB payload; production never changes it.
var maxOutboundMessageBytes = 65 << 20

// Kept package-private so focused tests can exercise the deterministic
// degradation path without allocating production-sized documents.
var maxDocumentSnapshotBudget = 128 << 20

// documentTextFitsOutboundMessage estimates JSON string escaping before a
// document notification is marshaled. It prevents a valid but control-heavy
// editor buffer from expanding several times over in memory merely to learn it
// cannot enter the bounded transport. The envelope reserve covers JSON-RPC
// fields, URI escaping, and version metadata; small test limits retain enough
// room for a minimal document notification.
func documentTextFitsOutboundMessage(text string) bool {
	envelope := maxDocumentEnvelopeBytes
	if maxOutboundMessageBytes <= envelope {
		envelope = maxOutboundMessageBytes / 4
	}
	limit := maxOutboundMessageBytes - envelope
	if limit <= 2 {
		return false
	}
	size := 2 // surrounding JSON string quotes
	for index := 0; index < len(text); {
		if size > limit {
			return false
		}
		b := text[index]
		switch b {
		case '"', '\\':
			size += 2
			index++
		case '\b', '\f', '\n', '\r', '\t':
			size += 2
			index++
		case '<', '>', '&':
			size += 6
			index++
		default:
			if b < 0x20 {
				size += 6
				index++
				continue
			}
			// encoding/json escapes U+2028 and U+2029 for JavaScript safety.
			if b == 0xe2 && index+2 < len(text) && text[index+1] == 0x80 && (text[index+2] == 0xa8 || text[index+2] == 0xa9) {
				size += 6
				index += 3
				continue
			}
			size++
			index++
		}
	}
	return size <= limit
}

type queuedNotification struct {
	key        string
	msg        any
	generation uint64
}

type outboundKind uint8

const (
	outboundBarrier outboundKind = iota + 1
	outboundDidChange
)

// outboundMessage holds an already-marshaled JSON-RPC frame body. Marshaling
// before enqueueing makes the queue's byte budget exact and ensures the sole
// writer goroutine is the only code that can block on server stdin.
type outboundMessage struct {
	data               []byte
	size               int
	kind               outboundKind
	uri                string
	version            int
	documentGeneration uint64 // non-zero only for Manager-owned document traffic
	done               chan error
}

// IsReady returns whether the client has completed initialization and remains
// able to service protocol traffic. An initialized process that is shutting
// down must never be reused by Manager.EnsureClient.
func (c *Client) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.initialized && c.running
}

// SupportsHover returns whether the server supports hover requests.
func (c *Client) SupportsHover() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.capsChecker != nil {
		return c.capsChecker.SupportsHover()
	}
	return capabilityEnabled(c.capabilities.HoverProvider)
}

// SupportsCompletion returns whether the server supports completion requests.
func (c *Client) SupportsCompletion() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.capsChecker != nil {
		return c.capsChecker.SupportsCompletion()
	}
	return c.capabilities.CompletionProvider != nil
}

// SupportsDefinition returns whether the server supports go-to-definition.
func (c *Client) SupportsDefinition() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.capsChecker != nil {
		return c.capsChecker.SupportsDefinition()
	}
	return capabilityEnabled(c.capabilities.DefinitionProvider)
}

// SupportsReferences returns whether the server supports find-references.
func (c *Client) SupportsReferences() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.capsChecker != nil {
		return c.capsChecker.SupportsReferences()
	}
	return capabilityEnabled(c.capabilities.ReferencesProvider)
}

// SupportsRename returns whether the server supports rename.
func (c *Client) SupportsRename() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.capsChecker != nil {
		return c.capsChecker.SupportsRename()
	}
	return capabilityEnabled(c.capabilities.RenameProvider)
}

// SupportsFormatting returns whether the server supports document formatting.
func (c *Client) SupportsFormatting() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.capsChecker != nil {
		return c.capsChecker.SupportsFormatting()
	}
	return capabilityEnabled(c.capabilities.FormattingProvider)
}

// GetCompletionTriggerCharacters returns the trigger characters for completion.
func (c *Client) GetCompletionTriggerCharacters() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.capsChecker != nil {
		return c.capsChecker.GetCompletionTriggerCharacters()
	}
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

// newCapabilitiesChecker creates a capabilities.Checker from the server capabilities
func (c *Client) newCapabilitiesChecker(caps ServerCapabilities, syncKind SyncKind) *capabilities.Checker {
	// Convert lsp.ServerCapabilities to capabilities.ServerCapabilities
	capsConv := capabilities.ServerCapabilities{
		TextDocumentSync:        caps.TextDocumentSync,
		CompletionProvider:      nil,
		PositionEncoding:        caps.PositionEncoding,
		HoverProvider:           caps.HoverProvider,
		DefinitionProvider:      caps.DefinitionProvider,
		ReferencesProvider:      caps.ReferencesProvider,
		RenameProvider:          caps.RenameProvider,
		DocumentSymbolProvider:  caps.DocumentSymbolProvider,
		CodeActionProvider:      caps.CodeActionProvider,
		FormattingProvider:      caps.FormattingProvider,
		RangeFormattingProvider: caps.RangeFormattingProvider,
		FoldingRangeProvider:    caps.FoldingRangeProvider,
		SignatureHelpProvider:   nil,
	}
	if caps.CompletionProvider != nil {
		capsConv.CompletionProvider = &capabilities.CompletionOptions{
			ResolveProvider:   caps.CompletionProvider.ResolveProvider,
			TriggerCharacters: caps.CompletionProvider.TriggerCharacters,
		}
	}
	if caps.SignatureHelpProvider != nil {
		capsConv.SignatureHelpProvider = &capabilities.SignatureHelpOptions{
			TriggerCharacters: caps.SignatureHelpProvider.TriggerCharacters,
		}
	}
	return capabilities.NewChecker(capsConv, capabilities.SyncKind(syncKind))
}

// DocumentVersion returns the tracked LSP version for an open document.
func (c *Client) DocumentVersion(uri string) (int, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	version, ok := c.openDocs[uri]
	return version, ok
}

// DocumentSyncSuppressed reports whether an oversized document was removed
// from this client's LSP session. A later, admissibly sized DidOpen clears the
// state and resumes synchronization for that URI.
func (c *Client) DocumentSyncSuppressed(uri string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, suppressed := c.suppressedDocs[uri]
	return suppressed
}

// documentSnapshot returns the immutable document text most recently sent to
// or read for this client. It intentionally returns a value so callers never
// hold the client lock while converting a potentially long line.
func (c *Client) documentSnapshot(uri string) (documentSnapshot, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	snapshot, ok := c.documents[uri]
	return snapshot, ok
}

func (c *Client) setDocumentSnapshot(uri string, version int, content string) {
	snapshot, err := newDocumentSnapshot(version, content)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.documents == nil {
		c.documents = make(map[string]documentSnapshot)
	}
	c.removeDocumentSnapshotLocked(uri)
	if c.documentSnapshotBytes+snapshot.retainedBytes() <= maxDocumentSnapshotBudget {
		c.documents[uri] = snapshot
		c.documentSnapshotBytes += snapshot.retainedBytes()
	}
}

type stagedDocument struct {
	wasOpen        bool
	previousVer    int
	hadSnapshot    bool
	previousSnap   documentSnapshot
	wasSuppressed  bool
	stagedSnapshot bool
}

func (c *Client) bindDocumentGeneration(uri string, generation uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.shuttingDown {
		return false
	}
	if c.documentGenerations == nil {
		c.documentGenerations = make(map[string]uint64)
	}
	if current := c.documentGenerations[uri]; current > generation {
		return false
	}
	c.documentGenerations[uri] = generation
	return true
}

func (c *Client) acceptsDocumentLifecycle(uri string, generation uint64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.shuttingDown && c.documentGenerations[uri] == generation
}

// closeDocumentGeneration invalidates a lifecycle before returning. Its close
// barrier is queued immediately (never written synchronously), so a future
// reopen cannot overtake it in the outbound FIFO.
func (c *Client) closeDocumentGeneration(uri string, nextGeneration uint64) {
	c.mu.Lock()
	if c.shuttingDown {
		c.mu.Unlock()
		return
	}
	if c.documentGenerations == nil {
		c.documentGenerations = make(map[string]uint64)
	}
	if c.documentGenerations[uri] > nextGeneration {
		c.mu.Unlock()
		return
	}
	c.documentGenerations[uri] = nextGeneration
	_, wasOpen := c.openDocs[uri]
	c.removeDocumentSnapshotLocked(uri)
	delete(c.openDocs, uri)
	delete(c.suppressedDocs, uri)
	c.mu.Unlock()
	if !wasOpen {
		return
	}
	if err := c.notify("textDocument/didClose", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}); err != nil {
		log.Error("lsp: didClose notification failed", "uri", uri, "err", err)
	}
}

func (c *Client) removeDocumentSnapshotLocked(uri string) {
	if snapshot, ok := c.documents[uri]; ok {
		c.documentSnapshotBytes -= snapshot.retainedBytes()
		if c.documentSnapshotBytes < 0 { // tolerate legacy test clients.
			c.documentSnapshotBytes = 0
		}
		delete(c.documents, uri)
	}
}

// stageDocument publishes a newer local snapshot before its notification is
// queued, which makes independently scheduled tea.Cmd document versions
// monotonic. rollbackStagedDocument restores the precise prior state if the
// bounded transport rejects the notification.
func (c *Client) stageDocument(uri string, version int, snapshot documentSnapshot, generation uint64, requireOpen bool) (stagedDocument, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.shuttingDown || c.documentGenerations[uri] != generation {
		return stagedDocument{}, false
	}
	if requireOpen {
		if _, open := c.openDocs[uri]; !open {
			return stagedDocument{}, false
		}
	}
	if current, open := c.openDocs[uri]; open && current >= version {
		return stagedDocument{}, false
	}
	state := stagedDocument{previousVer: c.openDocs[uri]}
	_, state.wasOpen = c.openDocs[uri]
	state.previousSnap, state.hadSnapshot = c.documents[uri]
	_, state.wasSuppressed = c.suppressedDocs[uri]
	if c.openDocs == nil {
		c.openDocs = make(map[string]int)
	}
	if c.documents == nil {
		c.documents = make(map[string]documentSnapshot)
	}
	c.openDocs[uri] = version
	c.removeDocumentSnapshotLocked(uri)
	if c.documentSnapshotBytes+snapshot.retainedBytes() <= maxDocumentSnapshotBudget {
		c.documents[uri] = snapshot
		c.documentSnapshotBytes += snapshot.retainedBytes()
		state.stagedSnapshot = true
	}
	return state, true
}

func (c *Client) commitStagedDocument(uri string, version int, generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if current, open := c.openDocs[uri]; open && current == version && c.documentGenerations[uri] == generation {
		delete(c.suppressedDocs, uri)
	}
}

func (c *Client) rollbackStagedDocument(uri string, version int, generation uint64, state stagedDocument) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if current, open := c.openDocs[uri]; !open || current != version || c.documentGenerations[uri] != generation {
		return
	}
	if state.wasOpen {
		c.openDocs[uri] = state.previousVer
	} else {
		delete(c.openDocs, uri)
	}
	c.removeDocumentSnapshotLocked(uri)
	if state.hadSnapshot {
		c.documents[uri] = state.previousSnap
		c.documentSnapshotBytes += state.previousSnap.retainedBytes()
	}
	if state.wasSuppressed {
		if c.suppressedDocs == nil {
			c.suppressedDocs = make(map[string]struct{})
		}
		c.suppressedDocs[uri] = struct{}{}
	} else {
		delete(c.suppressedDocs, uri)
	}
}

// retireOversizedDocument preserves the rest of a shared LSP client when one
// valid editor document cannot fit the bounded JSON-RPC transport. If the
// server may know the document, an ordered didClose barrier removes its stale
// snapshot before other traffic continues.
func (c *Client) retireOversizedDocument(uri string, generation uint64) {
	c.mu.Lock()
	if c.shuttingDown || c.documentGenerations[uri] != generation {
		c.mu.Unlock()
		return
	}
	_, wasOpen := c.openDocs[uri]
	delete(c.openDocs, uri)
	c.removeDocumentSnapshotLocked(uri)
	if c.suppressedDocs == nil {
		c.suppressedDocs = make(map[string]struct{})
	}
	c.suppressedDocs[uri] = struct{}{}
	c.mu.Unlock()

	if !wasOpen {
		return
	}
	if err := c.notify("textDocument/didClose", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}); err != nil {
		log.Error("lsp: failed to retire oversized document", "uri", uri, "err", err)
	}
}

func (c *Client) negotiatedPositionEncoding() positionEncoding {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.positionEncoding == "" {
		// An uninitialized client is never allowed to issue protocol requests.
		// Keep direct, minimal test clients byte-compatible with the historic
		// client while initialize explicitly records the protocol's UTF-16
		// legacy default.
		return positionEncodingUTF8
	}
	return c.positionEncoding
}

type callResult struct {
	Result json.RawMessage
	Error  *jsonrpcError
}

var errClientNotRunning = errors.New("client not running")

func capabilityEnabled(v any) bool {
	switch vv := v.(type) {
	case nil:
		return false
	case bool:
		return vv
	case map[string]any:
		return true
	default:
		return true
	}
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

func parseWorkspaceEditResult(result []byte) (WorkspaceEdit, error) {
	var edit WorkspaceEdit
	if err := json.Unmarshal(result, &edit); err != nil {
		return WorkspaceEdit{}, err
	}
	return edit, nil
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

func formattingRequestParams(uri string, options FormattingOptions) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options": map[string]any{
			"tabSize":      options.TabSize,
			"insertSpaces": options.InsertSpaces,
		},
	}
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"` // Optional additional error information
}

const jsonrpcMethodNotFound = -32601

// NewClient creates a new LSP client and starts the server process.
func NewClient(cfg ServerConfig, rootDir string, msgChan chan<- any) (*Client, error) {
	_, err := exec.LookPath(cfg.Command)
	if err != nil {
		return nil, fmt.Errorf("language server %q not found: %w", cfg.Command, err)
	}

	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Dir = rootDir
	if len(cfg.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

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
		cmd:            cmd,
		stdin:          stdin,
		stdout:         stdout,
		pending:        make(map[int]chan callResult),
		rootURI:        FileURI(rootDir),
		openDocs:       make(map[string]int),
		documents:      make(map[string]documentSnapshot),
		suppressedDocs: make(map[string]struct{}),
		running:        true,
		msgChan:        msgChan,
		cancelRead:     cancel,
		processDone:    make(chan struct{}),
		readDone:       make(chan struct{}),
	}

	c.startOutboundWorker()
	go c.reapProcess()
	go c.readLoop(ctx)

	return c, nil
}

// Initialize sends the initialize request to the server.
func (c *Client) Initialize() error {
	// Get actual process ID instead of nil
	processID := os.Getpid()
	params := initializeParams(processID, c.rootURI)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := c.call(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	var initResult InitializeResult
	if err := json.Unmarshal(result, &initResult); err != nil {
		return fmt.Errorf("parse initialize result: %w", err)
	}
	if err := c.applyInitializeResult(initResult); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	if err := c.sendBarrier(ctx, jsonrpcNotification{JSONRPC: "2.0", Method: "initialized", Params: map[string]any{}}); err != nil {
		return err
	}
	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return nil
}

func initializeParams(processID int, rootURI string) map[string]any {
	return map[string]any{
		"processId": processID,
		"rootUri":   rootURI,
		"clientInfo": map[string]string{
			"name":    "teak",
			"version": "1.0.0",
		},
		"workspaceFolders": []map[string]string{
			{
				"uri":  rootURI,
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
				// Server-initiated edits are routed to Bubble Tea, validated against
				// the pinned workspace root, and acknowledged only after Update has
				// made the decision.
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
				// The client converts all boundaries through immutable document
				// snapshots, so it can safely interoperate with legacy UTF-16
				// servers as well as UTF-8 and UTF-32 implementations.
				"positionEncodings": []string{
					string(positionEncodingUTF8),
					string(positionEncodingUTF16),
					string(positionEncodingUTF32),
				},
			},
		},
	}
}

// applyInitializeResult validates protocol choices before publishing server
// capabilities to the rest of the client.
func (c *Client) applyInitializeResult(initResult InitializeResult) error {
	encoding, err := validateServerPositionEncoding(initResult.Capabilities.PositionEncoding)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.capabilities = initResult.Capabilities
	c.positionEncoding = encoding
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
	// Create the capabilities checker
	c.capsChecker = c.newCapabilitiesChecker(initResult.Capabilities, c.syncKind)
	return nil
}

// Shutdown gracefully shuts down the server.
func (c *Client) Shutdown() {
	c.shutdownOnce.Do(func() {
		c.mu.Lock()
		c.running = false
		c.shuttingDown = true
		c.mu.Unlock()
		go c.shutdownProcess()
	})
}

// WaitForShutdown waits until the server process has been reaped or ctx is
// cancelled. Shutdown remains non-blocking so callers on a UI path can initiate
// teardown safely; application exit uses this method from a tea.Cmd before the
// Go process terminates.
func (c *Client) WaitForShutdown(ctx context.Context) bool {
	if c.processDone == nil {
		return true
	}
	select {
	case <-c.processDone:
		return true
	case <-ctx.Done():
		return false
	}
}

// DidOpen notifies the server that a document was opened.
func (c *Client) DidOpen(uri, languageID string, version int, content string) {
	c.didOpen(uri, languageID, version, content, 0)
}

func (c *Client) didOpen(uri, languageID string, version int, content string, generation uint64) {
	if !c.acceptsDocumentLifecycle(uri, generation) {
		return
	}
	snapshot, err := newDocumentSnapshot(version, content)
	if err != nil {
		log.Error("lsp: refusing to open invalid UTF-8 document", "uri", uri, "err", err)
		return
	}
	if !documentTextFitsOutboundMessage(content) {
		c.retireOversizedDocument(uri, generation)
		log.Error("lsp: retiring document that exceeds outbound transport limit", "uri", uri)
		return
	}
	state, staged := c.stageDocument(uri, version, snapshot, generation, false)
	if !staged {
		return
	}

	if err := c.notifyDocument(uri, generation, "textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": languageID,
			"version":    version,
			"text":       content,
		},
	}); err != nil {
		if errors.Is(err, errOutboundMessageTooLarge) {
			c.rollbackStagedDocument(uri, version, generation, state)
			c.retireOversizedDocument(uri, generation)
		}
		log.Error("lsp: didOpen notification failed", "uri", uri, "err", err)
		return
	}
	c.commitStagedDocument(uri, version, generation)
}

// DidChange notifies the server of a document change (full sync).
func (c *Client) DidChange(uri string, version int, content string) {
	c.didChange(uri, version, content, 0)
}

func (c *Client) didChange(uri string, version int, content string, generation uint64) {
	if !c.acceptsDocumentLifecycle(uri, generation) {
		return
	}
	snapshot, err := newDocumentSnapshot(version, content)
	if err != nil {
		log.Error("lsp: refusing to sync invalid UTF-8 document", "uri", uri, "err", err)
		return
	}
	state, staged := c.stageDocument(uri, version, snapshot, generation, true)
	if !staged {
		return
	}

	if err := c.enqueueDidChange(uri, version, map[string]any{
		"text": content,
	}, content, generation); err != nil {
		if errors.Is(err, errOutboundMessageTooLarge) {
			c.rollbackStagedDocument(uri, version, generation, state)
			c.retireOversizedDocument(uri, generation)
		}
		log.Error("lsp: didChange notification failed", "uri", uri, "err", err)
		return
	}
	c.commitStagedDocument(uri, version, generation)
}

// enqueueDidChange coalesces only unsent document changes. Incremental changes
// retain their precise range when they are the next write, but supply a full
// snapshot as their replacement so dropping an earlier queued edit never
// leaves the server applying a delta to the wrong document version.
func (c *Client) enqueueDidChange(uri string, version int, contentChange map[string]any, latestContent string, generation uint64) error {
	if !documentTextFitsOutboundMessage(latestContent) {
		return errOutboundMessageTooLarge
	}
	if text, ok := contentChange["text"].(string); ok && text != latestContent && !documentTextFitsOutboundMessage(text) {
		return errOutboundMessageTooLarge
	}
	notification := jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  "textDocument/didChange",
		Params: map[string]any{
			"textDocument": map[string]any{
				"uri":     uri,
				"version": version,
			},
			"contentChanges": []map[string]any{contentChange},
		},
	}
	// The full replacement is used only if this didChange supersedes another
	// unsent didChange for the URI.
	full := jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  "textDocument/didChange",
		Params: map[string]any{
			"textDocument": map[string]any{"uri": uri, "version": version},
			"contentChanges": []map[string]any{
				{"text": latestContent},
			},
		},
	}
	data, err := json.Marshal(notification)
	if err != nil {
		return err
	}
	fullData := data
	if text, fullChange := contentChange["text"].(string); !fullChange || len(contentChange) != 1 || text != latestContent {
		fullData, err = json.Marshal(full)
		if err != nil {
			return err
		}
	}
	_, err = c.enqueueOutbound(context.Background(), data, fullData, outboundDidChange, uri, version, false, generation)
	return err
}

// DidChangeIncremental notifies the server of an incremental document change.
// The range (startLine:startCol to endLine:endCol) describes the region in the
// old document that was replaced by text. All positions are 0-based.
func (c *Client) DidChangeIncremental(uri string, version int, startLine, startCol, endLine, endCol int, text string) {
	c.didChangeIncremental(uri, version, startLine, startCol, endLine, endCol, text, "", false, 0)
}

// DidChangeIncrementalWithContent provides the immutable full target content
// alongside an incremental change. It lets the client degrade safely to a
// full change if tea.Cmd scheduling delivers versions out of order.
func (c *Client) DidChangeIncrementalWithContent(uri string, version int, startLine, startCol, endLine, endCol int, text, content string) {
	c.didChangeIncremental(uri, version, startLine, startCol, endLine, endCol, text, content, true, 0)
}

func (c *Client) didChangeIncremental(uri string, version int, startLine, startCol, endLine, endCol int, text, targetContent string, hasTargetContent bool, generation uint64) {
	if !c.acceptsDocumentLifecycle(uri, generation) {
		return
	}
	snapshot, ok := c.documentSnapshot(uri)
	if !ok {
		log.Error("lsp: refusing incremental sync without a document snapshot", "uri", uri)
		return
	}
	if snapshot.version >= version {
		return
	}
	if snapshot.version+1 != version {
		if !hasTargetContent {
			log.Error("lsp: refusing out-of-order incremental sync without full content", "uri", uri, "version", version, "baseVersion", snapshot.version)
			return
		}
		c.didChange(uri, version, targetContent, generation)
		return
	}
	if !utf8.ValidString(text) {
		log.Error("lsp: refusing incremental sync with invalid UTF-8 replacement", "uri", uri)
		return
	}

	startInternal := Position{Line: startLine, Character: startCol}
	endInternal := Position{Line: endLine, Character: endCol}
	start, err := positionToProtocolSnapshot(snapshot, c.negotiatedPositionEncoding(), startInternal)
	if err != nil {
		log.Error("lsp: refusing invalid incremental change start", "uri", uri, "err", err)
		return
	}
	end, err := positionToProtocolSnapshot(snapshot, c.negotiatedPositionEncoding(), endInternal)
	if err != nil {
		log.Error("lsp: refusing invalid incremental change end", "uri", uri, "err", err)
		return
	}
	updated, err := replaceInternalRangeSnapshot(snapshot, startInternal, endInternal, text)
	if err != nil {
		log.Error("lsp: refusing invalid incremental change range", "uri", uri, "err", err)
		return
	}
	updatedSnapshot, err := newDocumentSnapshot(version, updated)
	if err != nil {
		log.Error("lsp: refusing incremental sync that creates invalid UTF-8", "uri", uri, "err", err)
		return
	}
	// Teak currently stores CRLF as a trailing CR in its byte-oriented line.
	// A cursor after that CR is distinct internally but has no ranged LSP
	// position (LSP excludes both CR and LF). Send a full content change to
	// keep the server snapshot exact instead of silently reordering text.
	fullChange := strings.Contains(snapshot.content, "\r\n") || strings.Contains(updated, "\r\n")

	state, staged := c.stageDocument(uri, version, updatedSnapshot, generation, true)
	if !staged {
		return
	}

	contentChange := map[string]any{"text": text}
	if fullChange {
		contentChange["text"] = updated
	} else {
		contentChange["range"] = map[string]any{
			"start": map[string]any{"line": start.Line, "character": start.Character},
			"end":   map[string]any{"line": end.Line, "character": end.Character},
		}
	}
	if !hasTargetContent {
		targetContent = updated
	}
	if err := c.enqueueDidChange(uri, version, contentChange, targetContent, generation); err != nil {
		if errors.Is(err, errOutboundMessageTooLarge) {
			c.rollbackStagedDocument(uri, version, generation, state)
			c.retireOversizedDocument(uri, generation)
		}
		log.Error("lsp: incremental didChange notification failed", "uri", uri, "err", err)
		return
	}
	c.commitStagedDocument(uri, version, generation)
}

// DidSave notifies the server that a document was saved.
func (c *Client) DidSave(uri string) {
	c.didSave(uri, 0)
}

func (c *Client) didSave(uri string, generation uint64) {
	if !c.acceptsDocumentLifecycle(uri, generation) {
		return
	}
	c.mu.RLock()
	_, open := c.openDocs[uri]
	c.mu.RUnlock()
	if !open {
		return
	}
	if err := c.notifyDocument(uri, generation, "textDocument/didSave", map[string]any{
		"textDocument": map[string]any{
			"uri": uri,
		},
	}); err != nil {
		log.Error("lsp: didSave notification failed", "uri", uri, "err", err)
	}
}

// DidClose notifies the server that a document was closed.
func (c *Client) DidClose(uri string) {
	c.mu.Lock()
	if c.shuttingDown {
		c.mu.Unlock()
		return
	}
	c.removeDocumentSnapshotLocked(uri)
	delete(c.openDocs, uri)
	delete(c.suppressedDocs, uri)
	c.mu.Unlock()

	if err := c.notify("textDocument/didClose", map[string]any{
		"textDocument": map[string]any{
			"uri": uri,
		},
	}); err != nil {
		log.Error("lsp: didClose notification failed", "uri", uri, "err", err)
	}
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.callInternal(ctx, method, params, true)
}

func (c *Client) callInternal(ctx context.Context, method string, params any, requireRunning bool) (json.RawMessage, error) {
	c.mu.Lock()
	if requireRunning && !c.running {
		c.mu.Unlock()
		return nil, errClientNotRunning
	}
	c.requestID++
	id := c.requestID
	ch := make(chan callResult, 1)
	if c.pending == nil {
		c.pending = make(map[int]chan callResult)
	}
	c.pending[id] = ch
	c.mu.Unlock()

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	possiblyWritten, err := c.sendRequest(ctx, req)
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		if possiblyWritten && ctx.Err() != nil {
			c.cancelRequest(id)
		}
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
		c.cancelRequest(id)
		return nil, ctx.Err()
	case <-c.processDone:
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, errClientNotRunning
	}
}

func (c *Client) cancelRequest(id int) {
	if err := c.notify("$/cancelRequest", map[string]any{"id": id}); err != nil {
		log.Error("lsp: cancel request notification failed", "id", id, "err", err)
	}
}

func isExpectedShutdownError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errClientNotRunning) || errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "closed pipe") ||
		strings.Contains(msg, "file already closed") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "use of closed network connection")
}

func (c *Client) notify(method string, params any) error {
	notif := jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.enqueueNotification(notif, outboundBarrier, "", nil)
}

// notifyDocument tags Manager-owned document lifecycle traffic. The sole
// writer rechecks the tag immediately before stdin I/O, so a close that wins
// while JSON is being marshaled cannot be followed by a stale reopen/change.
func (c *Client) notifyDocument(uri string, generation uint64, method string, params any) error {
	notif := jsonrpcNotification{JSONRPC: "2.0", Method: method, Params: params}
	data, err := json.Marshal(notif)
	if err != nil {
		return err
	}
	_, err = c.enqueueOutbound(context.Background(), data, nil, outboundBarrier, uri, 0, false, generation)
	return err
}

func (c *Client) sendBarrier(ctx context.Context, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = c.enqueueOutbound(ctx, data, nil, outboundBarrier, "", 0, true, 0)
	return err
}

// enqueueNotification never waits for stdin. Notifications are either placed
// in the bounded outbound queue or the client transitions to an unhealthy,
// deterministic shutdown state; none is silently dropped.
func (c *Client) enqueueNotification(msg any, kind outboundKind, uri string, replacement any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	var replacementData []byte
	if replacement != nil {
		replacementData, err = json.Marshal(replacement)
		if err != nil {
			return err
		}
	}
	_, err = c.enqueueOutbound(context.Background(), data, replacementData, kind, uri, 0, false, 0)
	return err
}

// sendRequest waits only until the dedicated writer has accepted the request
// onto stdin. It honours ctx while the request is queued, so a stuck server
// cannot strand request goroutines indefinitely.
func (c *Client) sendRequest(ctx context.Context, msg any) (bool, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return false, err
	}
	return c.enqueueOutbound(ctx, data, nil, outboundBarrier, "", 0, true, 0)
}

func (c *Client) enqueueOutbound(ctx context.Context, data, replacementData []byte, kind outboundKind, uri string, version int, wait bool, documentGeneration uint64) (bool, error) {
	if replacementData == nil {
		replacementData = data
	}
	if len(data) > maxOutboundMessageBytes || len(replacementData) > maxOutboundMessageBytes {
		return false, fmt.Errorf("%w: primary=%d replacement=%d", errOutboundMessageTooLarge, len(data), len(replacementData))
	}

	c.mu.RLock()
	stdin := c.stdin
	processDone := c.processDone
	c.mu.RUnlock()
	if stdin == nil {
		return false, errClientNotRunning
	}
	if processDone != nil {
		select {
		case <-processDone:
			return false, errClientNotRunning
		default:
		}
	}

	// Minimal clients in unit tests deliberately do not start a worker. Keep
	// their historic synchronous behavior while production always uses the
	// bounded queue below.
	c.outboundMu.Lock()
	wake := c.outboundWake
	c.outboundMu.Unlock()
	if wake == nil {
		return true, c.sendEncoded(data)
	}

	item := &outboundMessage{data: data, size: len(data), kind: kind, uri: uri, version: version, documentGeneration: documentGeneration}
	if wait {
		item.done = make(chan error, 1)
	}

	c.outboundMu.Lock()
	if c.outboundUnhealthy {
		c.outboundMu.Unlock()
		return false, errOutboundUnhealthy
	}
	if kind == outboundDidChange {
		if pending := c.pendingChanges[uri]; pending != nil && pending.documentGeneration == documentGeneration {
			if version <= pending.version {
				c.outboundMu.Unlock()
				return true, nil
			}
			replacementSize := len(replacementData)
			if c.outboundBytes-pending.size+replacementSize > maxPendingOutboundBytes {
				c.outboundMu.Unlock()
				c.markOutboundUnhealthy()
				return false, fmt.Errorf("lsp outbound queue byte budget exceeded")
			}
			pending.data = replacementData
			c.outboundBytes += replacementSize - pending.size
			pending.size = replacementSize
			pending.version = version
			c.outboundMu.Unlock()
			c.signalOutboundWake(wake)
			return true, nil
		}
	} else {
		// A didChange added after a protocol barrier cannot replace a change that
		// must precede that barrier (for example save or a request).
		clear(c.pendingChanges)
	}
	if len(c.outboundQueue) >= maxPendingOutbound || c.outboundBytes+item.size > maxPendingOutboundBytes {
		c.outboundMu.Unlock()
		c.markOutboundUnhealthy()
		return false, fmt.Errorf("lsp outbound queue is full")
	}
	c.outboundQueue = append(c.outboundQueue, item)
	c.outboundBytes += item.size
	if kind == outboundDidChange {
		c.pendingChanges[uri] = item
	}
	c.outboundMu.Unlock()
	c.signalOutboundWake(wake)

	if !wait {
		return true, nil
	}
	return c.waitForOutboundWrite(ctx, item, processDone)
}

func (c *Client) signalOutboundWake(wake chan struct{}) {
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (c *Client) waitForOutboundWrite(ctx context.Context, item *outboundMessage, processDone <-chan struct{}) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case err := <-item.done:
		return err == nil, err
	case <-ctx.Done():
		if c.cancelQueuedOutbound(item, ctx.Err()) {
			return false, ctx.Err()
		}
		// The sole writer already owns this request, so it may still reach the
		// server. The caller sends $/cancelRequest behind it before returning.
		return true, ctx.Err()
	case <-processDone:
		if c.cancelQueuedOutbound(item, errClientNotRunning) {
			return false, errClientNotRunning
		}
		return true, errClientNotRunning
	}
}

func (c *Client) cancelQueuedOutbound(item *outboundMessage, err error) bool {
	c.outboundMu.Lock()
	for index, queued := range c.outboundQueue {
		if queued != item {
			continue
		}
		copy(c.outboundQueue[index:], c.outboundQueue[index+1:])
		c.outboundQueue[len(c.outboundQueue)-1] = nil
		c.outboundQueue = c.outboundQueue[:len(c.outboundQueue)-1]
		c.outboundBytes -= item.size
		if item.kind == outboundDidChange && c.pendingChanges[item.uri] == item {
			delete(c.pendingChanges, item.uri)
		}
		c.outboundMu.Unlock()
		c.finishOutbound(item, err)
		return true
	}
	c.outboundMu.Unlock()
	return false
}

func (c *Client) startOutboundWorker() {
	c.outboundMu.Lock()
	if c.outboundStarted {
		c.outboundMu.Unlock()
		return
	}
	c.outboundStarted = true
	c.outboundWake = make(chan struct{}, 1)
	c.pendingChanges = make(map[string]*outboundMessage)
	c.outboundMu.Unlock()
	go c.outboundLoop()
}

// outboundLoop is the sole owner of potentially blocking stdin writes. It
// removes each item from the bounded queue before writing, allowing producers
// to keep coalescing only changes that have not crossed the write boundary.
func (c *Client) outboundLoop() {
	for {
		item := c.takeOutbound()
		if item == nil {
			c.outboundMu.Lock()
			wake := c.outboundWake
			c.outboundMu.Unlock()
			c.mu.RLock()
			processDone := c.processDone
			c.mu.RUnlock()
			select {
			case <-wake:
				continue
			case <-processDone:
				c.failQueuedOutbound(errClientNotRunning)
				return
			}
		}
		if item.documentGeneration != 0 && !c.acceptsDocumentLifecycle(item.uri, item.documentGeneration) {
			// The app closed/reopened this URI after the command had been
			// marshaled. Dropping it here preserves the already-queued close and
			// prevents a stale document lifecycle message from reaching stdin.
			c.finishOutbound(item, nil)
			continue
		}

		err := c.sendEncoded(item.data)
		c.finishOutbound(item, err)
		if err != nil {
			if !isExpectedShutdownError(err) {
				log.Error("lsp: outbound write failed", "err", err)
			}
			c.markOutboundUnhealthy()
			c.failQueuedOutbound(err)
			return
		}
	}
}

func (c *Client) takeOutbound() *outboundMessage {
	c.outboundMu.Lock()
	defer c.outboundMu.Unlock()
	if len(c.outboundQueue) == 0 {
		return nil
	}
	item := c.outboundQueue[0]
	c.outboundQueue[0] = nil
	c.outboundQueue = c.outboundQueue[1:]
	c.outboundBytes -= item.size
	if item.kind == outboundDidChange && c.pendingChanges[item.uri] == item {
		delete(c.pendingChanges, item.uri)
	}
	return item
}

func (c *Client) failQueuedOutbound(err error) {
	c.outboundMu.Lock()
	queue := c.outboundQueue
	c.outboundQueue = nil
	c.outboundBytes = 0
	clear(c.pendingChanges)
	c.outboundMu.Unlock()
	for _, item := range queue {
		c.finishOutbound(item, err)
	}
}

func (c *Client) finishOutbound(item *outboundMessage, err error) {
	if item.done == nil {
		return
	}
	select {
	case item.done <- err:
	default:
	}
}

func (c *Client) markOutboundUnhealthy() {
	c.outboundMu.Lock()
	if c.outboundUnhealthy {
		c.outboundMu.Unlock()
		return
	}
	c.outboundUnhealthy = true
	c.outboundMu.Unlock()
	// A full barrier queue means ordering can no longer be guaranteed. Stop the
	// client deterministically rather than dropping a response or reordering a
	// document lifecycle message. Shutdown itself remains asynchronous.
	c.Shutdown()
}

var errOutboundUnhealthy = errors.New("lsp outbound queue is unhealthy")

// errOutboundMessageTooLarge rejects one message before it reaches the queue.
// Document callers turn this into an ordered didClose for that URI, while
// queue-wide capacity failures still transition the whole client unhealthy.
var errOutboundMessageTooLarge = errors.New("lsp outbound message exceeds transport limit")

func (c *Client) sendEncoded(data []byte) error {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.RLock()
	stdin := c.stdin
	c.mu.RUnlock()
	if stdin == nil {
		return errClientNotRunning
	}
	if _, err := io.WriteString(stdin, header); err != nil {
		return err
	}
	_, err := stdin.Write(data)
	return err
}

func (c *Client) readLoop(ctx context.Context) {
	defer func() {
		if c.readDone != nil {
			close(c.readDone)
		}
	}()
	go c.dispatchNotifications(ctx)
	defer func() {
		if c.cancelRead != nil {
			c.cancelRead()
		}
	}()

	buf := make([]byte, 4096)
	var accumulated []byte
	c.mu.RLock()
	stdout := c.stdout
	c.mu.RUnlock()
	if stdout == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := stdout.Read(buf)
		if err != nil {
			return
		}
		accumulated = append(accumulated, buf[:n]...)

		for {
			msg, rest, state := parseFrame(accumulated)
			switch state {
			case frameReady:
				accumulated = rest
				c.handleMessage(msg)
			case frameDiscard:
				accumulated = rest
			default:
				if len(accumulated) > maxBufferedInput {
					log.Warn("lsp: discarding unframed input", "size", len(accumulated), "max", maxBufferedInput)
					accumulated = retainPossibleHeaderPrefix(accumulated)
				}
			}
			if state != frameReady && state != frameDiscard {
				break
			}
		}
	}
}

func (c *Client) reapProcess() {
	if c.cmd == nil {
		return
	}
	err := c.cmd.Wait()
	if c.processDone != nil {
		close(c.processDone)
	}
	c.mu.RLock()
	expected := c.shuttingDown
	command := ""
	if c.cmd != nil && len(c.cmd.Args) > 0 {
		command = c.cmd.Args[0]
	}
	c.mu.RUnlock()
	if !expected && c.msgChan != nil {
		select {
		case c.msgChan <- ServerExitedMsg{Command: command, Err: err}:
		default:
		}
	}
}

func (c *Client) shutdownProcess() {
	ctx, cancel := context.WithTimeout(context.Background(), lspShutdownRequestTimeout)
	gracefulDone := make(chan struct{})
	go func() {
		defer close(gracefulDone)
		if _, err := c.callInternal(ctx, "shutdown", nil, false); err != nil && !isExpectedShutdownError(err) {
			log.Error("lsp: shutdown failed", "err", err)
		}
		exitCtx, exitCancel := context.WithTimeout(context.Background(), lspShutdownRequestTimeout)
		defer exitCancel()
		if err := c.sendBarrier(exitCtx, jsonrpcNotification{JSONRPC: "2.0", Method: "exit"}); err != nil && !isExpectedShutdownError(err) {
			log.Error("lsp: exit notification failed", "err", err)
		}
	}()
	select {
	case <-gracefulDone:
	case <-ctx.Done():
	}
	cancel()

	c.mu.Lock()
	stdin, stdout, cmd, cancelRead := c.stdin, c.stdout, c.cmd, c.cancelRead
	c.stdin = nil
	c.stdout = nil
	c.mu.Unlock()
	if cancelRead != nil {
		cancelRead()
	}
	if stdin != nil {
		_ = stdin.Close()
	}
	if stdout != nil {
		_ = stdout.Close()
	}
	if lspWaitForDone(c.processDone, lspShutdownRequestTimeout) {
		return
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if !lspWaitForDone(c.processDone, lspShutdownReapTimeout) {
		log.Warn("lsp: process did not exit after forced shutdown")
	}
}

func lspWaitForDone(done <-chan struct{}, timeout time.Duration) bool {
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

const (
	maxMessageSize         = 10 * 1024 * 1024 // 10MB maximum message size
	maxHeaderSize          = 16 * 1024        // enough for standard LSP headers
	maxBufferedInput       = maxMessageSize + maxHeaderSize
	maxQueuedNotifications = 128
)

type frameState uint8

const (
	frameIncomplete frameState = iota
	frameReady
	frameDiscard
)

func parseMessage(data []byte) (json.RawMessage, []byte, bool) {
	content, rest, state := parseFrame(data)
	return content, rest, state == frameReady
}

func parseFrame(data []byte) (json.RawMessage, []byte, frameState) {
	headerStart := indexContentLengthHeader(data)
	if headerStart < 0 {
		if len(data) > maxHeaderSize {
			return nil, retainPossibleHeaderPrefix(data), frameDiscard
		}
		return nil, data, frameIncomplete
	}

	remaining := data[headerStart:]
	headerEnd := bytes.Index(remaining, []byte("\r\n\r\n"))
	if headerEnd < 0 {
		if len(remaining) > maxHeaderSize {
			return nil, nextHeader(remaining[len("Content-Length:"):]), frameDiscard
		}
		return nil, data, frameIncomplete
	}
	headerEnd += len("\r\n\r\n")
	if headerEnd > maxHeaderSize {
		return nil, nextHeader(remaining[headerEnd:]), frameDiscard
	}

	contentLength, ok := contentLengthFromHeader(remaining[:headerEnd])
	if !ok || contentLength > int64(maxMessageSize) {
		if contentLength > int64(maxMessageSize) {
			log.Warn("lsp: rejecting message (too large)", "size", contentLength, "max", maxMessageSize)
		}
		return nil, nextHeader(remaining[headerEnd:]), frameDiscard
	}

	frameSize := headerEnd + int(contentLength)
	if len(remaining) < frameSize {
		return nil, data, frameIncomplete
	}
	return json.RawMessage(remaining[headerEnd:frameSize]), remaining[frameSize:], frameReady
}

func indexContentLengthHeader(data []byte) int {
	needle := []byte("Content-Length:")
	for i := 0; i+len(needle) <= len(data); i++ {
		if data[i] != 'C' && data[i] != 'c' {
			continue
		}
		if bytes.EqualFold(data[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

func contentLengthFromHeader(header []byte) (int64, bool) {
	var contentLength int64
	found := false
	for _, line := range bytes.Split(header, []byte("\r\n")) {
		name, value, ok := bytes.Cut(line, []byte(":"))
		if !ok || !bytes.EqualFold(bytes.TrimSpace(name), []byte("Content-Length")) {
			continue
		}
		if found {
			return 0, false
		}
		parsed, err := strconv.ParseInt(string(bytes.TrimSpace(value)), 10, 64)
		if err != nil || parsed < 0 {
			return 0, false
		}
		contentLength = parsed
		found = true
	}
	return contentLength, found
}

func nextHeader(data []byte) []byte {
	if idx := indexContentLengthHeader(data); idx >= 0 {
		return data[idx:]
	}
	return retainPossibleHeaderPrefix(data)
}

func retainPossibleHeaderPrefix(data []byte) []byte {
	needle := []byte("Content-Length:")
	maxPrefix := len(needle) - 1
	if len(data) > maxPrefix {
		data = data[len(data)-maxPrefix:]
	}
	for len(data) > 0 && !bytes.EqualFold(needle[:len(data)], data) {
		data = data[1:]
	}
	return data
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
	case "window/workDoneProgress/create":
		c.handleWorkDoneProgressCreate(peek.ID)
	case "client/registerCapability":
		c.handleClientRegisterCapability(peek.ID)
	case "client/unregisterCapability":
		c.handleClientUnregisterCapability(peek.ID)
	case "window/showMessageRequest":
		c.handleShowMessageRequest(peek.ID, peek.Params)
	case "workspace/applyEdit":
		c.handleApplyEdit(peek.ID, peek.Params)
	default:
		if peek.ID != nil {
			c.sendErrorResponse(*peek.ID, jsonrpcMethodNotFound, fmt.Sprintf("method not found: %s", peek.Method), nil)
		}
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
	c.queueNotification("showMessage", LspShowMessageMsg{
		Type:    p.Type,
		Message: p.Message,
	})
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
	// Progress reporting - can be extended to show in UI.
	c.queueNotification("progress:"+fmt.Sprintf("%T:%v", p.Token, p.Token), LspProgressMsg{Token: p.Token, Value: p.Value})
}

func (c *Client) handleWorkspaceConfiguration(id *int, params json.RawMessage) {
	// Respond with a nil entry per requested config item.
	// Some servers expect the result array length to match the requested items.
	if id == nil {
		return
	}

	var req struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		c.sendResponse(*id, []any{})
		return
	}

	resp := make([]any, len(req.Items))
	for i := range resp {
		resp[i] = nil
	}
	c.sendResponse(*id, resp)
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

func (c *Client) handleWorkDoneProgressCreate(id *int) {
	if id != nil {
		c.sendResponse(*id, nil)
	}
}

func (c *Client) handleClientRegisterCapability(id *int) {
	if id != nil {
		c.sendResponse(*id, nil)
	}
}

func (c *Client) handleClientUnregisterCapability(id *int) {
	if id != nil {
		c.sendResponse(*id, nil)
	}
}

func (c *Client) handleShowMessageRequest(id *int, params json.RawMessage) {
	if id == nil {
		return
	}
	c.handleShowMessage(params)
	c.sendResponse(*id, nil)
}

func (c *Client) handleApplyEdit(id *int, params json.RawMessage) {
	if id == nil {
		return
	}

	var request struct {
		Label string          `json:"label"`
		Edit  json.RawMessage `json:"edit"`
	}
	if err := json.Unmarshal(params, &request); err != nil {
		c.sendApplyEditResponse(*id, false, "invalid workspace/applyEdit parameters")
		return
	}
	if len(request.Edit) == 0 || string(request.Edit) == "null" {
		c.sendApplyEditResponse(*id, false, "workspace/applyEdit did not include an edit")
		return
	}

	var edit WorkspaceEdit
	if err := json.Unmarshal(request.Edit, &edit); err != nil {
		c.sendApplyEditResponse(*id, false, "invalid workspace edit")
		return
	}
	convertedEdit, err := c.workspaceEditFromProtocol(edit)
	if err != nil {
		c.sendApplyEditResponse(*id, false, "workspace edit has invalid or unavailable document positions")
		return
	}
	if c.msgChan == nil {
		c.sendApplyEditResponse(*id, false, "editor UI is unavailable")
		return
	}

	c.applyEditMu.Lock()
	if c.pendingApplyEdits >= maxPendingApplyEdits {
		c.applyEditMu.Unlock()
		c.sendApplyEditResponse(*id, false, "too many pending workspace edits")
		return
	}
	c.pendingApplyEdits++
	c.applyEditMu.Unlock()

	requestCtx, requestCancel := context.WithCancel(context.Background())
	type applyEditState uint8
	const (
		applyEditPending applyEditState = iota
		applyEditClaimed
		applyEditResponded
	)
	var responseMu sync.Mutex
	state := applyEditPending
	responseDone := make(chan struct{})
	respond := func(applied bool, failureReason string) {
		responseMu.Lock()
		if state == applyEditResponded {
			responseMu.Unlock()
			return
		}
		state = applyEditResponded
		responseMu.Unlock()
		requestCancel()
		c.applyEditMu.Lock()
		if c.pendingApplyEdits > 0 {
			c.pendingApplyEdits--
		}
		c.applyEditMu.Unlock()
		close(responseDone)
		// This callback is invoked by Bubble Tea Update. Enqueueing a response
		// is bounded and non-blocking; the sole writer owns any stdin stall.
		c.sendApplyEditResponse(*id, applied, failureReason)
	}
	claim := func() bool {
		responseMu.Lock()
		defer responseMu.Unlock()
		if state != applyEditPending {
			return false
		}
		state = applyEditClaimed
		return true
	}
	expire := func(failureReason string) bool {
		responseMu.Lock()
		if state != applyEditPending {
			responseMu.Unlock()
			return false
		}
		state = applyEditResponded
		responseMu.Unlock()
		requestCancel()
		c.applyEditMu.Lock()
		if c.pendingApplyEdits > 0 {
			c.pendingApplyEdits--
		}
		c.applyEditMu.Unlock()
		close(responseDone)
		c.sendApplyEditResponse(*id, false, failureReason)
		return true
	}
	uiRequest := ApplyEditRequestMsg{
		RequestID: *id,
		Label:     request.Label,
		Edit:      convertedEdit,
		Context:   requestCtx,
		Claim:     claim,
		Respond:   respond,
	}

	// JSON-RPC reading must keep draining server responses while the UI decides
	// whether a workspace edit is valid. A bounded handoff and timeout prevent a
	// stalled Bubble Tea program from accumulating blocked reader goroutines.
	go func() {
		timer := time.NewTimer(applyEditResponseTimeout)
		defer timer.Stop()
		select {
		case c.msgChan <- uiRequest:
		case <-timer.C:
			expire("timed out waiting for editor to apply workspace edit")
			return
		case <-c.processDone:
			expire("language server stopped before workspace edit was applied")
			return
		}

		select {
		case <-responseDone:
		case <-timer.C:
			expire("timed out waiting for editor to apply workspace edit")
		case <-c.processDone:
			expire("language server stopped before workspace edit was applied")
		}
	}()
}

func (c *Client) sendApplyEditResponse(id int, applied bool, failureReason string) {
	result := map[string]any{"applied": applied}
	if !applied {
		if failureReason == "" {
			failureReason = "workspace edit was rejected"
		}
		result["failureReason"] = failureReason
	}
	c.sendResponse(id, result)
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
	if err := c.enqueueNotification(resp, outboundBarrier, "", nil); err != nil {
		log.Error("lsp: failed to send response", "err", err, "id", id)
	}
}

func (c *Client) sendErrorResponse(id int, code int, message string, data any) {
	resp := jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonrpcError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	if err := c.enqueueNotification(resp, outboundBarrier, "", nil); err != nil {
		log.Error("lsp: failed to send error response", "err", err, "id", id, "code", code)
	}
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

	diags := make([]Diagnostic, 0, len(p.Diagnostics))
	for _, d := range p.Diagnostics {
		diagnostic := Diagnostic{
			Range: DiagRange{
				Start: DiagPosition{Line: d.Range.Start.Line, Character: d.Range.Start.Character},
				End:   DiagPosition{Line: d.Range.End.Line, Character: d.Range.End.Character},
			},
			Severity: DiagSeverity(d.Severity),
			Message:  d.Message,
			Source:   d.Source,
		}
		converted, err := c.diagnosticFromProtocol(p.URI, diagnostic)
		if err != nil {
			// Diagnostics are advisory. A malformed or stale range must not move
			// a visible underline to a different byte offset; retain the other
			// diagnostics from this publication instead.
			log.Warn("lsp: dropping diagnostic with invalid position", "uri", p.URI, "err", err)
			continue
		}
		diags = append(diags, converted)
	}

	c.queueNotification("diagnostics:"+p.URI, DiagnosticsMsg{
		URI:         p.URI,
		Diagnostics: diags,
	})
}

func (c *Client) queueNotification(key string, msg any) {
	if c.msgChan == nil {
		return
	}

	c.notificationMu.Lock()
	if c.pendingNotification == nil {
		c.pendingNotification = make(map[string]*queuedNotification)
	}
	if c.notificationWake == nil {
		c.notificationWake = make(chan struct{}, 1)
	}
	if queued, ok := c.pendingNotification[key]; ok {
		queued.msg = msg
		queued.generation++
		c.notificationMu.Unlock()
		return
	}
	if len(c.notificationQueue) >= maxQueuedNotifications {
		dropped := c.notificationQueue[0]
		c.notificationQueue = c.notificationQueue[1:]
		delete(c.pendingNotification, dropped.key)
		log.Warn("lsp: notification queue full; dropping oldest notification", "key", dropped.key, "max", maxQueuedNotifications)
	}
	queued := &queuedNotification{key: key, msg: msg, generation: 1}
	c.notificationQueue = append(c.notificationQueue, queued)
	c.pendingNotification[key] = queued
	wake := c.notificationWake
	c.notificationMu.Unlock()

	select {
	case wake <- struct{}{}:
	default:
	}
}

func (c *Client) dispatchNotifications(ctx context.Context) {
	for {
		c.notificationMu.Lock()
		if c.notificationWake == nil {
			c.notificationWake = make(chan struct{}, 1)
		}
		wake := c.notificationWake
		hasQueued := len(c.notificationQueue) > 0
		c.notificationMu.Unlock()

		if !hasQueued {
			select {
			case <-ctx.Done():
				return
			case <-wake:
			}
			continue
		}

		c.notificationMu.Lock()
		queued := c.notificationQueue[0]
		msg := queued.msg
		generation := queued.generation
		c.notificationMu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-wake:
			// A newer notification may have replaced queued.msg. Re-evaluate it
			// before attempting delivery so diagnostics remain coalesced.
			continue
		case c.msgChan <- msg:
			c.notificationMu.Lock()
			if len(c.notificationQueue) > 0 && c.notificationQueue[0] == queued {
				if queued.generation == generation {
					c.notificationQueue = c.notificationQueue[1:]
					if c.pendingNotification[queued.key] == queued {
						delete(c.pendingNotification, queued.key)
					}
				}
			}
			c.notificationMu.Unlock()
		}
	}
}
