package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	log "github.com/charmbracelet/log"
)

const (
	maxDAPMessageSize      = 16 << 20
	maxDAPHeaderBytes      = 8 << 10
	maxDAPOutputEventBytes = 64 << 10
	maxDAPQueuedOutput     = 64
	maxDAPQueuedCritical   = 64
	shutdownRequestTimeout = 750 * time.Millisecond
	shutdownReapTimeout    = 2 * time.Second
)

// Client manages communication with a DAP debug adapter.
type Client struct {
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	mu           sync.Mutex
	pending      map[int]chan callResult
	running      bool
	initialized  bool
	msgChan      chan<- any
	seq          int
	writeMu      sync.Mutex
	shutdownOnce sync.Once
	processDone  chan struct{}
	readDone     chan struct{}

	// The adapter's protocol reader must never wait for Bubble Tea to process a
	// message. Keep bounded, priority-separated queues so noisy output cannot
	// evict lifecycle transitions while the UI is busy.
	eventMu        sync.Mutex
	criticalEvents []any
	outputEvents   []any
	eventWake      chan struct{}
	eventStop      chan struct{}
	eventDone      chan struct{}
	eventStopOnce  sync.Once
}

type callResult struct {
	Result json.RawMessage
	Error  *ErrorResponse
}

// resolveCommand tries to find a command in PATH, then in common Go binary locations.
func resolveCommand(command string) string {
	if p, err := exec.LookPath(command); err == nil {
		return p
	}
	// Check GOBIN and GOPATH/bin
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		if p := filepath.Join(gobin, command); fileExists(p) {
			return p
		}
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		if p := filepath.Join(gopath, "bin", command); fileExists(p) {
			return p
		}
	}
	// Check ~/go/bin as fallback
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, "go", "bin", command); fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isDelveDAP(command string, args []string) bool {
	base := filepath.Base(command)
	if base != "dlv" && base != "dlv.exe" {
		return false
	}
	for _, arg := range args {
		if arg == "dap" {
			return true
		}
	}
	return false
}

func findClientAddrArg(args []string) (string, bool) {
	for i, arg := range args {
		if strings.HasPrefix(arg, "--client-addr=") {
			return strings.TrimPrefix(arg, "--client-addr="), true
		}
		if arg == "--client-addr" && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func addClientAddrArg(args []string, addr string) []string {
	return append(args, "--client-addr="+addr)
}

func waitForDial(listener net.Listener, timeout time.Duration) (net.Conn, error) {
	type acceptResult struct {
		conn net.Conn
		err  error
	}
	resultCh := make(chan acceptResult, 1)
	go func() {
		conn, err := listener.Accept()
		resultCh <- acceptResult{conn: conn, err: err}
	}()
	select {
	case result := <-resultCh:
		return result.conn, result.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for debug adapter to connect")
	}
}

// NewClient creates a new DAP client and starts the debug adapter process.
func NewClient(command string, args []string, msgChan chan<- any) (*Client, error) {
	resolved := resolveCommand(command)
	if resolved == "" {
		return nil, fmt.Errorf("debug adapter %q not found in PATH. Install with: go install github.com/go-delve/delve/cmd/dlv@latest", command)
	}

	var (
		cmd    *exec.Cmd
		stdin  io.WriteCloser
		stdout io.ReadCloser
	)

	if isDelveDAP(resolved, args) {
		addr, hasAddr := findClientAddrArg(args)
		if !hasAddr {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return nil, fmt.Errorf("listen for delve client-addr: %w", err)
			}
			addr = listener.Addr().String()
			args = addClientAddrArg(args, addr)
			cmd = exec.Command(resolved, args...)
			cmd.Dir = "."
			if err := cmd.Start(); err != nil {
				_ = listener.Close()
				return nil, fmt.Errorf("start %s: %w", command, err)
			}
			conn, err := waitForDial(listener, 10*time.Second)
			_ = listener.Close()
			if err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return nil, fmt.Errorf("delve did not dial client-addr %q: %w", addr, err)
			}
			stdin = conn
			stdout = conn
		} else {
			listener, err := net.Listen("tcp", addr)
			if err != nil {
				return nil, fmt.Errorf("listen on provided client-addr %q: %w", addr, err)
			}
			cmd = exec.Command(resolved, args...)
			cmd.Dir = "."
			if err := cmd.Start(); err != nil {
				_ = listener.Close()
				return nil, fmt.Errorf("start %s: %w", command, err)
			}
			conn, err := waitForDial(listener, 10*time.Second)
			_ = listener.Close()
			if err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return nil, fmt.Errorf("delve did not dial client-addr %q: %w", addr, err)
			}
			stdin = conn
			stdout = conn
		}
	} else {
		cmd = exec.Command(resolved, args...)
		cmd.Dir = "."
		var err error
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("stdin pipe: %w", err)
		}

		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("stdout pipe: %w", err)
		}

		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start %s: %w", command, err)
		}
	}

	c := &Client{
		cmd:         cmd,
		stdin:       stdin,
		stdout:      stdout,
		pending:     make(map[int]chan callResult),
		running:     true,
		msgChan:     msgChan,
		seq:         0,
		processDone: make(chan struct{}),
		readDone:    make(chan struct{}),
	}
	c.initEventDelivery()

	go c.reapProcess()
	go c.readLoop()

	return c, nil
}

// nextSeq returns the next sequence number.
func (c *Client) nextSeq() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}

// Initialize sends the initialize request.
func (c *Client) Initialize() error {
	args := InitializeRequestArgs{
		AdapterID:       "teak",
		PathFormat:      "path",
		LinesStartAt1:   true,
		ColumnsStartAt1: true,
	}

	var result json.RawMessage
	err := c.sendRequest("initialize", args, &result)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return nil
}

// Launch starts debugging the specified program.
func (c *Client) Launch(program string) error {
	args := LaunchRequestArgs{
		Program: program,
		Mode:    "debug",
	}
	if err := c.sendRequest("launch", args, nil); err != nil {
		return err
	}
	// Most adapters (including Delve) expect configurationDone after launch/configuration.
	return c.sendRequest("configurationDone", nil, nil)
}

// SetBreakpoints sets breakpoints in a source file.
func (c *Client) SetBreakpoints(sourcePath string, breakpoints []int) ([]Breakpoint, error) {
	srcBreakpoints := make([]SourceBreakpoint, len(breakpoints))
	for i, line := range breakpoints {
		srcBreakpoints[i] = SourceBreakpoint{Line: line}
	}

	args := SetBreakpointsRequestArgs{
		Source: Source{
			Path: sourcePath,
		},
		Breakpoints: srcBreakpoints,
	}

	var result json.RawMessage
	err := c.sendRequest("setBreakpoints", args, &result)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Breakpoints []Breakpoint `json:"breakpoints"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return resp.Breakpoints, nil
}

// Continue resumes execution of a thread.
func (c *Client) Continue(threadId int) error {
	args := ContinueRequestArgs{
		ThreadId: threadId,
	}
	return c.sendRequest("continue", args, nil)
}

// Next steps over to the next line.
func (c *Client) Next(threadId int) error {
	args := map[string]int{"threadId": threadId}
	return c.sendRequest("next", args, nil)
}

// StepIn steps into a function call.
func (c *Client) StepIn(threadId int) error {
	args := map[string]int{"threadId": threadId}
	return c.sendRequest("stepIn", args, nil)
}

// StepOut steps out of the current function.
func (c *Client) StepOut(threadId int) error {
	args := map[string]int{"threadId": threadId}
	return c.sendRequest("stepOut", args, nil)
}

// StackTrace retrieves the stack trace for a thread.
func (c *Client) StackTrace(threadId int) ([]StackFrame, error) {
	args := StackTraceRequestArgs{
		ThreadId: threadId,
	}

	var result json.RawMessage
	err := c.sendRequest("stackTrace", args, &result)
	if err != nil {
		return nil, err
	}

	var resp StackTraceResponseBody
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return resp.StackFrames, nil
}

// Threads retrieves all threads.
func (c *Client) Threads() ([]Thread, error) {
	var result json.RawMessage
	err := c.sendRequest("threads", nil, &result)
	if err != nil {
		return nil, err
	}

	var resp ThreadsResponseBody
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return resp.Threads, nil
}

// Scopes retrieves the scopes for a stack frame.
func (c *Client) Scopes(frameId int) ([]Scope, error) {
	args := ScopesRequestArgs{
		FrameId: frameId,
	}

	var result json.RawMessage
	err := c.sendRequest("scopes", args, &result)
	if err != nil {
		return nil, err
	}

	var resp ScopesResponseBody
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return resp.Scopes, nil
}

// Variables retrieves the variables for a scope.
func (c *Client) Variables(variablesReference int) ([]Variable, error) {
	args := VariablesRequestArgs{
		VariablesReference: variablesReference,
	}

	var result json.RawMessage
	err := c.sendRequest("variables", args, &result)
	if err != nil {
		return nil, err
	}

	var resp VariablesResponseBody
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return resp.Variables, nil
}

// Disconnect stops the debug session.
func (c *Client) Disconnect() error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownRequestTimeout)
	defer cancel()
	return c.disconnectContext(ctx)
}

func (c *Client) disconnectContext(ctx context.Context) error {
	args := map[string]bool{
		"restart":           false,
		"terminateDebuggee": true,
	}
	return c.sendRequestContext(ctx, "disconnect", args, nil)
}

// Shutdown gracefully shuts down the debug adapter.
func (c *Client) Shutdown() {
	c.shutdownOnce.Do(func() {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		c.stopEventDelivery()
		go c.shutdownProcess()
	})
}

// WaitForShutdown waits until the adapter process has been reaped or ctx is
// cancelled. It is intended for bounded application teardown outside Update.
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

// IsReady returns whether the client has completed initialization.
func (c *Client) IsReady() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initialized
}

func (c *Client) sendRequest(command string, args any, result *json.RawMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return c.sendRequestContext(ctx, command, args, result)
}

func (c *Client) sendRequestContext(ctx context.Context, command string, args any, result *json.RawMessage) error {
	seq := c.nextSeq()
	c.mu.Lock()
	ch := make(chan callResult, 1)
	c.pending[seq] = ch
	c.mu.Unlock()

	req := Request{
		Seq:       seq,
		Type:      "request",
		Command:   command,
		Arguments: args,
	}

	if err := c.send(req); err != nil {
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		return err
	}

	var res callResult
	select {
	case res = <-ch:
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		return fmt.Errorf("DAP request %q: %w", command, ctx.Err())
	}
	c.mu.Lock()
	delete(c.pending, seq)
	c.mu.Unlock()

	if res.Error != nil {
		return fmt.Errorf("DAP error %d: %s", res.Error.Id, res.Error.Message)
	}

	if result != nil {
		*result = res.Result
	}
	return nil
}

func (c *Client) send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	stdin := c.stdin
	c.mu.Unlock()
	if stdin == nil {
		return errors.New("DAP client stdin is closed")
	}
	if _, err := io.WriteString(stdin, header); err != nil {
		return err
	}
	_, err = stdin.Write(data)
	return err
}

func (c *Client) readLoop() {
	defer func() {
		if c.readDone != nil {
			close(c.readDone)
		}
	}()
	reader := bufio.NewReader(c.stdout)

	for {
		content, err := readDAPFrame(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Error("dap: read frame error", "err", err)
			}
			return
		}

		c.handleMessage(content)
	}
}

func (c *Client) reapProcess() {
	if c.cmd == nil {
		return
	}
	_ = c.cmd.Wait()
	if c.processDone != nil {
		close(c.processDone)
	}
}

func (c *Client) shutdownProcess() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownRequestTimeout)
	gracefulDone := make(chan struct{})
	go func() {
		_ = c.disconnectContext(ctx)
		close(gracefulDone)
	}()
	select {
	case <-gracefulDone:
	case <-ctx.Done():
	}
	cancel()

	c.mu.Lock()
	stdin, stdout, cmd := c.stdin, c.stdout, c.cmd
	c.stdin = nil
	c.stdout = nil
	c.mu.Unlock()
	if stdin != nil {
		_ = stdin.Close()
	}
	if stdout != nil {
		_ = stdout.Close()
	}

	if waitForDone(c.processDone, shutdownRequestTimeout) {
		return
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if !waitForDone(c.processDone, shutdownReapTimeout) {
		log.Warn("dap: process did not exit after forced shutdown")
	}
}

func waitForDone(done <-chan struct{}, timeout time.Duration) bool {
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

func readDAPFrame(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	headerBytes := 0

	for {
		line, err := reader.ReadSlice('\n')
		headerBytes += len(line)
		if err == bufio.ErrBufferFull || headerBytes > maxDAPHeaderBytes {
			return nil, fmt.Errorf("DAP header exceeds limit of %d bytes", maxDAPHeaderBytes)
		}
		if err != nil {
			return nil, err
		}

		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			break
		}

		name, value, ok := strings.Cut(trimmed, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue
		}
		if contentLength >= 0 {
			return nil, fmt.Errorf("duplicate Content-Length header")
		}

		parsed, parseErr := strconv.Atoi(strings.TrimSpace(value))
		if parseErr != nil || parsed <= 0 {
			return nil, fmt.Errorf("invalid Content-Length %q", strings.TrimSpace(value))
		}
		if parsed > maxDAPMessageSize {
			return nil, fmt.Errorf("Content-Length %d exceeds limit of %d bytes", parsed, maxDAPMessageSize)
		}
		contentLength = parsed
	}

	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	content := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, content); err != nil {
		return nil, fmt.Errorf("read DAP content: %w", err)
	}
	return content, nil
}

func (c *Client) handleMessage(data []byte) {
	// First, try to parse as response
	var resp Response
	if err := json.Unmarshal(data, &resp); err == nil && resp.Type == "response" {
		c.mu.Lock()
		ch, ok := c.pending[resp.RequestSeq]
		c.mu.Unlock()

		if ok {
			var errResp *ErrorResponse
			if !resp.Success {
				errResp = &ErrorResponse{
					Message: resp.Message,
				}
				// Try to parse structured error from body
				if len(resp.Body) > 0 {
					var bodyErr struct {
						Error *ErrorResponse `json:"error"`
					}
					if json.Unmarshal(resp.Body, &bodyErr) == nil && bodyErr.Error != nil {
						errResp = bodyErr.Error
						if errResp.Message == "" {
							errResp.Message = resp.Message
						}
						if errResp.Format != "" && errResp.Message == "" {
							errResp.Message = errResp.Format
						}
					}
				}
			}
			// A compliant adapter replies once per request, but a duplicate or late
			// response must not stall the sole protocol reader. The request channel
			// is deliberately one-slot buffered so the normal response can arrive
			// just before its caller starts waiting; additional responses are stale.
			select {
			case ch <- callResult{
				Result: resp.Body,
				Error:  errResp,
			}:
			default:
				log.Warn("dap: dropping duplicate or late response", "request_seq", resp.RequestSeq)
			}
		}
		return
	}

	// Try to parse as event
	var event Event
	if err := json.Unmarshal(data, &event); err == nil && event.Type == "event" {
		c.handleEvent(&event)
		return
	}
}

func (c *Client) handleEvent(event *Event) {
	if c.msgChan == nil {
		return
	}

	switch event.Event {
	case "stopped":
		if body, ok := event.Body.(map[string]any); ok {
			c.emit(StoppedEventMsg{
				Reason:            boundedDAPText(getStr(body, "reason")),
				Description:       boundedDAPText(getStr(body, "description")),
				ThreadId:          getInt(body, "threadId"),
				AllThreadsStopped: getBool(body, "allThreadsStopped"),
			})
		}
	case "continued":
		if body, ok := event.Body.(map[string]any); ok {
			c.emit(ContinuedEventMsg{
				ThreadId:            getInt(body, "threadId"),
				AllThreadsContinued: getBool(body, "allThreadsContinued"),
			})
		}
	case "exited":
		if body, ok := event.Body.(map[string]any); ok {
			c.emit(ExitedEventMsg{
				ExitCode: int(getInt(body, "exitCode")),
			})
		}
	case "terminated":
		c.emit(TerminatedEventMsg{})
	case "output":
		if body, ok := event.Body.(map[string]any); ok {
			c.emit(OutputEventMsg{
				Category: boundedDAPText(getStr(body, "category")),
				Output:   boundedDAPText(getStr(body, "output")),
			})
		}
	case "breakpoint":
		if body, ok := event.Body.(map[string]any); ok {
			// DAP breakpoint payload can be either:
			// {"reason":"changed","breakpoint":{...}}
			// or a flattened body in some adapters.
			breakpointBody := body
			if nested, nok := body["breakpoint"].(map[string]any); nok {
				breakpointBody = nested
			}
			bp := Breakpoint{
				Verified: getBool(breakpointBody, "verified"),
				Message:  boundedDAPText(getStr(breakpointBody, "message")),
				Line:     getInt(breakpointBody, "line"),
			}
			if src, sok := breakpointBody["source"].(map[string]any); sok {
				bp.Source = Source{
					Name: boundedDAPText(getStr(src, "name")),
					Path: boundedDAPText(getStr(src, "path")),
				}
			}
			c.emit(BreakpointEventMsg{
				Reason:     boundedDAPText(getStr(body, "reason")),
				Breakpoint: bp,
			})
		}
	}
}

func boundedDAPText(value string) string {
	if len(value) <= maxDAPOutputEventBytes {
		return value
	}
	start := len(value) - maxDAPOutputEventBytes
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:]
}

// emit never lets an adapter flood or a stopped UI block the protocol reader.
// Lifecycle events use a separate FIFO queue, so output cannot crowd out a
// stopped, exited, or terminated notification. The queues and the dispatcher
// are only initialized for real clients; retaining a direct non-blocking send
// keeps small, manually constructed clients useful in unit tests.
func (c *Client) emit(msg any) {
	if c.msgChan == nil {
		return
	}
	if c.eventWake == nil {
		select {
		case c.msgChan <- msg:
		default:
			log.Warn("dap: dropping UI event because message queue is full")
		}
		return
	}

	c.eventMu.Lock()
	if isDAPCriticalEvent(msg) {
		c.enqueueCriticalEventLocked(msg)
	} else if len(c.outputEvents) < maxDAPQueuedOutput {
		c.outputEvents = append(c.outputEvents, msg)
	}
	c.eventMu.Unlock()
	c.signalEventDispatcher()
}

func (c *Client) initEventDelivery() {
	if c.msgChan == nil || c.eventWake != nil {
		return
	}
	c.eventWake = make(chan struct{}, 1)
	c.eventStop = make(chan struct{})
	c.eventDone = make(chan struct{})
	go c.eventDeliveryLoop()
}

func (c *Client) stopEventDelivery() {
	if c.eventStop == nil {
		return
	}
	c.eventStopOnce.Do(func() {
		close(c.eventStop)
	})
}

func (c *Client) eventDeliveryLoop() {
	defer close(c.eventDone)
	for {
		msg, ok := c.nextQueuedEvent()
		if !ok {
			select {
			case <-c.eventWake:
				continue
			case <-c.eventStop:
				return
			case <-c.readDone:
				// The protocol stream ended and there is no queued notification
				// left to deliver, so retaining a dispatcher would leak a goroutine.
				return
			}
		}

		select {
		case c.msgChan <- msg:
		case <-c.eventStop:
			return
		}
	}
}

func (c *Client) nextQueuedEvent() (any, bool) {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	if len(c.criticalEvents) > 0 {
		msg := c.criticalEvents[0]
		c.criticalEvents[0] = nil
		c.criticalEvents = c.criticalEvents[1:]
		return msg, true
	}
	if len(c.outputEvents) > 0 {
		msg := c.outputEvents[0]
		c.outputEvents[0] = nil
		c.outputEvents = c.outputEvents[1:]
		return msg, true
	}
	return nil, false
}

func (c *Client) signalEventDispatcher() {
	select {
	case c.eventWake <- struct{}{}:
	default:
	}
}

func isDAPCriticalEvent(msg any) bool {
	switch msg.(type) {
	case StoppedEventMsg, ContinuedEventMsg, ExitedEventMsg, TerminatedEventMsg:
		return true
	default:
		return false
	}
}

func (c *Client) enqueueCriticalEventLocked(msg any) {
	if len(c.criticalEvents) < maxDAPQueuedCritical {
		c.criticalEvents = append(c.criticalEvents, msg)
		return
	}

	// An adapter should not flood lifecycle events, but if it does, preserve a
	// final exit/termination by evicting an older transient state first.
	for i, queued := range c.criticalEvents {
		switch queued.(type) {
		case StoppedEventMsg, ContinuedEventMsg:
			c.criticalEvents = append(c.criticalEvents[:i], c.criticalEvents[i+1:]...)
			c.criticalEvents = append(c.criticalEvents, msg)
			return
		}
	}
	log.Warn("dap: dropping lifecycle event because critical queue is full")
}

func getStr(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(m map[string]any, key string) int {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return int(val)
		case int:
			return val
		}
	}
	return 0
}

func getBool(m map[string]any, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}
