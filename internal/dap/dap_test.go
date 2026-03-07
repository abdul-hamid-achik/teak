package dap

import (
	"testing"
)

// TestDebugConfig tests DebugConfig struct
func TestDebugConfig(t *testing.T) {
	config := DebugConfig{
		Type:    "go",
		Command: "dlv",
		Args:    []string{"dap"},
		Program: "/test/main.go",
		Cwd:     "/test",
		Env:     map[string]string{"GOOS": "linux"},
	}

	if config.Type != "go" {
		t.Errorf("Expected Type 'go', got %q", config.Type)
	}
	if config.Command != "dlv" {
		t.Errorf("Expected Command 'dlv', got %q", config.Command)
	}
	if config.Program != "/test/main.go" {
		t.Errorf("Expected Program '/test/main.go', got %q", config.Program)
	}
}

// TestDebugState tests DebugState constants
func TestDebugState(t *testing.T) {
	if StateInactive != 0 {
		t.Errorf("Expected StateInactive 0, got %d", StateInactive)
	}
	if StateRunning != 1 {
		t.Errorf("Expected StateRunning 1, got %d", StateRunning)
	}
	if StateStopped != 2 {
		t.Errorf("Expected StateStopped 2, got %d", StateStopped)
	}
}

// TestDebugStateString tests DebugState String method
func TestDebugStateString(t *testing.T) {
	tests := []struct {
		state  DebugState
		expect string
	}{
		{StateInactive, "inactive"},
		{StateRunning, "running"},
		{StateStopped, "stopped"},
		{99, "unknown"},
	}

	for _, tt := range tests {
		result := tt.state.String()
		if result != tt.expect {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, result, tt.expect)
		}
	}
}

// TestManagerCreation tests NewManager function
func TestManagerCreation(t *testing.T) {
	manager := NewManager("/test")

	if manager.rootDir != "/test" {
		t.Errorf("Expected rootDir '/test', got %q", manager.rootDir)
	}
	if manager.msgChan == nil {
		t.Error("Expected msgChan to be initialized")
	}
	if manager.state != StateInactive {
		t.Errorf("Expected state StateInactive, got %v", manager.state)
	}
	if manager.client != nil {
		t.Error("Expected client to be nil initially")
	}
}

// TestManagerCreationWithEmptyRoot tests NewManager with empty root
func TestManagerCreationWithEmptyRoot(t *testing.T) {
	manager := NewManager("")

	if manager.rootDir != "" {
		t.Errorf("Expected empty rootDir, got %q", manager.rootDir)
	}
}

// TestManagerInitialState tests initial manager state
func TestManagerInitialState(t *testing.T) {
	manager := NewManager("/test")

	if manager.State() != StateInactive {
		t.Errorf("Expected StateInactive, got %v", manager.State())
	}
}

// TestManagerStateMethod tests State method
func TestManagerStateMethod(t *testing.T) {
	manager := NewManager("/test")

	// Set state directly for testing
	manager.state = StateRunning
	if manager.State() != StateRunning {
		t.Errorf("Expected StateRunning, got %v", manager.State())
	}

	manager.state = StateStopped
	if manager.State() != StateStopped {
		t.Errorf("Expected StateStopped, got %v", manager.State())
	}
}

// TestManagerStopWithNilClient tests Stop with nil client
func TestManagerStopWithNilClient(t *testing.T) {
	manager := NewManager("/test")

	// Should not crash with nil client
	manager.Stop()

	if manager.state != StateInactive {
		t.Errorf("Expected StateInactive after Stop, got %v", manager.state)
	}
}

// TestManagerStartWithNilConfig tests Start with empty config
func TestManagerStartWithNilConfig(t *testing.T) {
	manager := NewManager("/test")

	config := DebugConfig{}
	err := manager.Start(config)

	// Should return error for empty command
	if err == nil {
		t.Error("Expected error for empty config")
	}
}

// TestManagerLaunchWithNilClient tests Launch with nil client
func TestManagerLaunchWithNilClient(t *testing.T) {
	manager := NewManager("/test")

	err := manager.Launch()

	// Should return error for nil client
	if err == nil {
		t.Error("Expected error for nil client")
	}
}

// TestManagerContinueWithNilClient tests Continue with nil client
func TestManagerContinueWithNilClient(t *testing.T) {
	manager := NewManager("/test")

	err := manager.Continue()

	// Should return error for nil client
	if err == nil {
		t.Error("Expected error for nil client")
	}
}

// TestManagerNextWithNilClient tests Next with nil client
func TestManagerNextWithNilClient(t *testing.T) {
	manager := NewManager("/test")

	err := manager.Next()

	// Should return error for nil client
	if err == nil {
		t.Error("Expected error for nil client")
	}
}

// TestManagerStepInWithNilClient tests StepIn with nil client
func TestManagerStepInWithNilClient(t *testing.T) {
	manager := NewManager("/test")

	err := manager.StepIn()

	// Should return error for nil client
	if err == nil {
		t.Error("Expected error for nil client")
	}
}

// TestManagerStepOutWithNilClient tests StepOut with nil client
func TestManagerStepOutWithNilClient(t *testing.T) {
	manager := NewManager("/test")

	err := manager.StepOut()

	// Should return error for nil client
	if err == nil {
		t.Error("Expected error for nil client")
	}
}

// TestManagerGetStackTraceWithNilClient tests GetStackTrace with nil client
func TestManagerGetStackTraceWithNilClient(t *testing.T) {
	manager := NewManager("/test")

	frames, err := manager.GetStackTrace()

	// Should return error for nil client
	if err == nil {
		t.Error("Expected error for nil client")
	}
	if frames != nil {
		t.Error("Expected nil frames for nil client")
	}
}

// TestManagerGetScopesWithNilClient tests GetScopes with nil client
func TestManagerGetScopesWithNilClient(t *testing.T) {
	manager := NewManager("/test")

	scopes, err := manager.GetScopes(0)

	// Should return error for nil client
	if err == nil {
		t.Error("Expected error for nil client")
	}
	if scopes != nil {
		t.Error("Expected nil scopes for nil client")
	}
}

// TestManagerGetVariablesWithNilClient tests GetVariables with nil client
func TestManagerGetVariablesWithNilClient(t *testing.T) {
	manager := NewManager("/test")

	vars, err := manager.GetVariables(0)

	// Should return error for nil client
	if err == nil {
		t.Error("Expected error for nil client")
	}
	if vars != nil {
		t.Error("Expected nil variables for nil client")
	}
}

// TestManagerSetBreakpointsWithNilClient tests SetBreakpoints with nil client
func TestManagerSetBreakpointsWithNilClient(t *testing.T) {
	manager := NewManager("/test")

	bps, err := manager.SetBreakpoints("/test.go", []int{10, 20})

	// Should return error for nil client
	if err == nil {
		t.Error("Expected error for nil client")
	}
	if bps != nil {
		t.Error("Expected nil breakpoints for nil client")
	}
}

// TestManagerIsRunningWithNilClient tests IsRunning with nil client
func TestManagerIsRunningWithNilClient(t *testing.T) {
	manager := NewManager("/test")

	if manager.IsRunning() {
		t.Error("Expected IsRunning to be false for nil client")
	}
}

// TestManagerMsgChan tests MsgChan method
func TestManagerMsgChan(t *testing.T) {
	manager := NewManager("/test")

	ch := manager.MsgChan()
	if ch == nil {
		t.Error("Expected non-nil message channel")
	}
}

// TestDebugConfigCopy tests DebugConfig copy behavior
func TestDebugConfigCopy(t *testing.T) {
	original := DebugConfig{
		Type:    "go",
		Command: "dlv",
		Program: "/test.go",
		Env:     map[string]string{"KEY": "value"},
	}

	// Copy by value
	copy := original
	copy.Type = "node"

	if original.Type != "go" {
		t.Error("Expected original to be unchanged")
	}
	if copy.Type != "node" {
		t.Errorf("Expected copy to be modified, got %q", copy.Type)
	}
}

// TestDebugConfigWithAllFields tests DebugConfig with all fields set
func TestDebugConfigWithAllFields(t *testing.T) {
	config := DebugConfig{
		Type:    "go",
		Command: "dlv",
		Args:    []string{"dap", "--log"},
		Program: "/test/main.go",
		Cwd:     "/test",
		Env: map[string]string{
			"GOOS":   "linux",
			"GOARCH": "amd64",
		},
	}

	if len(config.Args) != 2 {
		t.Errorf("Expected 2 args, got %d", len(config.Args))
	}
	if len(config.Env) != 2 {
		t.Errorf("Expected 2 env vars, got %d", len(config.Env))
	}
}

// TestDebugConfigWithEmptyFields tests DebugConfig with empty fields
func TestDebugConfigWithEmptyFields(t *testing.T) {
	config := DebugConfig{}

	if config.Type != "" {
		t.Errorf("Expected empty Type, got %q", config.Type)
	}
	if config.Command != "" {
		t.Errorf("Expected empty Command, got %q", config.Command)
	}
	if config.Program != "" {
		t.Errorf("Expected empty Program, got %q", config.Program)
	}
	if config.Cwd != "" {
		t.Errorf("Expected empty Cwd, got %q", config.Cwd)
	}
	if config.Args != nil {
		t.Error("Expected nil Args")
	}
	if config.Env != nil {
		t.Error("Expected nil Env")
	}
}

// TestSourceStruct tests Source struct
func TestSourceStruct(t *testing.T) {
	source := Source{
		Name: "main.go",
		Path: "/test/main.go",
	}

	if source.Name != "main.go" {
		t.Errorf("Expected Name 'main.go', got %q", source.Name)
	}
	if source.Path != "/test/main.go" {
		t.Errorf("Expected Path '/test/main.go', got %q", source.Path)
	}
}

// TestSourceWithEmptyFields tests Source with empty fields
func TestSourceWithEmptyFields(t *testing.T) {
	source := Source{}

	if source.Name != "" {
		t.Errorf("Expected empty Name, got %q", source.Name)
	}
	if source.Path != "" {
		t.Errorf("Expected empty Path, got %q", source.Path)
	}
}

// TestSourceBreakpointStruct tests SourceBreakpoint struct
func TestSourceBreakpointStruct(t *testing.T) {
	bp := SourceBreakpoint{
		Line:   10,
		Column: 5,
	}

	if bp.Line != 10 {
		t.Errorf("Expected Line 10, got %d", bp.Line)
	}
	if bp.Column != 5 {
		t.Errorf("Expected Column 5, got %d", bp.Column)
	}
}

// TestBreakpointStruct tests Breakpoint struct
func TestBreakpointStruct(t *testing.T) {
	bp := Breakpoint{
		Verified: true,
		Message:  "breakpoint set",
		Source:   Source{Name: "main.go", Path: "/test/main.go"},
		Line:     10,
		Column:   5,
	}

	if !bp.Verified {
		t.Error("Expected Verified to be true")
	}
	if bp.Line != 10 {
		t.Errorf("Expected Line 10, got %d", bp.Line)
	}
}

// TestStackFrameStruct tests StackFrame struct
func TestStackFrameStruct(t *testing.T) {
	frame := StackFrame{
		Id:     1,
		Name:   "main",
		Source: Source{Name: "main.go", Path: "/test/main.go"},
		Line:   10,
		Column: 5,
	}

	if frame.Id != 1 {
		t.Errorf("Expected Id 1, got %d", frame.Id)
	}
	if frame.Name != "main" {
		t.Errorf("Expected Name 'main', got %q", frame.Name)
	}
	if frame.Line != 10 {
		t.Errorf("Expected Line 10, got %d", frame.Line)
	}
}

// TestThreadStruct tests Thread struct
func TestThreadStruct(t *testing.T) {
	thread := Thread{
		Id:   1,
		Name: "main thread",
	}

	if thread.Id != 1 {
		t.Errorf("Expected Id 1, got %d", thread.Id)
	}
	if thread.Name != "main thread" {
		t.Errorf("Expected Name 'main thread', got %q", thread.Name)
	}
}

// TestScopeStruct tests Scope struct
func TestScopeStruct(t *testing.T) {
	scope := Scope{
		Name:               "Locals",
		PresentationHint:   "locals",
		VariablesReference: 1,
		Expensive:          false,
	}

	if scope.Name != "Locals" {
		t.Errorf("Expected Name 'Locals', got %q", scope.Name)
	}
	if scope.VariablesReference != 1 {
		t.Errorf("Expected VariablesReference 1, got %d", scope.VariablesReference)
	}
}

// TestVariableStruct tests Variable struct
func TestVariableStruct(t *testing.T) {
	variable := Variable{
		Name:               "x",
		Value:              "42",
		Type:               "int",
		VariablesReference: 0,
	}

	if variable.Name != "x" {
		t.Errorf("Expected Name 'x', got %q", variable.Name)
	}
	if variable.Value != "42" {
		t.Errorf("Expected Value '42', got %q", variable.Value)
	}
}

// TestInitializeRequestArgs tests InitializeRequestArgs struct
func TestInitializeRequestArgs(t *testing.T) {
	args := InitializeRequestArgs{
		AdapterID:       "go",
		PathFormat:      "path",
		LinesStartAt1:   true,
		ColumnsStartAt1: true,
	}

	if args.AdapterID != "go" {
		t.Errorf("Expected AdapterID 'go', got %q", args.AdapterID)
	}
	if !args.LinesStartAt1 {
		t.Error("Expected LinesStartAt1 to be true")
	}
}

// TestLaunchRequestArgs tests LaunchRequestArgs struct
func TestLaunchRequestArgs(t *testing.T) {
	args := LaunchRequestArgs{
		Program: "/test/main.go",
		Mode:    "debug",
		Args:    []string{"arg1", "arg2"},
		Cwd:     "/test",
		Env:     map[string]string{"KEY": "value"},
	}

	if args.Program != "/test/main.go" {
		t.Errorf("Expected Program '/test/main.go', got %q", args.Program)
	}
	if len(args.Args) != 2 {
		t.Errorf("Expected 2 args, got %d", len(args.Args))
	}
}

// TestSetBreakpointsRequestArgs tests SetBreakpointsRequestArgs struct
func TestSetBreakpointsRequestArgs(t *testing.T) {
	args := SetBreakpointsRequestArgs{
		Source: Source{Name: "main.go", Path: "/test/main.go"},
		Breakpoints: []SourceBreakpoint{
			{Line: 10, Column: 5},
			{Line: 20, Column: 10},
		},
	}

	if len(args.Breakpoints) != 2 {
		t.Errorf("Expected 2 breakpoints, got %d", len(args.Breakpoints))
	}
}

// TestStackTraceRequestArgs tests StackTraceRequestArgs struct
func TestStackTraceRequestArgs(t *testing.T) {
	args := StackTraceRequestArgs{
		ThreadId:   1,
		StartFrame: 0,
		Levels:     10,
	}

	if args.ThreadId != 1 {
		t.Errorf("Expected ThreadId 1, got %d", args.ThreadId)
	}
	if args.Levels != 10 {
		t.Errorf("Expected Levels 10, got %d", args.Levels)
	}
}

// TestStackTraceResponseBody tests StackTraceResponseBody struct
func TestStackTraceResponseBody(t *testing.T) {
	body := StackTraceResponseBody{
		StackFrames: []StackFrame{
			{Id: 1, Name: "main", Line: 10},
			{Id: 2, Name: "test", Line: 20},
		},
		TotalFrames: 2,
	}

	if len(body.StackFrames) != 2 {
		t.Errorf("Expected 2 stack frames, got %d", len(body.StackFrames))
	}
	if body.TotalFrames != 2 {
		t.Errorf("Expected TotalFrames 2, got %d", body.TotalFrames)
	}
}

// TestThreadsResponseBody tests ThreadsResponseBody struct
func TestThreadsResponseBody(t *testing.T) {
	body := ThreadsResponseBody{
		Threads: []Thread{
			{Id: 1, Name: "main"},
			{Id: 2, Name: "worker"},
		},
	}

	if len(body.Threads) != 2 {
		t.Errorf("Expected 2 threads, got %d", len(body.Threads))
	}
}

// TestErrorResponseStruct tests ErrorResponse struct
func TestErrorResponseStruct(t *testing.T) {
	err := ErrorResponse{
		Id:                 1,
		Format:             "error format",
		Message:            "error message",
		SendTelemetry:      false,
		ShowUser:           true,
		VariablesReference: 0,
	}

	if err.Id != 1 {
		t.Errorf("Expected Id 1, got %d", err.Id)
	}
	if err.Message != "error message" {
		t.Errorf("Expected Message 'error message', got %q", err.Message)
	}
	if !err.ShowUser {
		t.Error("Expected ShowUser to be true")
	}
}

// TestRequestStruct tests Request struct
func TestRequestStruct(t *testing.T) {
	req := Request{
		Seq:       1,
		Type:      "request",
		Command:   "initialize",
		Arguments: InitializeRequestArgs{AdapterID: "go"},
	}

	if req.Seq != 1 {
		t.Errorf("Expected Seq 1, got %d", req.Seq)
	}
	if req.Command != "initialize" {
		t.Errorf("Expected Command 'initialize', got %q", req.Command)
	}
}

// TestEventStruct tests Event struct
func TestEventStruct(t *testing.T) {
	event := Event{
		Seq:   1,
		Type:  "event",
		Event: "stopped",
		Body:  map[string]string{"reason": "breakpoint"},
	}

	if event.Seq != 1 {
		t.Errorf("Expected Seq 1, got %d", event.Seq)
	}
	if event.Event != "stopped" {
		t.Errorf("Expected Event 'stopped', got %q", event.Event)
	}
}

// TestResponseStruct tests Response struct
func TestResponseStruct(t *testing.T) {
	resp := Response{
		Seq:        1,
		Type:       "response",
		RequestSeq: 1,
		Command:    "initialize",
		Success:    true,
		Message:    "",
		Body:       []byte("{}"),
	}

	if !resp.Success {
		t.Error("Expected Success to be true")
	}
	if resp.Command != "initialize" {
		t.Errorf("Expected Command 'initialize', got %q", resp.Command)
	}
}

// TestManagerStateTransitions tests manager state transitions
func TestManagerStateTransitions(t *testing.T) {
	manager := NewManager("/test")

	// Initial state
	if manager.State() != StateInactive {
		t.Errorf("Expected StateInactive, got %v", manager.State())
	}

	// Manually set states for testing
	manager.state = StateRunning
	if manager.State() != StateRunning {
		t.Errorf("Expected StateRunning, got %v", manager.State())
	}

	manager.state = StateStopped
	if manager.State() != StateStopped {
		t.Errorf("Expected StateStopped, got %v", manager.State())
	}

	manager.state = StateInactive
	if manager.State() != StateInactive {
		t.Errorf("Expected StateInactive, got %v", manager.State())
	}
}

// TestManagerMultipleStopCalls tests multiple Stop calls
func TestManagerMultipleStopCalls(t *testing.T) {
	manager := NewManager("/test")

	// Should not crash with multiple Stop calls
	manager.Stop()
	manager.Stop()
	manager.Stop()

	if manager.state != StateInactive {
		t.Errorf("Expected StateInactive, got %v", manager.state)
	}
}

// TestManagerWithDifferentRootDirs tests Manager with different root directories
func TestManagerWithDifferentRootDirs(t *testing.T) {
	roots := []string{"/test", "/project", "", ".", "~/home"}

	for _, root := range roots {
		manager := NewManager(root)
		if manager.rootDir != root {
			t.Errorf("Expected rootDir %q, got %q", root, manager.rootDir)
		}
	}
}

// TestDebugConfigWithSpecialCharacters tests DebugConfig with special characters
func TestDebugConfigWithSpecialCharacters(t *testing.T) {
	config := DebugConfig{
		Type:    "go",
		Command: "dlv",
		Program: "/path/with spaces/main.go",
		Cwd:     "/path/with spaces",
	}

	if config.Program != "/path/with spaces/main.go" {
		t.Errorf("Expected Program with spaces, got %q", config.Program)
	}
}

// TestDebugConfigWithUnicodeCharacters tests DebugConfig with unicode characters
func TestDebugConfigWithUnicodeCharacters(t *testing.T) {
	config := DebugConfig{
		Type:    "go",
		Command: "dlv",
		Program: "/测试/main.go",
		Cwd:     "/测试",
	}

	if config.Program != "/测试/main.go" {
		t.Errorf("Expected Program with unicode, got %q", config.Program)
	}
}

// TestSourceCopy tests Source copy behavior
func TestSourceCopy(t *testing.T) {
	original := Source{Name: "main.go", Path: "/test/main.go"}
	copy := original
	copy.Name = "modified.go"

	if original.Name != "main.go" {
		t.Error("Expected original to be unchanged")
	}
	if copy.Name != "modified.go" {
		t.Errorf("Expected copy to be modified, got %q", copy.Name)
	}
}

// TestBreakpointCopy tests Breakpoint copy behavior
func TestBreakpointCopy(t *testing.T) {
	original := Breakpoint{Verified: true, Line: 10}
	copy := original
	copy.Verified = false
	copy.Line = 20

	if !original.Verified {
		t.Error("Expected original.Verified to be true")
	}
	if original.Line != 10 {
		t.Errorf("Expected original.Line 10, got %d", original.Line)
	}
}

// TestStackFrameCopy tests StackFrame copy behavior
func TestStackFrameCopy(t *testing.T) {
	original := StackFrame{Id: 1, Name: "main", Line: 10}
	copy := original
	copy.Id = 2
	copy.Name = "modified"
	copy.Line = 20

	if original.Id != 1 {
		t.Error("Expected original.Id to be 1")
	}
	if original.Name != "main" {
		t.Errorf("Expected original.Name 'main', got %q", original.Name)
	}
	if original.Line != 10 {
		t.Errorf("Expected original.Line 10, got %d", original.Line)
	}
}

// TestThreadCopy tests Thread copy behavior
func TestThreadCopy(t *testing.T) {
	original := Thread{Id: 1, Name: "main"}
	copy := original
	copy.Id = 2
	copy.Name = "modified"

	if original.Id != 1 {
		t.Error("Expected original.Id to be 1")
	}
	if original.Name != "main" {
		t.Errorf("Expected original.Name 'main', got %q", original.Name)
	}
}

// TestVariableCopy tests Variable copy behavior
func TestVariableCopy(t *testing.T) {
	original := Variable{Name: "x", Value: "42"}
	copy := original
	copy.Name = "y"
	copy.Value = "100"

	if original.Name != "x" {
		t.Error("Expected original.Name to be 'x'")
	}
	if original.Value != "42" {
		t.Errorf("Expected original.Value '42', got %q", original.Value)
	}
}

// TestScopeCopy tests Scope copy behavior
func TestScopeCopy(t *testing.T) {
	original := Scope{Name: "Locals", VariablesReference: 1}
	copy := original
	copy.Name = "modified"
	copy.VariablesReference = 2

	if original.Name != "Locals" {
		t.Error("Expected original.Name to be 'Locals'")
	}
	if original.VariablesReference != 1 {
		t.Errorf("Expected original.VariablesReference 1, got %d", original.VariablesReference)
	}
}

// TestRequestCopy tests Request copy behavior
func TestRequestCopy(t *testing.T) {
	original := Request{Seq: 1, Command: "initialize"}
	copy := original
	copy.Seq = 2
	copy.Command = "modified"

	if original.Seq != 1 {
		t.Error("Expected original.Seq to be 1")
	}
	if original.Command != "initialize" {
		t.Errorf("Expected original.Command 'initialize', got %q", original.Command)
	}
}

// TestEventCopy tests Event copy behavior
func TestEventCopy(t *testing.T) {
	original := Event{Seq: 1, Event: "stopped"}
	copy := original
	copy.Seq = 2
	copy.Event = "modified"

	if original.Seq != 1 {
		t.Error("Expected original.Seq to be 1")
	}
	if original.Event != "stopped" {
		t.Errorf("Expected original.Event 'stopped', got %q", original.Event)
	}
}

// TestResponseCopy tests Response copy behavior
func TestResponseCopy(t *testing.T) {
	original := Response{Seq: 1, Success: true, Command: "initialize"}
	copy := original
	copy.Seq = 2
	copy.Success = false
	copy.Command = "modified"

	if original.Seq != 1 {
		t.Error("Expected original.Seq to be 1")
	}
	if !original.Success {
		t.Error("Expected original.Success to be true")
	}
	if original.Command != "initialize" {
		t.Errorf("Expected original.Command 'initialize', got %q", original.Command)
	}
}

// TestErrorResponseCopy tests ErrorResponse copy behavior
func TestErrorResponseCopy(t *testing.T) {
	original := ErrorResponse{Id: 1, Message: "error"}
	copy := original
	copy.Id = 2
	copy.Message = "modified"

	if original.Id != 1 {
		t.Error("Expected original.Id to be 1")
	}
	if original.Message != "error" {
		t.Errorf("Expected original.Message 'error', got %q", original.Message)
	}
}

// TestInitializeRequestArgsCopy tests InitializeRequestArgs copy behavior
func TestInitializeRequestArgsCopy(t *testing.T) {
	original := InitializeRequestArgs{AdapterID: "go", LinesStartAt1: true}
	copy := original
	copy.AdapterID = "node"
	copy.LinesStartAt1 = false

	if original.AdapterID != "go" {
		t.Error("Expected original.AdapterID to be 'go'")
	}
	if !original.LinesStartAt1 {
		t.Error("Expected original.LinesStartAt1 to be true")
	}
}

// TestLaunchRequestArgsCopy tests LaunchRequestArgs copy behavior
func TestLaunchRequestArgsCopy(t *testing.T) {
	original := LaunchRequestArgs{Program: "/test.go", Mode: "debug"}
	copy := original
	copy.Program = "/modified.go"
	copy.Mode = "modified"

	if original.Program != "/test.go" {
		t.Error("Expected original.Program to be '/test.go'")
	}
	if original.Mode != "debug" {
		t.Errorf("Expected original.Mode 'debug', got %q", original.Mode)
	}
}

// TestSetBreakpointsRequestArgsCopy tests SetBreakpointsRequestArgs copy behavior
func TestSetBreakpointsRequestArgsCopy(t *testing.T) {
	original := SetBreakpointsRequestArgs{
		Source: Source{Name: "main.go"},
		Breakpoints: []SourceBreakpoint{{Line: 10}},
	}
	copy := original
	copy.Breakpoints = append(copy.Breakpoints, SourceBreakpoint{Line: 20})

	if len(original.Breakpoints) != 1 {
		t.Errorf("Expected original.Breakpoints length 1, got %d", len(original.Breakpoints))
	}
	if len(copy.Breakpoints) != 2 {
		t.Errorf("Expected copy.Breakpoints length 2, got %d", len(copy.Breakpoints))
	}
}

// TestStackTraceRequestArgsCopy tests StackTraceRequestArgs copy behavior
func TestStackTraceRequestArgsCopy(t *testing.T) {
	original := StackTraceRequestArgs{ThreadId: 1, Levels: 10}
	copy := original
	copy.ThreadId = 2
	copy.Levels = 20

	if original.ThreadId != 1 {
		t.Error("Expected original.ThreadId to be 1")
	}
	if original.Levels != 10 {
		t.Errorf("Expected original.Levels 10, got %d", original.Levels)
	}
}

// TestStackTraceResponseBodyCopy tests StackTraceResponseBody copy behavior
func TestStackTraceResponseBodyCopy(t *testing.T) {
	original := StackTraceResponseBody{
		StackFrames: []StackFrame{{Id: 1}},
		TotalFrames: 1,
	}
	copy := original
	copy.StackFrames = append(copy.StackFrames, StackFrame{Id: 2})
	copy.TotalFrames = 2

	if len(original.StackFrames) != 1 {
		t.Errorf("Expected original.StackFrames length 1, got %d", len(original.StackFrames))
	}
	if original.TotalFrames != 1 {
		t.Errorf("Expected original.TotalFrames 1, got %d", original.TotalFrames)
	}
}

// TestThreadsResponseBodyCopy tests ThreadsResponseBody copy behavior
func TestThreadsResponseBodyCopy(t *testing.T) {
	original := ThreadsResponseBody{
		Threads: []Thread{{Id: 1}},
	}
	copy := original
	copy.Threads = append(copy.Threads, Thread{Id: 2})

	if len(original.Threads) != 1 {
		t.Errorf("Expected original.Threads length 1, got %d", len(original.Threads))
	}
	if len(copy.Threads) != 2 {
		t.Errorf("Expected copy.Threads length 2, got %d", len(copy.Threads))
	}
}

// TestManagerConfigField tests Manager config field
func TestManagerConfigField(t *testing.T) {
	manager := NewManager("/test")

	config := DebugConfig{
		Type:    "go",
		Command: "dlv",
		Program: "/test.go",
	}

	manager.config = config

	if manager.config.Type != "go" {
		t.Errorf("Expected config.Type 'go', got %q", manager.config.Type)
	}
	if manager.config.Program != "/test.go" {
		t.Errorf("Expected config.Program '/test.go', got %q", manager.config.Program)
	}
}

// TestManagerClientField tests Manager client field
func TestManagerClientField(t *testing.T) {
	manager := NewManager("/test")

	if manager.client != nil {
		t.Error("Expected client to be nil initially")
	}
}

// TestAllDebugStates tests all DebugState values
func TestAllDebugStates(t *testing.T) {
	states := []DebugState{StateInactive, StateRunning, StateStopped}
	expectedStrings := []string{"inactive", "running", "stopped"}

	for i, state := range states {
		if state.String() != expectedStrings[i] {
			t.Errorf("State(%d).String() = %q, want %q", state, state.String(), expectedStrings[i])
		}
	}
}

// TestDebugStateUnknownValue tests DebugState with unknown value
func TestDebugStateUnknownValue(t *testing.T) {
	state := DebugState(999)
	result := state.String()
	if result != "unknown" {
		t.Errorf("State(999).String() = %q, want 'unknown'", result)
	}
}

// TestSourceBreakpointWithZeroColumn tests SourceBreakpoint with zero column
func TestSourceBreakpointWithZeroColumn(t *testing.T) {
	bp := SourceBreakpoint{Line: 10, Column: 0}

	if bp.Line != 10 {
		t.Errorf("Expected Line 10, got %d", bp.Line)
	}
	if bp.Column != 0 {
		t.Errorf("Expected Column 0, got %d", bp.Column)
	}
}

// TestBreakpointWithEmptyMessage tests Breakpoint with empty message
func TestBreakpointWithEmptyMessage(t *testing.T) {
	bp := Breakpoint{Verified: true, Message: ""}

	if !bp.Verified {
		t.Error("Expected Verified to be true")
	}
	if bp.Message != "" {
		t.Errorf("Expected empty Message, got %q", bp.Message)
	}
}

// TestStackFrameWithEmptySource tests StackFrame with empty source
func TestStackFrameWithEmptySource(t *testing.T) {
	frame := StackFrame{
		Id:     1,
		Name:   "main",
		Source: Source{},
		Line:   10,
	}

	if frame.Source.Name != "" {
		t.Errorf("Expected empty Source.Name, got %q", frame.Source.Name)
	}
	if frame.Source.Path != "" {
		t.Errorf("Expected empty Source.Path, got %q", frame.Source.Path)
	}
}

// TestThreadWithEmptyName tests Thread with empty name
func TestThreadWithEmptyName(t *testing.T) {
	thread := Thread{Id: 1, Name: ""}

	if thread.Id != 1 {
		t.Errorf("Expected Id 1, got %d", thread.Id)
	}
	if thread.Name != "" {
		t.Errorf("Expected empty Name, got %q", thread.Name)
	}
}

// TestScopeWithEmptyFields tests Scope with empty fields
func TestScopeWithEmptyFields(t *testing.T) {
	scope := Scope{}

	if scope.Name != "" {
		t.Errorf("Expected empty Name, got %q", scope.Name)
	}
	if scope.VariablesReference != 0 {
		t.Errorf("Expected VariablesReference 0, got %d", scope.VariablesReference)
	}
	if scope.Expensive {
		t.Error("Expected Expensive to be false")
	}
}

// TestVariableWithEmptyFields tests Variable with empty fields
func TestVariableWithEmptyFields(t *testing.T) {
	variable := Variable{}

	if variable.Name != "" {
		t.Errorf("Expected empty Name, got %q", variable.Name)
	}
	if variable.Value != "" {
		t.Errorf("Expected empty Value, got %q", variable.Value)
	}
	if variable.Type != "" {
		t.Errorf("Expected empty Type, got %q", variable.Type)
	}
}

// TestRequestWithEmptyFields tests Request with empty fields
func TestRequestWithEmptyFields(t *testing.T) {
	req := Request{}

	if req.Seq != 0 {
		t.Errorf("Expected Seq 0, got %d", req.Seq)
	}
	if req.Type != "" {
		t.Errorf("Expected empty Type, got %q", req.Type)
	}
	if req.Command != "" {
		t.Errorf("Expected empty Command, got %q", req.Command)
	}
}

// TestEventWithEmptyFields tests Event with empty fields
func TestEventWithEmptyFields(t *testing.T) {
	event := Event{}

	if event.Seq != 0 {
		t.Errorf("Expected Seq 0, got %d", event.Seq)
	}
	if event.Type != "" {
		t.Errorf("Expected empty Type, got %q", event.Type)
	}
	if event.Event != "" {
		t.Errorf("Expected empty Event, got %q", event.Event)
	}
}

// TestResponseWithEmptyFields tests Response with empty fields
func TestResponseWithEmptyFields(t *testing.T) {
	resp := Response{}

	if resp.Seq != 0 {
		t.Errorf("Expected Seq 0, got %d", resp.Seq)
	}
	if resp.Type != "" {
		t.Errorf("Expected empty Type, got %q", resp.Type)
	}
	if resp.Command != "" {
		t.Errorf("Expected empty Command, got %q", resp.Command)
	}
	if resp.Success {
		t.Error("Expected Success to be false")
	}
}

// TestErrorResponseWithEmptyFields tests ErrorResponse with empty fields
func TestErrorResponseWithEmptyFields(t *testing.T) {
	err := ErrorResponse{}

	if err.Id != 0 {
		t.Errorf("Expected Id 0, got %d", err.Id)
	}
	if err.Format != "" {
		t.Errorf("Expected empty Format, got %q", err.Format)
	}
	if err.Message != "" {
		t.Errorf("Expected empty Message, got %q", err.Message)
	}
	if err.SendTelemetry {
		t.Error("Expected SendTelemetry to be false")
	}
	if err.ShowUser {
		t.Error("Expected ShowUser to be false")
	}
	if err.VariablesReference != 0 {
		t.Errorf("Expected VariablesReference 0, got %d", err.VariablesReference)
	}
}

// TestInitializeRequestArgsWithEmptyFields tests InitializeRequestArgs with empty fields
func TestInitializeRequestArgsWithEmptyFields(t *testing.T) {
	args := InitializeRequestArgs{}

	if args.AdapterID != "" {
		t.Errorf("Expected empty AdapterID, got %q", args.AdapterID)
	}
	if args.PathFormat != "" {
		t.Errorf("Expected empty PathFormat, got %q", args.PathFormat)
	}
	if args.LinesStartAt1 {
		t.Error("Expected LinesStartAt1 to be false")
	}
	if args.ColumnsStartAt1 {
		t.Error("Expected ColumnsStartAt1 to be false")
	}
}

// TestLaunchRequestArgsWithEmptyFields tests LaunchRequestArgs with empty fields
func TestLaunchRequestArgsWithEmptyFields(t *testing.T) {
	args := LaunchRequestArgs{}

	if args.Program != "" {
		t.Errorf("Expected empty Program, got %q", args.Program)
	}
	if args.Mode != "" {
		t.Errorf("Expected empty Mode, got %q", args.Mode)
	}
	if args.Args != nil {
		t.Error("Expected nil Args")
	}
	if args.Cwd != "" {
		t.Errorf("Expected empty Cwd, got %q", args.Cwd)
	}
	if args.Env != nil {
		t.Error("Expected nil Env")
	}
}

// TestSetBreakpointsRequestArgsWithEmptyFields tests SetBreakpointsRequestArgs with empty fields
func TestSetBreakpointsRequestArgsWithEmptyFields(t *testing.T) {
	args := SetBreakpointsRequestArgs{}

	if args.Source.Name != "" {
		t.Errorf("Expected empty Source.Name, got %q", args.Source.Name)
	}
	if args.Source.Path != "" {
		t.Errorf("Expected empty Source.Path, got %q", args.Source.Path)
	}
	if args.Breakpoints != nil {
		t.Error("Expected nil Breakpoints")
	}
}

// TestStackTraceRequestArgsWithEmptyFields tests StackTraceRequestArgs with empty fields
func TestStackTraceRequestArgsWithEmptyFields(t *testing.T) {
	args := StackTraceRequestArgs{}

	if args.ThreadId != 0 {
		t.Errorf("Expected ThreadId 0, got %d", args.ThreadId)
	}
	if args.StartFrame != 0 {
		t.Errorf("Expected StartFrame 0, got %d", args.StartFrame)
	}
	if args.Levels != 0 {
		t.Errorf("Expected Levels 0, got %d", args.Levels)
	}
}

// TestStackTraceResponseBodyWithEmptyFields tests StackTraceResponseBody with empty fields
func TestStackTraceResponseBodyWithEmptyFields(t *testing.T) {
	body := StackTraceResponseBody{}

	if body.StackFrames != nil {
		t.Error("Expected nil StackFrames")
	}
	if body.TotalFrames != 0 {
		t.Errorf("Expected TotalFrames 0, got %d", body.TotalFrames)
	}
}

// TestThreadsResponseBodyWithEmptyFields tests ThreadsResponseBody with empty fields
func TestThreadsResponseBodyWithEmptyFields(t *testing.T) {
	body := ThreadsResponseBody{}

	if body.Threads != nil {
		t.Error("Expected nil Threads")
	}
}

// TestManagerMsgChanNotNil tests MsgChan returns non-nil channel
func TestManagerMsgChanNotNil(t *testing.T) {
	manager := NewManager("/test")

	ch := manager.MsgChan()
	if ch == nil {
		t.Error("Expected non-nil message channel")
	}
}


// TestDebugConfigEnvMapOperations tests DebugConfig Env map operations
func TestDebugConfigEnvMapOperations(t *testing.T) {
	config := DebugConfig{
		Env: make(map[string]string),
	}

	// Add env var
	config.Env["KEY"] = "value"
	if config.Env["KEY"] != "value" {
		t.Errorf("Expected Env['KEY'] = 'value', got %q", config.Env["KEY"])
	}

	// Remove env var
	delete(config.Env, "KEY")
	if _, ok := config.Env["KEY"]; ok {
		t.Error("Expected Env['KEY'] to be deleted")
	}
}

// TestDebugConfigArgsSliceOperations tests DebugConfig Args slice operations
func TestDebugConfigArgsSliceOperations(t *testing.T) {
	config := DebugConfig{
		Args: []string{"arg1", "arg2"},
	}

	// Append arg
	config.Args = append(config.Args, "arg3")
	if len(config.Args) != 3 {
		t.Errorf("Expected 3 args, got %d", len(config.Args))
	}

	// Access args
	if config.Args[0] != "arg1" {
		t.Errorf("Expected Args[0] = 'arg1', got %q", config.Args[0])
	}
}

// TestSourceBreakpointSliceOperations tests SourceBreakpoint slice operations
func TestSourceBreakpointSliceOperations(t *testing.T) {
	breakpoints := []SourceBreakpoint{
		{Line: 10, Column: 5},
		{Line: 20, Column: 10},
		{Line: 30, Column: 15},
	}

	if len(breakpoints) != 3 {
		t.Errorf("Expected 3 breakpoints, got %d", len(breakpoints))
	}

	// Access breakpoint
	if breakpoints[1].Line != 20 {
		t.Errorf("Expected breakpoints[1].Line = 20, got %d", breakpoints[1].Line)
	}
}

// TestStackFrameSliceOperations tests StackFrame slice operations
func TestStackFrameSliceOperations(t *testing.T) {
	frames := []StackFrame{
		{Id: 1, Name: "main", Line: 10},
		{Id: 2, Name: "test", Line: 20},
	}

	if len(frames) != 2 {
		t.Errorf("Expected 2 frames, got %d", len(frames))
	}

	// Access frame
	if frames[0].Name != "main" {
		t.Errorf("Expected frames[0].Name = 'main', got %q", frames[0].Name)
	}
}

// TestThreadSliceOperations tests Thread slice operations
func TestThreadSliceOperations(t *testing.T) {
	threads := []Thread{
		{Id: 1, Name: "main"},
		{Id: 2, Name: "worker"},
	}

	if len(threads) != 2 {
		t.Errorf("Expected 2 threads, got %d", len(threads))
	}

	// Access thread
	if threads[1].Name != "worker" {
		t.Errorf("Expected threads[1].Name = 'worker', got %q", threads[1].Name)
	}
}

// TestVariableSliceOperations tests Variable slice operations
func TestVariableSliceOperations(t *testing.T) {
	variables := []Variable{
		{Name: "x", Value: "42"},
		{Name: "y", Value: "100"},
	}

	if len(variables) != 2 {
		t.Errorf("Expected 2 variables, got %d", len(variables))
	}

	// Access variable
	if variables[0].Value != "42" {
		t.Errorf("Expected variables[0].Value = '42', got %q", variables[0].Value)
	}
}

// TestScopeSliceOperations tests Scope slice operations
func TestScopeSliceOperations(t *testing.T) {
	scopes := []Scope{
		{Name: "Locals", VariablesReference: 1},
		{Name: "Globals", VariablesReference: 2},
	}

	if len(scopes) != 2 {
		t.Errorf("Expected 2 scopes, got %d", len(scopes))
	}

	// Access scope
	if scopes[1].Name != "Globals" {
		t.Errorf("Expected scopes[1].Name = 'Globals', got %q", scopes[1].Name)
	}
}

// TestBreakpointSliceOperations tests Breakpoint slice operations
func TestBreakpointSliceOperations(t *testing.T) {
	breakpoints := []Breakpoint{
		{Verified: true, Line: 10},
		{Verified: false, Line: 20},
	}

	if len(breakpoints) != 2 {
		t.Errorf("Expected 2 breakpoints, got %d", len(breakpoints))
	}

	// Access breakpoint
	if !breakpoints[0].Verified {
		t.Error("Expected breakpoints[0].Verified to be true")
	}
}

// TestRequestSliceOperations tests Request slice operations
func TestRequestSliceOperations(t *testing.T) {
	requests := []Request{
		{Seq: 1, Command: "initialize"},
		{Seq: 2, Command: "launch"},
	}

	if len(requests) != 2 {
		t.Errorf("Expected 2 requests, got %d", len(requests))
	}

	// Access request
	if requests[1].Command != "launch" {
		t.Errorf("Expected requests[1].Command = 'launch', got %q", requests[1].Command)
	}
}

// TestEventSliceOperations tests Event slice operations
func TestEventSliceOperations(t *testing.T) {
	events := []Event{
		{Seq: 1, Event: "stopped"},
		{Seq: 2, Event: "continued"},
	}

	if len(events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(events))
	}

	// Access event
	if events[0].Event != "stopped" {
		t.Errorf("Expected events[0].Event = 'stopped', got %q", events[0].Event)
	}
}

// TestResponseSliceOperations tests Response slice operations
func TestResponseSliceOperations(t *testing.T) {
	responses := []Response{
		{Seq: 1, Success: true, Command: "initialize"},
		{Seq: 2, Success: false, Command: "launch"},
	}

	if len(responses) != 2 {
		t.Errorf("Expected 2 responses, got %d", len(responses))
	}

	// Access response
	if !responses[0].Success {
		t.Error("Expected responses[0].Success to be true")
	}
}

// TestAllProtocolTypesExist tests that all protocol types exist
func TestAllProtocolTypesExist(t *testing.T) {
	// Just verify we can create instances of all types
	_ = Request{}
	_ = Event{}
	_ = Response{}
	_ = ErrorResponse{}
	_ = InitializeRequestArgs{}
	_ = LaunchRequestArgs{}
	_ = SetBreakpointsRequestArgs{}
	_ = Source{}
	_ = SourceBreakpoint{}
	_ = Breakpoint{}
	_ = StackTraceRequestArgs{}
	_ = StackTraceResponseBody{}
	_ = StackFrame{}
	_ = ThreadsResponseBody{}
	_ = Thread{}
	_ = Scope{}
	_ = Variable{}
}

// TestAllDebugStatesExist tests that all debug states exist
func TestAllDebugStatesExist(t *testing.T) {
	// Just verify we can use all states
	_ = StateInactive
	_ = StateRunning
	_ = StateStopped
}

// TestManagerMethodsExist tests that all Manager methods exist
func TestManagerMethodsExist(t *testing.T) {
	manager := NewManager("/test")

	// Verify methods exist and are callable
	_ = manager.State()
	_ = manager.MsgChan()
	manager.Stop()
	_ = manager.IsRunning()

	// Methods that return errors (should return error for nil client)
	_ = manager.Launch()
	_ = manager.Continue()
	_ = manager.Next()
	_ = manager.StepIn()
	_ = manager.StepOut()
	_, _ = manager.GetStackTrace()
	_, _ = manager.GetScopes(0)
	_, _ = manager.GetVariables(0)
	_, _ = manager.SetBreakpoints("/test.go", []int{10})
}

// TestDebugConfigMethodsExist tests that DebugConfig is properly defined
func TestDebugConfigMethodsExist(t *testing.T) {
	config := DebugConfig{
		Type:    "go",
		Command: "dlv",
		Program: "/test.go",
	}

	// Verify fields are accessible
	_ = config.Type
	_ = config.Command
	_ = config.Program
	_ = config.Args
	_ = config.Cwd
	_ = config.Env
}

// TestAllTypesAreComparable tests that all types are comparable
func TestAllTypesAreComparable(t *testing.T) {
	// Test that basic types can be compared
	s1 := Source{Name: "test"}
	s2 := Source{Name: "test"}
	s3 := Source{Name: "different"}

	if s1 != s2 {
		t.Error("Expected equal sources to be equal")
	}
	if s1 == s3 {
		t.Error("Expected different sources to be not equal")
	}
}

// TestAllTypesHaveZeroValues tests that all types have proper zero values
func TestAllTypesHaveZeroValues(t *testing.T) {
	// Test zero values
	var source Source
	var bp SourceBreakpoint
	var breakpoint Breakpoint
	var frame StackFrame
	var thread Thread
	var scope Scope
	var variable Variable

	if source.Name != "" {
		t.Errorf("Expected zero Source.Name to be empty, got %q", source.Name)
	}
	if bp.Line != 0 {
		t.Errorf("Expected zero SourceBreakpoint.Line to be 0, got %d", bp.Line)
	}
	if breakpoint.Verified {
		t.Error("Expected zero Breakpoint.Verified to be false")
	}
	if frame.Id != 0 {
		t.Errorf("Expected zero StackFrame.Id to be 0, got %d", frame.Id)
	}
	if thread.Id != 0 {
		t.Errorf("Expected zero Thread.Id to be 0, got %d", thread.Id)
	}
	if scope.Name != "" {
		t.Errorf("Expected zero Scope.Name to be empty, got %q", scope.Name)
	}
	if variable.Name != "" {
		t.Errorf("Expected zero Variable.Name to be empty, got %q", variable.Name)
	}
}
