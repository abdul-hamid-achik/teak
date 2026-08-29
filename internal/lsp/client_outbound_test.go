package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockedCaptureWriteCloser struct {
	captureWriteCloser
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockedCaptureWriteCloser() *blockedCaptureWriteCloser {
	return &blockedCaptureWriteCloser{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

func (w *blockedCaptureWriteCloser) Write(data []byte) (int, error) {
	w.once.Do(func() { w.started <- struct{}{} })
	<-w.release
	return w.captureWriteCloser.Write(data)
}

func (w *blockedCaptureWriteCloser) unblock() {
	select {
	case <-w.release:
	default:
		close(w.release)
	}
}

func newOutboundTestClient(t *testing.T, stdin *blockedCaptureWriteCloser) *Client {
	t.Helper()
	client := &Client{
		stdin:       stdin,
		pending:     make(map[int]chan callResult),
		running:     true,
		initialized: true,
		processDone: make(chan struct{}),
	}
	client.startOutboundWorker()
	t.Cleanup(func() {
		stdin.unblock()
		close(client.processDone)
	})
	return client
}

func waitForBlockedWriter(t *testing.T, writer *blockedCaptureWriteCloser) {
	t.Helper()
	select {
	case <-writer.started:
	case <-time.After(time.Second):
		t.Fatal("outbound writer did not begin its blocked write")
	}
}

func capturedOutboundMessages(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	var messages []map[string]any
	for len(raw) > 0 {
		content, rest, state := parseFrame(raw)
		if state != frameReady {
			if state == frameIncomplete {
				break
			}
			t.Fatalf("outbound frame state = %v for %q", state, string(raw))
		}
		var message map[string]any
		if err := json.Unmarshal(content, &message); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
		raw = rest
	}
	return messages
}

func waitForOutboundMessages(t *testing.T, writer *blockedCaptureWriteCloser, count int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		messages := capturedOutboundMessages(t, writer.Bytes())
		if len(messages) >= count {
			return messages
		}
		time.Sleep(time.Millisecond)
	}
	return capturedOutboundMessages(t, writer.Bytes())
}

func TestOutboundDidChangeReturnsWhileWriterIsBlocked(t *testing.T) {
	writer := newBlockedCaptureWriteCloser()
	client := newOutboundTestClient(t, writer)

	client.DidOpen("file:///workspace/main.go", "go", 1, "old")
	waitForBlockedWriter(t, writer)

	returned := make(chan struct{})
	go func() {
		client.DidChange("file:///workspace/main.go", 2, "latest")
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("didChange waited for a blocked stdin write")
	}
}

func TestOutboundCoalescesPendingDidChangeToLatestVersion(t *testing.T) {
	writer := newBlockedCaptureWriteCloser()
	client := newOutboundTestClient(t, writer)
	uri := "file:///workspace/main.go"

	client.DidOpen(uri, "go", 1, "one")
	waitForBlockedWriter(t, writer)
	for version := 2; version <= 64; version++ {
		client.DidChange(uri, version, fmt.Sprintf("version-%d", version))
	}

	writer.unblock()
	messages := waitForOutboundMessages(t, writer, 2)
	if len(messages) != 2 {
		t.Fatalf("outbound message count = %d, want open plus latest change", len(messages))
	}
	if got := messages[0]["method"]; got != "textDocument/didOpen" {
		t.Fatalf("first outbound method = %q, want didOpen", got)
	}
	if got := messages[1]["method"]; got != "textDocument/didChange" {
		t.Fatalf("second outbound method = %q, want didChange", got)
	}
	params := messages[1]["params"].(map[string]any)
	if got := int(params["textDocument"].(map[string]any)["version"].(float64)); got != 64 {
		t.Fatalf("coalesced didChange version = %d, want 64", got)
	}
	change := params["contentChanges"].([]any)[0].(map[string]any)
	if got := change["text"]; got != "version-64" {
		t.Fatalf("coalesced didChange text = %q, want latest", got)
	}
}

func TestOutboundRejectsReversedOlderDidChangeVersion(t *testing.T) {
	writer := newBlockedCaptureWriteCloser()
	client := newOutboundTestClient(t, writer)
	uri := "file:///workspace/main.go"

	client.DidOpen(uri, "go", 1, "one")
	waitForBlockedWriter(t, writer)
	client.DidChange(uri, 3, "three")
	client.DidChange(uri, 2, "two")

	if version, open := client.DocumentVersion(uri); !open || version != 3 {
		t.Fatalf("tracked document version = %d, open = %v; want version 3 open", version, open)
	}
	writer.unblock()
	messages := waitForOutboundMessages(t, writer, 2)
	if len(messages) != 2 {
		t.Fatalf("outbound message count = %d, want open plus version 3", len(messages))
	}
	params := messages[1]["params"].(map[string]any)
	if got := int(params["textDocument"].(map[string]any)["version"].(float64)); got != 3 {
		t.Fatalf("didChange version = %d, want 3", got)
	}
}

func TestOutboundReversedIncrementalChangeFallsBackToFullLatestSnapshot(t *testing.T) {
	writer := newBlockedCaptureWriteCloser()
	client := newOutboundTestClient(t, writer)
	uri := "file:///workspace/main.go"

	client.DidOpen(uri, "go", 1, "a")
	waitForBlockedWriter(t, writer)
	// Both edits were derived from version 1. Cmd scheduling may run v3 before
	// v2, so v3 must be sent as a complete target document instead of a delta
	// whose range relies on the old server snapshot.
	client.DidChangeIncrementalWithContent(uri, 3, 0, 1, 0, 1, "c", "abc")
	client.DidChangeIncrementalWithContent(uri, 2, 0, 1, 0, 1, "b", "ab")

	writer.unblock()
	messages := waitForOutboundMessages(t, writer, 2)
	if len(messages) != 2 {
		t.Fatalf("outbound message count = %d, want open plus version 3", len(messages))
	}
	params := messages[1]["params"].(map[string]any)
	if got := int(params["textDocument"].(map[string]any)["version"].(float64)); got != 3 {
		t.Fatalf("didChange version = %d, want 3", got)
	}
	change := params["contentChanges"].([]any)[0].(map[string]any)
	if got := change["text"]; got != "abc" {
		t.Fatalf("fallback didChange text = %q, want full v3 target", got)
	}
	if _, incremental := change["range"]; incremental {
		t.Fatal("reversed incremental change retained a range instead of falling back to full sync")
	}
}

func TestOutboundAllowsDocumentLargerThanTenMiBWithinTransportBudget(t *testing.T) {
	writer := newBlockedCaptureWriteCloser()
	client := newOutboundTestClient(t, writer)
	uri := "file:///workspace/main.go"

	client.DidOpen(uri, "go", 1, "one")
	waitForBlockedWriter(t, writer)
	client.DidChange(uri, 2, strings.Repeat("x", 11<<20))

	if !client.IsReady() {
		t.Fatal("11 MiB document exceeded the outbound transport policy")
	}
	client.outboundMu.Lock()
	queuedBytes := client.outboundBytes
	client.outboundMu.Unlock()
	if queuedBytes <= 10<<20 || queuedBytes > maxPendingOutboundBytes {
		t.Fatalf("queued byte count = %d, want within explicit transport budget above 10 MiB", queuedBytes)
	}
}

func TestDocumentTextFitsOutboundMessageAccountsForJSONExpansion(t *testing.T) {
	previousLimit := maxOutboundMessageBytes
	maxOutboundMessageBytes = 100
	t.Cleanup(func() { maxOutboundMessageBytes = previousLimit })

	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "ordinary text", text: strings.Repeat("x", 70), want: true},
		{name: "control bytes", text: strings.Repeat("\x00", 20), want: false},
		{name: "HTML escaped by encoding json", text: strings.Repeat("<", 20), want: false},
		{name: "unicode line separator", text: strings.Repeat("\u2028", 20), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := documentTextFitsOutboundMessage(tt.text); got != tt.want {
				t.Fatalf("documentTextFitsOutboundMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOutboundOversizedDocumentIsRetiredWithoutPoisoningOtherDocuments(t *testing.T) {
	previousLimit := maxOutboundMessageBytes
	maxOutboundMessageBytes = 1024
	t.Cleanup(func() { maxOutboundMessageBytes = previousLimit })

	writer := newBlockedCaptureWriteCloser()
	client := newOutboundTestClient(t, writer)
	oversizedURI := "file:///workspace/oversized.go"
	healthyURI := "file:///workspace/healthy.go"

	client.DidOpen(oversizedURI, "go", 1, "one")
	waitForBlockedWriter(t, writer)
	// The source is valid UTF-8 but JSON must escape every NUL byte. The test
	// scales down the package-private transport cap; the 6x expansion is the
	// same one that can push a valid 64 MiB editor document over the 65 MiB
	// production message limit.
	oversizedContent := strings.Repeat("\x00", 256)
	client.DidChange(oversizedURI, 2, oversizedContent)
	client.DidOpen(healthyURI, "go", 1, "healthy")

	if _, open := client.DocumentVersion(oversizedURI); open {
		t.Fatal("oversized document remained marked as synchronized")
	}
	if !client.DocumentSyncSuppressed(oversizedURI) {
		t.Fatal("oversized document was not marked for a later full reopen")
	}
	if version, open := client.DocumentVersion(healthyURI); !open || version != 1 {
		t.Fatalf("healthy document version = %d, open = %v; want version 1 open", version, open)
	}
	if !client.IsReady() {
		t.Fatal("oversized document poisoned the LSP client for other files")
	}

	writer.unblock()
	messages := waitForOutboundMessages(t, writer, 3)
	if len(messages) != 3 {
		t.Fatalf("outbound message count = %d, want open, close, healthy open", len(messages))
	}
	wantMethods := []string{"textDocument/didOpen", "textDocument/didClose", "textDocument/didOpen"}
	for index, want := range wantMethods {
		if got, _ := messages[index]["method"].(string); got != want {
			t.Fatalf("message %d method = %q, want %q", index, got, want)
		}
	}
	closeParams := messages[1]["params"].(map[string]any)
	if got := closeParams["textDocument"].(map[string]any)["uri"]; got != oversizedURI {
		t.Fatalf("retirement didClose URI = %q, want %q", got, oversizedURI)
	}
	healthyParams := messages[2]["params"].(map[string]any)
	if got := healthyParams["textDocument"].(map[string]any)["uri"]; got != healthyURI {
		t.Fatalf("healthy didOpen URI = %q, want %q", got, healthyURI)
	}

	client.DidOpen(oversizedURI, "go", 3, "small again")
	if version, open := client.DocumentVersion(oversizedURI); !open || version != 3 {
		t.Fatalf("reopened document version = %d, open = %v; want version 3 open", version, open)
	}
	if client.DocumentSyncSuppressed(oversizedURI) {
		t.Fatal("admissibly sized reopen left document suppressed")
	}
}

func TestOutboundBarriersPreserveOpenChangeSaveCloseResponseAndRequestOrder(t *testing.T) {
	writer := newBlockedCaptureWriteCloser()
	client := newOutboundTestClient(t, writer)
	uri := "file:///workspace/main.go"

	client.DidOpen(uri, "go", 1, "one")
	waitForBlockedWriter(t, writer)
	client.DidChange(uri, 2, "two")
	client.DidSave(uri)
	client.DidClose(uri)
	client.handleWorkDoneProgressCreate(ptrTo(31))

	requestResult := make(chan error, 1)
	go func() {
		_, err := client.call(context.Background(), "workspace/executeCommand", map[string]any{"command": "test"})
		requestResult <- err
	}()

	writer.unblock()
	messages := waitForOutboundMessages(t, writer, 6)
	if len(messages) != 6 {
		t.Fatalf("outbound message count = %d, want 6", len(messages))
	}
	wantMethods := []string{
		"textDocument/didOpen",
		"textDocument/didChange",
		"textDocument/didSave",
		"textDocument/didClose",
		"",
		"workspace/executeCommand",
	}
	for index, want := range wantMethods {
		got, _ := messages[index]["method"].(string)
		if got != want {
			t.Fatalf("message %d method = %q, want %q", index, got, want)
		}
	}
	if got := int(messages[4]["id"].(float64)); got != 31 {
		t.Fatalf("response id = %d, want 31", got)
	}
	requestID := int(messages[5]["id"].(float64))
	client.handleMessage(json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":null}`, requestID)))
	if err := <-requestResult; err != nil {
		t.Fatalf("queued request error = %v", err)
	}
}

func TestOutboundBarrierOverflowMarksClientUnhealthyAndStopsIt(t *testing.T) {
	writer := newBlockedCaptureWriteCloser()
	client := newOutboundTestClient(t, writer)

	client.DidOpen("file:///workspace/main.go", "go", 1, "one")
	waitForBlockedWriter(t, writer)
	for range maxPendingOutbound + 1 {
		client.DidSave("file:///workspace/main.go")
	}
	if client.IsReady() {
		t.Fatal("barrier overflow left the LSP client ready")
	}
}

func TestOutboundRequestCancellationRemovesQueuedRequest(t *testing.T) {
	writer := newBlockedCaptureWriteCloser()
	client := newOutboundTestClient(t, writer)

	client.DidOpen("file:///workspace/main.go", "go", 1, "one")
	waitForBlockedWriter(t, writer)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.call(ctx, "workspace/executeCommand", map[string]any{"command": "cancel"})
		result <- err
	}()
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		client.mu.RLock()
		pending := len(client.pending)
		client.mu.RUnlock()
		if pending == 1 {
			break
		}
	}
	cancel()
	select {
	case err := <-result:
		if err != context.Canceled {
			t.Fatalf("cancelled request error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled request remained blocked behind stdin")
	}
	client.mu.RLock()
	pending := len(client.pending)
	client.mu.RUnlock()
	if pending != 0 {
		t.Fatalf("pending requests = %d, want 0", pending)
	}
	writer.unblock()
	messages := waitForOutboundMessages(t, writer, 1)
	if bytes.Contains(writer.Bytes(), []byte(`"workspace/executeCommand"`)) {
		t.Fatalf("cancelled queued request reached stdin: %#v", messages)
	}
}

func TestOutboundRequestReturnsWhenProcessStopsDuringBlockedWrite(t *testing.T) {
	writer := newBlockedCaptureWriteCloser()
	client := &Client{
		stdin:       writer,
		pending:     make(map[int]chan callResult),
		running:     true,
		initialized: true,
		processDone: make(chan struct{}),
	}
	client.startOutboundWorker()
	t.Cleanup(writer.unblock)

	result := make(chan error, 1)
	go func() {
		_, err := client.call(context.Background(), "workspace/executeCommand", map[string]any{"command": "stop"})
		result <- err
	}()
	waitForBlockedWriter(t, writer)
	close(client.processDone)

	select {
	case err := <-result:
		if !errors.Is(err, ErrClientNotRunning) {
			t.Fatalf("stopped request error = %v, want client-not-running", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request remained blocked after process exit")
	}
}

func TestShutdownRejectsNormalDocumentLifecycleNotifications(t *testing.T) {
	writer := newBlockedCaptureWriteCloser()
	client := newOutboundTestClient(t, writer)
	uri := "file:///workspace/main.go"

	client.Shutdown()
	client.DidOpen(uri, "go", 1, "one")
	client.DidChange(uri, 2, "two")
	client.DidSave(uri)
	client.DidClose(uri)

	if _, open := client.DocumentVersion(uri); open {
		t.Fatal("shutdown accepted a normal document lifecycle operation")
	}
	client.mu.RLock()
	_, snapshotRetained := client.documents[uri]
	client.mu.RUnlock()
	if snapshotRetained {
		t.Fatal("shutdown retained a snapshot from a normal document notification")
	}
}

func TestDocumentSnapshotBudgetIsGlobalAndRecoversAfterClose(t *testing.T) {
	previousBudget := maxDocumentSnapshotBudget
	maxDocumentSnapshotBudget = 20
	t.Cleanup(func() { maxDocumentSnapshotBudget = previousBudget })

	client := &Client{
		stdin:    &captureWriteCloser{},
		openDocs: make(map[string]int),
		running:  true,
	}
	firstURI := "file:///workspace/first.go"
	secondURI := "file:///workspace/second.go"
	thirdURI := "file:///workspace/third.go"

	client.DidOpen(firstURI, "go", 1, "first")
	client.DidOpen(secondURI, "go", 1, "second")
	if _, ok := client.documentSnapshot(firstURI); !ok {
		t.Fatal("first snapshot was not retained within the global budget")
	}
	if _, ok := client.documentSnapshot(secondURI); ok {
		t.Fatal("second snapshot exceeded the global budget but was retained")
	}
	if version, open := client.DocumentVersion(secondURI); !open || version != 1 {
		t.Fatalf("budget degradation lost LSP synchronization: version=%d open=%v", version, open)
	}
	if _, err := client.protocolPositionToInternal(secondURI, Position{Line: 0, Character: 0}); err != nil {
		t.Fatalf("UTF-8 degradation should retain byte-coordinate compatibility: %v", err)
	}
	client.mu.Lock()
	client.positionEncoding = positionEncodingUTF16
	client.mu.Unlock()
	if _, err := client.protocolPositionToInternal(secondURI, Position{Line: 0, Character: 0}); err == nil {
		t.Fatal("UTF-16 conversion guessed coordinates without a retained snapshot")
	}

	client.DidClose(firstURI)
	client.DidOpen(thirdURI, "go", 1, "third")
	if _, ok := client.documentSnapshot(thirdURI); !ok {
		t.Fatal("closing a retained document did not release its global snapshot budget")
	}
	client.mu.RLock()
	used := client.documentSnapshotBytes
	client.mu.RUnlock()
	if used > maxDocumentSnapshotBudget {
		t.Fatalf("retained snapshot bytes = %d, budget = %d", used, maxDocumentSnapshotBudget)
	}
}

func TestDocumentGenerationRejectsLateOpenAndChangeAfterReopen(t *testing.T) {
	client := &Client{
		stdin:    &captureWriteCloser{},
		openDocs: make(map[string]int),
		running:  true,
	}
	uri := "file:///workspace/main.go"
	if !client.bindDocumentGeneration(uri, 1) {
		t.Fatal("failed to bind initial document generation")
	}
	client.didOpen(uri, "go", 1, "old", 1)
	client.closeDocumentGeneration(uri, 2)
	if !client.bindDocumentGeneration(uri, 3) {
		t.Fatal("failed to bind reopened document generation")
	}
	client.didOpen(uri, "go", 1, "new", 3)

	client.didOpen(uri, "go", 2, "late-open", 1)
	client.didChange(uri, 2, "late-change", 1)

	snapshot, ok := client.documentSnapshot(uri)
	if !ok || snapshot.content != "new" || snapshot.version != 1 {
		t.Fatalf("late lifecycle command changed reopened snapshot: %#v, retained=%v", snapshot, ok)
	}
}

func TestOutboundWriterDropsQueuedDocumentGenerationAfterClose(t *testing.T) {
	writer := newBlockedCaptureWriteCloser()
	client := newOutboundTestClient(t, writer)
	uri := "file:///workspace/main.go"
	if !client.bindDocumentGeneration(uri, 1) {
		t.Fatal("failed to bind initial generation")
	}
	if err := client.notify("$/block", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	waitForBlockedWriter(t, writer)
	client.didOpen(uri, "go", 1, "old", 1)
	client.closeDocumentGeneration(uri, 2)
	if !client.bindDocumentGeneration(uri, 3) {
		t.Fatal("failed to bind reopened generation")
	}
	client.didOpen(uri, "go", 1, "new", 3)

	writer.unblock()
	messages := waitForOutboundMessages(t, writer, 3)
	if len(messages) != 3 {
		t.Fatalf("outbound messages = %d, want block-close-new-open", len(messages))
	}
	if got := messages[1]["method"]; got != "textDocument/didClose" {
		t.Fatalf("message after blocker = %q, want didClose", got)
	}
	if got := messages[2]["method"]; got != "textDocument/didOpen" {
		t.Fatalf("final message = %q, want new didOpen", got)
	}
	text := messages[2]["params"].(map[string]any)["textDocument"].(map[string]any)["text"]
	if text != "new" {
		t.Fatalf("reopened text = %q, want new", text)
	}
}

func TestIsReadyRequiresRunningClient(t *testing.T) {
	client := &Client{initialized: true, running: false}
	if client.IsReady() {
		t.Fatal("stopped initialized client reported ready")
	}
}
