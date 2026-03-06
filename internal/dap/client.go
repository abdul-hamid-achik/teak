package dap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/charmbracelet/log"
)

// Client manages communication with a DAP debug adapter.
type Client struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	mu          sync.Mutex
	pending     map[int]chan callResult
	running     bool
	initialized bool
	msgChan     chan<- any
	seq         int
}

type callResult struct {
	Result json.RawMessage
	Error  *ErrorResponse
}

// Protocol types

// Request represents a DAP request.
type Request struct {
	Seq       int    `json:"seq"`
	Type      string `json:"type"`
	Command   string `json:"command"`
	Arguments any    `json:"arguments,omitempty"`
}

// Event represents a DAP event.
type Event struct {
	Seq   int    `json:"seq"`
	Type  string `json:"type"`
	Event string `json:"event"`
	Body  any    `json:"body,omitempty"`
}

// Response represents a DAP response.
type Response struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	RequestSeq int             `json:"request_seq"`
	Command    string          `json:"command"`
	Success    bool            `json:"success"`
	Message    string          `json:"message,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

// ErrorResponse represents an error in a DAP response.
type ErrorResponse struct {
	Id                 int    `json:"id"`
	Format             string `json:"format"`
	Message            string `json:"message"`
	SendTelemetry      bool   `json:"sendTelemetry"`
	ShowUser           bool   `json:"showUser"`
	VariablesReference int    `json:"variablesReference"`
}

// InitializeRequest arguments
type InitializeRequestArgs struct {
	AdapterID       string `json:"adapterID"`
	PathFormat      string `json:"pathFormat,omitempty"`
	LinesStartAt1   bool   `json:"linesStartAt1,omitempty"`
	ColumnsStartAt1 bool   `json:"columnsStartAt1,omitempty"`
}

// LaunchRequest arguments
type LaunchRequestArgs struct {
	Program string            `json:"program"`
	Mode    string            `json:"mode,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// SetBreakpointsRequest arguments
type SetBreakpointsRequestArgs struct {
	Source      Source             `json:"source"`
	Breakpoints []SourceBreakpoint `json:"breakpoints"`
}

// Source represents a source file.
type Source struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

// SourceBreakpoint represents a breakpoint in source code.
type SourceBreakpoint struct {
	Line   int `json:"line"`
	Column int `json:"column,omitempty"`
}

// Breakpoint represents a breakpoint.
type Breakpoint struct {
	Verified bool   `json:"verified"`
	Message  string `json:"message,omitempty"`
	Source   Source `json:"source,omitempty"`
	Line     int    `json:"line"`
	Column   int    `json:"column,omitempty"`
}

// StackTraceRequest arguments
type StackTraceRequestArgs struct {
	ThreadId   int `json:"threadId"`
	StartFrame int `json:"startFrame,omitempty"`
	Levels     int `json:"levels,omitempty"`
}

// StackTraceResponse body
type StackTraceResponseBody struct {
	StackFrames []StackFrame `json:"stackFrames"`
	TotalFrames int          `json:"totalFrames"`
}

// StackFrame represents a stack frame.
type StackFrame struct {
	Id               int    `json:"id"`
	Name             string `json:"name"`
	Source           Source `json:"source,omitempty"`
	Line             int    `json:"line"`
	Column           int    `json:"column"`
	PresentationHint string `json:"presentationHint,omitempty"`
}

// ThreadsResponse body
type ThreadsResponseBody struct {
	Threads []Thread `json:"threads"`
}

// Thread represents a thread.
type Thread struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

// ScopesRequest arguments
type ScopesRequestArgs struct {
	FrameId int `json:"frameId"`
}

// ScopesResponse body
type ScopesResponseBody struct {
	Scopes []Scope `json:"scopes"`
}

// Scope represents a scope (e.g., Locals, Globals).
type Scope struct {
	Name               string `json:"name"`
	PresentationHint   string `json:"presentationHint,omitempty"`
	VariablesReference int    `json:"variablesReference"`
	Expensive          bool   `json:"expensive"`
}

// VariablesRequest arguments
type VariablesRequestArgs struct {
	VariablesReference int `json:"variablesReference"`
}

// VariablesResponse body
type VariablesResponseBody struct {
	Variables []Variable `json:"variables"`
}

// Variable represents a variable.
type Variable struct {
	Name               string `json:"name"`
	Value              string `json:"value"`
	Type               string `json:"type,omitempty"`
	VariablesReference int    `json:"variablesReference"`
	PresentationHint   string `json:"presentationHint,omitempty"`
}

// ContinueRequest arguments
type ContinueRequestArgs struct {
	ThreadId int `json:"threadId"`
}

// ContinueResponse body
type ContinueResponseBody struct {
	AllThreadsContinued bool `json:"allThreadsContinued"`
}

// StoppedEvent body
type StoppedEventBody struct {
	Reason            string `json:"reason"`
	Description       string `json:"description,omitempty"`
	ThreadId          int    `json:"threadId,omitempty"`
	AllThreadsStopped bool   `json:"allThreadsStopped,omitempty"`
}

// ExitedEvent body
type ExitedEventBody struct {
	ExitCode int `json:"exitCode"`
}

// OutputEvent body
type OutputEventBody struct {
	Category string `json:"category,omitempty"`
	Output   string `json:"output"`
	Source   Source `json:"source,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
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

// NewClient creates a new DAP client and starts the debug adapter process.
func NewClient(command string, args []string, msgChan chan<- any) (*Client, error) {
	resolved := resolveCommand(command)
	if resolved == "" {
		return nil, fmt.Errorf("debug adapter %q not found in PATH. Install with: go install github.com/go-delve/delve/cmd/dlv@latest", command)
	}

	cmd := exec.Command(resolved, args...)
	cmd.Dir = "."

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command, err)
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		pending: make(map[int]chan callResult),
		running: true,
		msgChan: msgChan,
		seq:     0,
	}

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

	// Send initialized event
	if err := c.sendEvent("initialized", map[string]any{}); err != nil {
		return fmt.Errorf("initialized event: %w", err)
	}
	// DAP spec: configurationDone must follow initialized
	return c.sendRequest("configurationDone", nil, nil)
}

// Launch starts debugging the specified program.
func (c *Client) Launch(program string) error {
	args := LaunchRequestArgs{
		Program: program,
		Mode:    "debug",
	}

	return c.sendRequest("launch", args, nil)
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
	args := map[string]bool{
		"restart":           false,
		"terminateDebuggee": true,
	}
	return c.sendRequest("disconnect", args, nil)
}

// Shutdown gracefully shuts down the debug adapter.
func (c *Client) Shutdown() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.mu.Unlock()

	c.Disconnect()
	c.stdin.Close()
	c.cmd.Wait()
}

// IsReady returns whether the client has completed initialization.
func (c *Client) IsReady() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initialized
}

func (c *Client) sendRequest(command string, args any, result *json.RawMessage) error {
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
	case <-time.After(30 * time.Second):
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		return fmt.Errorf("DAP request %q timed out after 30s", command)
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

func (c *Client) sendEvent(event string, body any) error {
	e := Event{
		Seq:   c.nextSeq(),
		Type:  "event",
		Event: event,
		Body:  body,
	}
	return c.send(e)
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

func (c *Client) readLoop() {
	reader := bufio.NewReader(c.stdout)

	for {
		// Read Content-Length header
		var contentLength int
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					return
				}
				log.Error("dap: read error", "err", err)
				return
			}

			line = strings.TrimSpace(line)
			if line == "" {
				break
			}

			if strings.HasPrefix(line, "Content-Length:") {
				lengthStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
				contentLength, err = strconv.Atoi(lengthStr)
				if err != nil {
					log.Error("dap: invalid content length", "err", err)
					return
				}
			}
		}

		// Read content
		content := make([]byte, contentLength)
		_, err := io.ReadFull(reader, content)
		if err != nil {
			log.Error("dap: read content error", "err", err)
			return
		}

		c.handleMessage(content)
	}
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
			ch <- callResult{
				Result: resp.Body,
				Error:  errResp,
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
			c.msgChan <- StoppedEventMsg{
				Reason:            getStr(body, "reason"),
				Description:       getStr(body, "description"),
				ThreadId:          getInt(body, "threadId"),
				AllThreadsStopped: getBool(body, "allThreadsStopped"),
			}
		}
	case "continued":
		if body, ok := event.Body.(map[string]any); ok {
			c.msgChan <- ContinuedEventMsg{
				ThreadId:            getInt(body, "threadId"),
				AllThreadsContinued: getBool(body, "allThreadsContinued"),
			}
		}
	case "exited":
		if body, ok := event.Body.(map[string]any); ok {
			c.msgChan <- ExitedEventMsg{
				ExitCode: int(getInt(body, "exitCode")),
			}
		}
	case "terminated":
		c.msgChan <- TerminatedEventMsg{}
	case "output":
		if body, ok := event.Body.(map[string]any); ok {
			c.msgChan <- OutputEventMsg{
				Category: getStr(body, "category"),
				Output:   getStr(body, "output"),
			}
		}
	case "breakpoint":
		if body, ok := event.Body.(map[string]any); ok {
			c.msgChan <- BreakpointEventMsg{
				Reason: getStr(body, "reason"),
				Breakpoint: Breakpoint{
					Verified: getBool(body, "verified"),
					Message:  getStr(body, "message"),
					Line:     getInt(body, "line"),
				},
			}
		}
	}
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
