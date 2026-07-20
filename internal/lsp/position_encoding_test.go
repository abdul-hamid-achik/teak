package lsp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestServerPositionEncodingNegotiation(t *testing.T) {
	tests := []struct {
		name     string
		encoding string
		want     positionEncoding
		wantErr  string
	}{
		{name: "utf-8", encoding: "utf-8", want: positionEncodingUTF8},
		{name: "utf-16", encoding: "utf-16", want: positionEncodingUTF16},
		{name: "utf-32", encoding: "utf-32", want: positionEncodingUTF32},
		{name: "legacy default", encoding: "", want: positionEncodingUTF16},
		{name: "unknown", encoding: "bytes", wantErr: "bytes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateServerPositionEncoding(tt.encoding)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("validateServerPositionEncoding(%q) error = %v, want %q", tt.encoding, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateServerPositionEncoding(%q) error = %v", tt.encoding, err)
			}
			if got != tt.want {
				t.Fatalf("validateServerPositionEncoding(%q) = %q, want %q", tt.encoding, got, tt.want)
			}
		})
	}
}

func TestInitializeParamsOffersAllSupportedPositionEncodings(t *testing.T) {
	params := initializeParams(42, "file:///workspace")
	capabilities, ok := params["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities = %T, want map[string]any", params["capabilities"])
	}
	general, ok := capabilities["general"].(map[string]any)
	if !ok {
		t.Fatalf("general = %T, want map[string]any", capabilities["general"])
	}
	encodings, ok := general["positionEncodings"].([]string)
	if !ok {
		t.Fatalf("positionEncodings = %T, want []string", general["positionEncodings"])
	}
	want := []string{string(positionEncodingUTF8), string(positionEncodingUTF16), string(positionEncodingUTF32)}
	if len(encodings) != len(want) {
		t.Fatalf("positionEncodings = %#v, want %#v", encodings, want)
	}
	for i := range want {
		if encodings[i] != want[i] {
			t.Fatalf("positionEncodings = %#v, want %#v", encodings, want)
		}
	}
}

func TestDocumentPositionConversionUnicodeAndCRLF(t *testing.T) {
	const content = "a😀e\u0301\r\nβ\n"
	tests := []struct {
		name     string
		encoding positionEncoding
		internal Position
		protocol Position
	}{
		{name: "utf8 after astral", encoding: positionEncodingUTF8, internal: Position{Line: 0, Character: 5}, protocol: Position{Line: 0, Character: 5}},
		{name: "utf16 after astral", encoding: positionEncodingUTF16, internal: Position{Line: 0, Character: 5}, protocol: Position{Line: 0, Character: 3}},
		{name: "utf32 after astral", encoding: positionEncodingUTF32, internal: Position{Line: 0, Character: 5}, protocol: Position{Line: 0, Character: 2}},
		{name: "utf16 combining mark", encoding: positionEncodingUTF16, internal: Position{Line: 0, Character: 8}, protocol: Position{Line: 0, Character: 5}},
		{name: "utf32 combining mark", encoding: positionEncodingUTF32, internal: Position{Line: 0, Character: 8}, protocol: Position{Line: 0, Character: 4}},
		{name: "crlf end is content end utf16", encoding: positionEncodingUTF16, internal: Position{Line: 0, Character: 9}, protocol: Position{Line: 0, Character: 5}},
		{name: "next line utf16", encoding: positionEncodingUTF16, internal: Position{Line: 1, Character: 2}, protocol: Position{Line: 1, Character: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := positionToProtocol(content, tt.encoding, tt.internal)
			if err != nil {
				t.Fatalf("positionToProtocol() error = %v", err)
			}
			if got != tt.protocol {
				t.Fatalf("positionToProtocol() = %#v, want %#v", got, tt.protocol)
			}

			back, err := positionFromProtocol(content, tt.encoding, got)
			if err != nil {
				t.Fatalf("positionFromProtocol() error = %v", err)
			}
			wantBack := tt.internal
			// Teak stores the CR in a CRLF line, while LSP positions deliberately
			// stop before the line terminator.
			if tt.internal.Line == 0 && tt.internal.Character == 9 {
				wantBack.Character = 8
			}
			if back != wantBack {
				t.Fatalf("positionFromProtocol() = %#v, want %#v", back, wantBack)
			}
		})
	}
}

func TestDocumentPositionConversionRejectsInvalidCoordinates(t *testing.T) {
	const valid = "a😀\n"
	for _, tt := range []struct {
		name string
		fn   func() error
	}{
		{name: "negative internal line", fn: func() error {
			_, err := positionToProtocol(valid, positionEncodingUTF16, Position{Line: -1})
			return err
		}},
		{name: "internal byte splits rune", fn: func() error {
			_, err := positionToProtocol(valid, positionEncodingUTF16, Position{Line: 0, Character: 2})
			return err
		}},
		{name: "internal column outside line", fn: func() error {
			_, err := positionToProtocol(valid, positionEncodingUTF16, Position{Line: 0, Character: 99})
			return err
		}},
		{name: "protocol line outside document", fn: func() error {
			_, err := positionFromProtocol(valid, positionEncodingUTF32, Position{Line: 3})
			return err
		}},
		{name: "utf16 splits surrogate pair", fn: func() error {
			_, err := positionFromProtocol(valid, positionEncodingUTF16, Position{Line: 0, Character: 2})
			return err
		}},
		{name: "protocol column outside line", fn: func() error {
			_, err := positionFromProtocol(valid, positionEncodingUTF32, Position{Line: 0, Character: 99})
			return err
		}},
		{name: "invalid utf8 document", fn: func() error {
			_, err := positionToProtocol("a\xff", positionEncodingUTF16, Position{Line: 0, Character: 1})
			return err
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(); err == nil {
				t.Fatal("conversion succeeded, want error")
			}
		})
	}
}

func TestClientConvertsIncrementalChangesAgainstOldDocumentSnapshot(t *testing.T) {
	uri := "file:///workspace/main.go"
	client := &Client{
		stdin:            &captureWriteCloser{},
		openDocs:         make(map[string]int),
		positionEncoding: positionEncodingUTF16,
	}
	client.DidOpen(uri, "go", 1, "a😀b")

	stdin := &captureWriteCloser{}
	client.stdin = stdin
	client.DidChangeIncremental(uri, 2, 0, 1, 0, 5, "X")

	message := decodeCapturedMessage(t, stdin.Bytes())
	params := message["params"].(map[string]any)
	changes := params["contentChanges"].([]any)
	change := changes[0].(map[string]any)
	rangeValue := change["range"].(map[string]any)
	start := rangeValue["start"].(map[string]any)
	end := rangeValue["end"].(map[string]any)
	if got := int(start["character"].(float64)); got != 1 {
		t.Fatalf("incremental start.character = %d, want 1 UTF-16 code unit", got)
	}
	if got := int(end["character"].(float64)); got != 3 {
		t.Fatalf("incremental end.character = %d, want 3 UTF-16 code units", got)
	}

	if got, ok := client.documentSnapshot(uri); !ok || got.content != "aXb" || got.version != 2 {
		t.Fatalf("document snapshot = %#v, %v; want version 2 content aXb", got, ok)
	}
}

func TestClientUsesFullChangeForCRLFIncrementalEdit(t *testing.T) {
	uri := "file:///workspace/main.go"
	client := &Client{
		stdin:            &captureWriteCloser{},
		openDocs:         make(map[string]int),
		positionEncoding: positionEncodingUTF16,
	}
	client.DidOpen(uri, "go", 1, "a\r\n")

	stdin := &captureWriteCloser{}
	client.stdin = stdin
	// The editor's byte model permits this position immediately before LF,
	// after CR. A ranged LSP change cannot express that CRLF distinction.
	client.DidChangeIncremental(uri, 2, 0, 2, 0, 2, "X")

	message := decodeCapturedMessage(t, stdin.Bytes())
	params := message["params"].(map[string]any)
	change := params["contentChanges"].([]any)[0].(map[string]any)
	if _, ok := change["range"]; ok {
		t.Fatalf("CRLF incremental change = %#v, want full-text change without range", change)
	}
	if got := change["text"]; got != "a\rX\n" {
		t.Fatalf("CRLF full change text = %q, want %q", got, "a\rX\n")
	}
}

func TestClientConvertsDiagnosticsFromNegotiatedEncoding(t *testing.T) {
	uri := "file:///workspace/main.go"
	client := &Client{
		msgChan:          make(chan any, 1),
		openDocs:         map[string]int{uri: 1},
		positionEncoding: positionEncodingUTF16,
		capabilities:     ServerCapabilities{FormattingProvider: true},
	}
	client.setDocumentSnapshot(uri, 1, "a😀b")
	client.handleDiagnostics(json.RawMessage(fmt.Sprintf(`{
		"uri": %q,
		"diagnostics": [{
			"range": {"start": {"line": 0, "character": 1}, "end": {"line": 0, "character": 3}},
			"severity": 1, "message": "bad emoji", "source": "test"
		}]
	}`, uri)))

	client.notificationMu.Lock()
	defer client.notificationMu.Unlock()
	if len(client.notificationQueue) != 1 {
		t.Fatalf("queued diagnostics = %d, want 1", len(client.notificationQueue))
	}
	msg, ok := client.notificationQueue[0].msg.(DiagnosticsMsg)
	if !ok {
		t.Fatalf("message = %T, want DiagnosticsMsg", client.notificationQueue[0].msg)
	}
	got := msg.Diagnostics[0].Range
	if got.Start != (DiagPosition{Line: 0, Character: 1}) || got.End != (DiagPosition{Line: 0, Character: 5}) {
		t.Fatalf("diagnostic range = %#v, want byte range 1:5", got)
	}
}

func TestClientFormattingConvertsTextEditsFromNegotiatedEncoding(t *testing.T) {
	uri := "file:///workspace/main.go"
	stdin := &captureWriteCloser{written: make(chan struct{}, 1)}
	client := &Client{
		stdin:            stdin,
		pending:          make(map[int]chan callResult),
		running:          true,
		openDocs:         map[string]int{uri: 1},
		positionEncoding: positionEncodingUTF16,
		capabilities:     ServerCapabilities{FormattingProvider: true},
	}
	client.setDocumentSnapshot(uri, 1, "a😀b")

	result := make(chan struct {
		edits []TextEdit
		err   error
	}, 1)
	go func() {
		edits, err := client.Formatting(uri, FormattingOptions{TabSize: 4, InsertSpaces: true})
		result <- struct {
			edits []TextEdit
			err   error
		}{edits, err}
	}()

	select {
	case <-stdin.written:
	case <-time.After(time.Second):
		t.Fatal("formatting request was not sent")
	}
	request := decodeCapturedMessage(t, stdin.Bytes())
	id := int(request["id"].(float64))
	client.handleMessage(json.RawMessage(fmt.Sprintf(`{
		"jsonrpc":"2.0", "id":%d, "result":[{
			"range":{"start":{"line":0,"character":1},"end":{"line":0,"character":3}},
			"newText":"X"
		}]
	}`, id)))

	got := <-result
	if got.err != nil {
		t.Fatalf("Formatting() error = %v", got.err)
	}
	if len(got.edits) != 1 || got.edits[0].StartCol != 1 || got.edits[0].EndCol != 5 {
		t.Fatalf("Formatting() edits = %#v, want byte range 1:5", got.edits)
	}
}

func TestClientCompletionConvertsOutboundPosition(t *testing.T) {
	uri := "file:///workspace/main.go"
	stdin := &captureWriteCloser{written: make(chan struct{}, 1)}
	client := &Client{
		stdin:            stdin,
		pending:          make(map[int]chan callResult),
		running:          true,
		openDocs:         map[string]int{uri: 1},
		positionEncoding: positionEncodingUTF16,
	}
	client.setDocumentSnapshot(uri, 1, "a😀b")

	result := make(chan error, 1)
	go func() {
		_, err := client.Completion(uri, 0, 5)
		result <- err
	}()

	request := waitForCapturedRequest(t, stdin)
	params := request["params"].(map[string]any)
	position := params["position"].(map[string]any)
	if got := int(position["character"].(float64)); got != 3 {
		t.Fatalf("completion character = %d, want UTF-16 character 3", got)
	}
	respondToCapturedRequest(t, client, request, `[]`)
	if err := <-result; err != nil {
		t.Fatalf("Completion() error = %v", err)
	}
}

func TestClientDefinitionConvertsInboundLocationPosition(t *testing.T) {
	sourceURI := "file:///workspace/main.go"
	targetURI := "file:///workspace/target.go"
	stdin := &captureWriteCloser{written: make(chan struct{}, 1)}
	client := &Client{
		stdin:            stdin,
		pending:          make(map[int]chan callResult),
		running:          true,
		openDocs:         map[string]int{sourceURI: 1, targetURI: 1},
		positionEncoding: positionEncodingUTF16,
	}
	client.setDocumentSnapshot(sourceURI, 1, "a😀b")
	client.setDocumentSnapshot(targetURI, 1, "a😀b")

	result := make(chan struct {
		locations []Location
		err       error
	}, 1)
	go func() {
		locations, err := client.Definition(sourceURI, 0, 5)
		result <- struct {
			locations []Location
			err       error
		}{locations, err}
	}()

	request := waitForCapturedRequest(t, stdin)
	respondToCapturedRequest(t, client, request, fmt.Sprintf(`[{"uri":%q,"range":{"start":{"line":0,"character":1},"end":{"line":0,"character":3}}}]`, targetURI))
	got := <-result
	if got.err != nil {
		t.Fatalf("Definition() error = %v", got.err)
	}
	if len(got.locations) != 1 || got.locations[0].StartCol != 1 || got.locations[0].EndCol != 5 {
		t.Fatalf("Definition() locations = %#v, want byte columns 1:5", got.locations)
	}
}

func TestClientCodeActionConvertsRangesAndWorkspaceEdit(t *testing.T) {
	uri := "file:///workspace/main.go"
	stdin := &captureWriteCloser{written: make(chan struct{}, 1)}
	client := &Client{
		stdin:            stdin,
		pending:          make(map[int]chan callResult),
		running:          true,
		openDocs:         map[string]int{uri: 1},
		positionEncoding: positionEncodingUTF16,
	}
	client.setDocumentSnapshot(uri, 1, "a😀b")

	result := make(chan struct {
		actions []CodeAction
		err     error
	}, 1)
	go func() {
		actions, err := client.CodeAction(uri, 0, 1, 0, 5, []Diagnostic{{
			Range: DiagRange{Start: DiagPosition{Line: 0, Character: 1}, End: DiagPosition{Line: 0, Character: 5}},
		}})
		result <- struct {
			actions []CodeAction
			err     error
		}{actions, err}
	}()

	request := waitForCapturedRequest(t, stdin)
	params := request["params"].(map[string]any)
	rangeValue := params["range"].(map[string]any)
	if got := int(rangeValue["end"].(map[string]any)["character"].(float64)); got != 3 {
		t.Fatalf("code action end character = %d, want 3 UTF-16 units", got)
	}
	diagnostics := params["context"].(map[string]any)["diagnostics"].([]any)
	diagnosticRange := diagnostics[0].(map[string]any)["range"].(map[string]any)
	if got := int(diagnosticRange["end"].(map[string]any)["character"].(float64)); got != 3 {
		t.Fatalf("diagnostic end character = %d, want 3 UTF-16 units", got)
	}
	respondToCapturedRequest(t, client, request, fmt.Sprintf(`[{"title":"replace","edit":{"changes":{%q:[{"range":{"start":{"line":0,"character":1},"end":{"line":0,"character":3}},"newText":"X"}]}}}]`, uri))
	got := <-result
	if got.err != nil {
		t.Fatalf("CodeAction() error = %v", got.err)
	}
	if len(got.actions) != 1 || got.actions[0].Edit == nil || got.actions[0].Edit.Changes[uri][0].EndCol != 5 {
		t.Fatalf("CodeAction() actions = %#v, want workspace edit byte end column 5", got.actions)
	}
}

func TestClientWorkspaceApplyEditConvertsInboundRanges(t *testing.T) {
	uri := "file:///workspace/main.go"
	requests := make(chan any, 1)
	stdin := &captureWriteCloser{written: make(chan struct{}, 1)}
	client := &Client{
		stdin:            stdin,
		msgChan:          requests,
		openDocs:         map[string]int{uri: 1},
		positionEncoding: positionEncodingUTF16,
	}
	client.setDocumentSnapshot(uri, 1, "a😀b")
	client.handleMessage(json.RawMessage(fmt.Sprintf(`{
		"jsonrpc":"2.0", "id":42, "method":"workspace/applyEdit",
		"params":{"edit":{"changes":{%q:[{
			"range":{"start":{"line":0,"character":1},"end":{"line":0,"character":3}},"newText":"X"
		}]}}}
	}`, uri)))

	select {
	case raw := <-requests:
		req, ok := raw.(ApplyEditRequestMsg)
		if !ok {
			t.Fatalf("request = %T, want ApplyEditRequestMsg", raw)
		}
		if got := req.Edit.Changes[uri][0].EndCol; got != 5 {
			t.Fatalf("workspace edit end col = %d, want byte column 5", got)
		}
		req.Respond(true, "")
	case <-time.After(time.Second):
		t.Fatal("workspace edit was not delivered")
	}
}

func TestClientDefersUnopenedUTF16LocationWithoutCachingOrFileIO(t *testing.T) {
	uri := "file:///path-controlled-by-server/unknown.go"
	client := &Client{positionEncoding: positionEncodingUTF16}
	location, err := client.locationFromProtocol(Location{
		URI: uri, StartLine: 0, StartCol: 1, EndLine: 0, EndCol: 3,
	})
	if err != nil {
		t.Fatalf("locationFromProtocol() error = %v", err)
	}
	if location.ProtocolEncoding != string(positionEncodingUTF16) {
		t.Fatalf("ProtocolEncoding = %q, want utf-16", location.ProtocolEncoding)
	}
	if _, ok := client.documentSnapshot(uri); ok {
		t.Fatal("unopened server URI was cached as a document snapshot")
	}
}

func TestDidCloseRemovesDocumentPositionSnapshot(t *testing.T) {
	uri := "file:///workspace/main.go"
	client := &Client{openDocs: map[string]int{}, documents: map[string]documentSnapshot{}}
	client.DidOpen(uri, "go", 1, "a😀b")
	if _, ok := client.documentSnapshot(uri); !ok {
		t.Fatal("DidOpen() did not retain document snapshot")
	}
	client.DidClose(uri)
	if _, ok := client.documentSnapshot(uri); ok {
		t.Fatal("DidClose() retained document snapshot")
	}
}

func TestHotPositionConversionDoesNotAllocateOrScanWholeDocument(t *testing.T) {
	const lines = 100_000
	content := strings.Repeat("0123456789😀abcdefghij\n", lines)
	snapshot, err := newDocumentSnapshot(1, content)
	if err != nil {
		t.Fatalf("newDocumentSnapshot() error = %v", err)
	}
	if snapshot.lineCount != lines+1 {
		t.Fatalf("line count = %d, want %d", snapshot.lineCount, lines+1)
	}
	maxCheckpoints := 1 + lines/positionLineCheckpointStride
	if len(snapshot.lineCheckpoints) > maxCheckpoints {
		t.Fatalf("line checkpoints = %d, want at most %d", len(snapshot.lineCheckpoints), maxCheckpoints)
	}
	client := &Client{documents: map[string]documentSnapshot{"file:///large.go": snapshot}, positionEncoding: positionEncodingUTF16}
	position := Position{Line: lines - 1, Character: len("0123456789😀")}
	allocations := testing.AllocsPerRun(100, func() {
		got, err := client.internalPositionToProtocol("file:///large.go", position)
		if err != nil || got.Character != 12 { // ten ASCII bytes + a UTF-16 surrogate pair
			panic(fmt.Sprintf("conversion = %#v, %v", got, err))
		}
	})
	if allocations != 0 {
		t.Fatalf("hot position conversion allocations = %f, want 0", allocations)
	}
}

func TestDocumentSnapshotLineMetadataIsBoundedForMillionsOfLines(t *testing.T) {
	const lines = 1_000_000
	snapshot, err := newDocumentSnapshot(1, strings.Repeat("x\n", lines))
	if err != nil {
		t.Fatalf("newDocumentSnapshot() error = %v", err)
	}
	if snapshot.lineCount != lines+1 {
		t.Fatalf("line count = %d, want %d", snapshot.lineCount, lines+1)
	}
	wantAtMost := 1 + lines/positionLineCheckpointStride
	if len(snapshot.lineCheckpoints) > wantAtMost {
		t.Fatalf("checkpoint entries = %d, want at most %d", len(snapshot.lineCheckpoints), wantAtMost)
	}
	if _, err := positionToProtocolSnapshot(snapshot, positionEncodingUTF16, Position{Line: lines - 1, Character: 1}); err != nil {
		t.Fatalf("last-line conversion error = %v", err)
	}
}

func TestPositionFromProtocolLineBytes(t *testing.T) {
	tests := []struct {
		name      string
		line      []byte
		encoding  string
		character int
		want      int
		wantErr   bool
	}{
		{name: "utf8 byte column", line: []byte("a😀b"), encoding: "utf-8", character: 5, want: 5},
		{name: "utf16 astral", line: []byte("a😀b"), encoding: "utf-16", character: 3, want: 5},
		{name: "utf32 rune", line: []byte("a😀b"), encoding: "utf-32", character: 2, want: 5},
		{name: "crlf content excludes carriage return", line: []byte("abc\r"), encoding: "utf-16", character: 3, want: 3},
		{name: "split surrogate", line: []byte("a😀b"), encoding: "utf-16", character: 2, wantErr: true},
		{name: "invalid utf8", line: []byte{'a', 0xff}, encoding: "utf-16", character: 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PositionFromProtocolLine(tt.line, tt.encoding, tt.character)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("PositionFromProtocolLine() = %d, nil; want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("PositionFromProtocolLine() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("PositionFromProtocolLine() = %d, want %d", got, tt.want)
			}
		})
	}
}

func BenchmarkClientPositionToProtocolLargeDocument(b *testing.B) {
	const lines = 100_000
	content := strings.Repeat("0123456789😀abcdefghij\n", lines)
	snapshot, err := newDocumentSnapshot(1, content)
	if err != nil {
		b.Fatal(err)
	}
	client := &Client{documents: map[string]documentSnapshot{"file:///large.go": snapshot}, positionEncoding: positionEncodingUTF16}
	position := Position{Line: lines - 1, Character: len("0123456789😀")}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := client.internalPositionToProtocol("file:///large.go", position); err != nil {
			b.Fatal(err)
		}
	}
}

func waitForCapturedRequest(t *testing.T, stdin *captureWriteCloser) map[string]any {
	t.Helper()
	select {
	case <-stdin.written:
	case <-time.After(time.Second):
		t.Fatal("LSP request was not sent")
	}
	return decodeCapturedMessage(t, stdin.Bytes())
}

func respondToCapturedRequest(t *testing.T, client *Client, request map[string]any, result string) {
	t.Helper()
	id, ok := request["id"].(float64)
	if !ok {
		t.Fatalf("request id = %T, want number", request["id"])
	}
	client.handleMessage(json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, int(id), result)))
}
