package dap

import "encoding/json"

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

// InitializeRequestArgs is the arguments for the initialize request.
type InitializeRequestArgs struct {
	AdapterID       string `json:"adapterID"`
	PathFormat      string `json:"pathFormat,omitempty"`
	LinesStartAt1   bool   `json:"linesStartAt1,omitempty"`
	ColumnsStartAt1 bool   `json:"columnsStartAt1,omitempty"`
}

// LaunchRequestArgs is the arguments for the launch request.
type LaunchRequestArgs struct {
	Program string            `json:"program"`
	Mode    string            `json:"mode,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// SetBreakpointsRequestArgs is the arguments for the setBreakpoints request.
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

// StackTraceRequestArgs is the arguments for the stackTrace request.
type StackTraceRequestArgs struct {
	ThreadId   int `json:"threadId"`
	StartFrame int `json:"startFrame,omitempty"`
	Levels     int `json:"levels,omitempty"`
}

// StackTraceResponseBody is the response body for stackTrace.
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

// ThreadsResponseBody is the response body for threads.
type ThreadsResponseBody struct {
	Threads []Thread `json:"threads"`
}

// Thread represents a thread.
type Thread struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

// ScopesRequestArgs is the arguments for the scopes request.
type ScopesRequestArgs struct {
	FrameId int `json:"frameId"`
}

// ScopesResponseBody is the response body for scopes.
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

// VariablesRequestArgs is the arguments for the variables request.
type VariablesRequestArgs struct {
	VariablesReference int `json:"variablesReference"`
}

// VariablesResponseBody is the response body for variables.
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

// ContinueRequestArgs is the arguments for the continue request.
type ContinueRequestArgs struct {
	ThreadId int `json:"threadId"`
}

// ContinueResponseBody is the response body for continue.
type ContinueResponseBody struct {
	AllThreadsContinued bool `json:"allThreadsContinued"`
}

// StoppedEventBody is the event body for stopped.
type StoppedEventBody struct {
	Reason            string `json:"reason"`
	Description       string `json:"description,omitempty"`
	ThreadId          int    `json:"threadId,omitempty"`
	AllThreadsStopped bool   `json:"allThreadsStopped,omitempty"`
}

// ExitedEventBody is the event body for exited.
type ExitedEventBody struct {
	ExitCode int `json:"exitCode"`
}

// OutputEventBody is the event body for output.
type OutputEventBody struct {
	Category string `json:"category,omitempty"`
	Output   string `json:"output"`
	Source   Source `json:"source,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
}
