package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	sdk "github.com/coder/acp-go-sdk"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/acp"
	"teak/internal/ui"
)

// TestAgentModelCreation tests New function
func TestAgentModelCreation(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.toolCallMap == nil {
		t.Error("Expected toolCallMap to be initialized")
	}
	if model.alwaysAllow == nil {
		t.Error("Expected alwaysAllow to be initialized")
	}
	if !model.autoScroll {
		t.Error("Expected autoScroll to be true")
	}
	if model.state != AgentDisconnected {
		t.Errorf("Expected state AgentDisconnected, got %v", model.state)
	}
	if model.loading {
		t.Error("Expected loading to be false")
	}
	if model.connected {
		t.Error("Expected connected to be false")
	}
}

func TestAgentHistoryIsBounded(t *testing.T) {
	model := New(ui.DefaultTheme())
	for i := 0; i < maxChatMessages+25; i++ {
		model.AddSystemMessage(strings.Repeat("x", maxChatMessageBytes/8))
	}

	if got := len(model.messages); got > maxChatMessages {
		t.Fatalf("messages = %d, want at most %d", got, maxChatMessages)
	}
	if model.messageBytes > maxChatHistoryBytes {
		t.Fatalf("messageBytes = %d, want at most %d", model.messageBytes, maxChatHistoryBytes)
	}
}

func TestAgentStreamingContentIsBoundedAndValidUTF8(t *testing.T) {
	model := New(ui.DefaultTheme())
	oversized := strings.Repeat("界", maxStreamContentBytes)

	model, _ = model.Update(acp.AgentTextMsg{Text: oversized})

	if model.streamBytes > maxStreamContentBytes {
		t.Fatalf("streamBytes = %d, want at most %d", model.streamBytes, maxStreamContentBytes)
	}
	if got, wantMax := len(model.streamBlocks), maxStreamContentBytes/streamRenderBlockBytes+1; got > wantMax {
		t.Fatalf("stream blocks = %d, want at most %d", got, wantMax)
	}
	for _, block := range model.streamBlocks {
		if !utf8.ValidString(block.Content) {
			t.Fatal("bounded stream content is not valid UTF-8")
		}
	}
}

func TestAgentTextWrappingAndTagTruncationPreserveUnicode(t *testing.T) {
	for _, line := range wrapText("界界界 hello 🌳", 4) {
		if !utf8.ValidString(line) {
			t.Fatalf("wrapped line %q is not valid UTF-8", line)
		}
		if got := lipgloss.Width(line); got > 4 {
			t.Fatalf("wrapped line width = %d, want at most 4: %q", got, line)
		}
	}

	model := New(ui.DefaultTheme())
	model.taggedFiles = []TaggedFile{{Name: "🌳界-file.go"}}
	line := model.renderTaggedFiles(5)
	if !utf8.ValidString(line) {
		t.Fatalf("tag line %q is not valid UTF-8", line)
	}
	if got := lipgloss.Width(line); got > 5 {
		t.Fatalf("tag line width = %d, want at most 5", got)
	}
}

func TestAgentChatRenderCacheIsSharedAcrossValueCopiesAndInvalidated(t *testing.T) {
	model := New(ui.DefaultTheme())
	model.SetConnected(true)
	model.AddSystemMessage("first")

	first := model.cachedChatLines(40)
	copyOfModel := model
	second := copyOfModel.cachedChatLines(40)
	if len(first) == 0 || len(second) == 0 || &first[0] != &second[0] {
		t.Fatal("unchanged value copy did not reuse the shared render cache")
	}

	copyOfModel.AddSystemMessage("second")
	third := copyOfModel.cachedChatLines(40)
	if len(third) == 0 || &third[0] == &first[0] {
		t.Fatal("new message did not invalidate the render cache")
	}
	if !strings.Contains(strings.Join(third, "\n"), "second") {
		t.Fatal("rebuilt render cache does not contain the new message")
	}
}

func TestAgentStreamRenderCacheRebuildsOnlyDirtyTailAndMatchesCanonicalOutput(t *testing.T) {
	model := New(ui.DefaultTheme())
	model.SetConnected(true)
	model.SetSize(48, 18)
	for i := 0; i < 64; i++ {
		model.AddSystemMessage("history that must stay cached across streaming updates")
	}

	model, _ = model.Update(acp.AgentTextMsg{Text: strings.Repeat("alpha ", streamRenderBlockBytes/4)})
	first := slices.Clone(model.cachedChatLines(48))
	cache := model.chatCache
	if cache == nil || len(cache.streamBlockStarts) == 0 {
		t.Fatal("initial render did not record stream block boundaries")
	}
	prefixEnd := cache.streamBlockStarts[len(cache.streamBlockStarts)-1]
	if prefixEnd == 0 {
		t.Fatal("stream tail unexpectedly starts at the beginning of the transcript")
	}
	prefix := &cache.lines[0]

	model, _ = model.Update(acp.AgentTextMsg{Text: "omega"})
	got := model.cachedChatLines(48)
	if want := model.buildChatLines(48); !slices.Equal(got, want) {
		t.Fatalf("incremental lines differ from canonical render\n got: %q\nwant: %q", got, want)
	}
	if cache.fullBuilds != 1 || cache.incrementalBuilds != 1 {
		t.Fatalf("build counts = full %d incremental %d, want 1/1", cache.fullBuilds, cache.incrementalBuilds)
	}
	if &cache.lines[0] != prefix {
		t.Fatal("stream update replaced the cached transcript prefix")
	}
	if len(first) < prefixEnd {
		t.Fatal("test setup produced an invalid stream boundary")
	}
}

func TestAgentStreamRenderCachePreservesChunkRenderingAndStyles(t *testing.T) {
	chunked := New(ui.DefaultTheme())
	chunked.SetConnected(true)
	chunked.SetSize(28, 12)
	chunked, _ = chunked.Update(acp.AgentTextMsg{Text: "A short streamed "})
	chunked, _ = chunked.Update(acp.AgentTextMsg{Text: "sentence keeps wrapping."})
	chunked, _ = chunked.Update(acp.AgentThoughtMsg{Text: "a styled thought"})

	single := chunked
	single.streamBlocks = []StreamBlock{
		{Kind: BlockText, Content: "A short streamed sentence keeps wrapping."},
		{Kind: BlockThought, Content: "a styled thought"},
	}
	single.chatCache = &chatRenderCache{dirty: true, streamDirtyFrom: -1}

	if got, want := chunked.cachedChatLines(28), single.buildChatLines(28); !slices.Equal(got, want) {
		t.Fatalf("chunked render differs from a single logical block\n got: %q\nwant: %q", got, want)
	}
	if !strings.Contains(strings.Join(chunked.cachedChatLines(28), "\n"), "\x1b[") {
		t.Fatal("thought style was lost from the cached render")
	}
}

func TestAgentStreamRenderCacheInvalidatesOnResizeAndStructuralChanges(t *testing.T) {
	model := New(ui.DefaultTheme())
	model.SetConnected(true)
	model.SetSize(42, 16)
	model, _ = model.Update(acp.AgentTextMsg{Text: "first streaming chunk"})
	_ = model.cachedChatLines(42)
	cache := model.chatCache

	model.SetSize(60, 16)
	if !cache.dirty {
		t.Fatal("resize did not invalidate the render cache")
	}
	if got, want := model.cachedChatLines(60), model.buildChatLines(60); !slices.Equal(got, want) {
		t.Fatal("resized cache differs from canonical render")
	}

	model, _ = model.Update(acp.AgentToolCallMsg{ID: "tool", Title: "inspect", Status: sdk.ToolCallStatusPending})
	if !cache.dirty {
		t.Fatal("structural stream change did not invalidate the render cache")
	}
	if got, want := model.cachedChatLines(60), model.buildChatLines(60); !slices.Equal(got, want) {
		t.Fatal("structurally invalidated cache differs from canonical render")
	}
}

func TestAgentStreamRenderCacheLargeStreamHasBoundedBlocksAndIncrementalTail(t *testing.T) {
	model := New(ui.DefaultTheme())
	model.SetConnected(true)
	model.SetSize(80, 24)
	chunk := strings.Repeat("data ", streamRenderBlockBytes/5)
	for model.streamBytes+len(chunk) <= maxStreamContentBytes {
		model, _ = model.Update(acp.AgentTextMsg{Text: chunk})
	}
	_ = model.cachedChatLines(80)
	cache := model.chatCache
	if got, wantMax := len(model.streamBlocks), maxStreamContentBytes/streamRenderBlockBytes+1; got > wantMax {
		t.Fatalf("stream blocks = %d, want at most %d", got, wantMax)
	}
	if cache.fullBuilds != 1 {
		t.Fatalf("full builds = %d, want 1", cache.fullBuilds)
	}

	model, _ = model.Update(acp.AgentTextMsg{Text: "tail"})
	_ = model.cachedChatLines(80)
	if cache.incrementalBuilds != 1 {
		t.Fatalf("incremental builds = %d, want 1", cache.incrementalBuilds)
	}
	if cache.renderedStreamBlocks > 2 {
		t.Fatalf("tail update rendered %d stream blocks, want at most 2", cache.renderedStreamBlocks)
	}
}

func TestAgentAlwaysAllowDoesNotPersistForUnknownToolKind(t *testing.T) {
	options := []sdk.PermissionOption{
		{Kind: sdk.PermissionOptionKindAllowAlways, OptionId: "always"},
		{Kind: sdk.PermissionOptionKindAllowOnce, OptionId: "once"},
	}
	model := New(ui.DefaultTheme())
	model.permission = &PermissionPrompt{
		ToolCall:   sdk.RequestPermissionToolCall{ToolCallId: "first"},
		Options:    options,
		ResponseCh: make(chan sdk.RequestPermissionResponse, 1),
	}

	model, _ = model.handlePermissionKey("a")
	if model.alwaysAllow[""] {
		t.Fatal("unknown tool kind was persisted as an always-allow wildcard")
	}

	response := make(chan sdk.RequestPermissionResponse, 1)
	model, _ = model.Update(acp.AgentPermissionRequestMsg{
		ToolCall:   sdk.RequestPermissionToolCall{ToolCallId: "second"},
		Options:    options,
		ResponseCh: response,
	})
	if model.permission == nil {
		t.Fatal("second unknown tool request bypassed the permission prompt")
	}
	select {
	case <-response:
		t.Fatal("second unknown tool request was auto-approved")
	default:
	}
}

func TestAgentAlwaysAllowRequiresExplicitOption(t *testing.T) {
	toolKind := sdk.ToolKindExecute
	response := make(chan sdk.RequestPermissionResponse, 1)
	model := New(ui.DefaultTheme())
	model.permission = &PermissionPrompt{
		ToolCall: sdk.RequestPermissionToolCall{ToolCallId: "once-only", Kind: &toolKind},
		Options: []sdk.PermissionOption{
			{Kind: sdk.PermissionOptionKindAllowOnce, OptionId: "once"},
			{Kind: sdk.PermissionOptionKindRejectOnce, OptionId: "reject"},
		},
		ResponseCh: response,
	}

	model, _ = model.handlePermissionKey("a")
	if model.alwaysAllow[string(toolKind)] {
		t.Fatal("always-allow state persisted without an allow_always option")
	}
	select {
	case got := <-response:
		if got.Outcome.Selected == nil || got.Outcome.Selected.OptionId != "once" {
			t.Fatalf("response = %#v, want allow-once selection", got)
		}
	default:
		t.Fatal("allow-once fallback did not respond")
	}
}

func TestAgentPermissionViewOnlyShowsSupportedOptions(t *testing.T) {
	zone.NewGlobal()
	toolKind := sdk.ToolKindExecute
	model := New(ui.DefaultTheme())
	model.permission = &PermissionPrompt{
		ToolCall: sdk.RequestPermissionToolCall{ToolCallId: "once-only", Kind: &toolKind},
		Options: []sdk.PermissionOption{
			{Kind: sdk.PermissionOptionKindAllowOnce, OptionId: "once"},
			{Kind: sdk.PermissionOptionKindRejectOnce, OptionId: "reject"},
		},
		ResponseCh: make(chan sdk.RequestPermissionResponse, 1),
	}

	withoutAlways := strings.Join(model.renderPermission(80), "\n")
	if strings.Contains(withoutAlways, "[a] Always") {
		t.Fatal("permission view advertised allow_always when ACP did not offer it")
	}

	model.permission.Options = append(model.permission.Options,
		sdk.PermissionOption{Kind: sdk.PermissionOptionKindAllowAlways, OptionId: "always"},
	)
	withAlways := strings.Join(model.renderPermission(80), "\n")
	if !strings.Contains(withAlways, "[a] Always") {
		t.Fatal("permission view hid allow_always when ACP offered it")
	}
}

func TestAgentPermissionDecisionNeverBlocksTheUpdateLoop(t *testing.T) {
	model := New(ui.DefaultTheme())
	model.permission = &PermissionPrompt{
		ToolCall: sdk.RequestPermissionToolCall{ToolCallId: "blocked"},
		Options: []sdk.PermissionOption{{
			Kind:     sdk.PermissionOptionKindRejectOnce,
			OptionId: "reject",
		}},
		ResponseCh: make(chan sdk.RequestPermissionResponse),
	}

	done := make(chan struct{})
	go func() {
		_, _ = model.handlePermissionKey("n")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("permission decision blocked on an unread response channel")
	}
}

func BenchmarkAgentViewCachedHistory(b *testing.B) {
	model := New(ui.DefaultTheme())
	model.SetConnected(true)
	model.SetSize(48, 30)
	for i := 0; i < maxChatMessages; i++ {
		model.AddSystemMessage("bounded chat history line for render-cache benchmark")
	}
	_ = model.View()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = model.View()
	}
}

func BenchmarkAgentViewStreamingDirtyTail(b *testing.B) {
	model := benchmarkStreamingRenderModel(b)
	tail := len(model.streamBlocks) - 1

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.invalidateStreamTail(tail)
		_ = model.View()
	}
}

// BenchmarkAgentViewStreamingFullRebuild models the pre-cache behavior: each
// streamed chunk invalidated the complete chat transcript. Keep it next to the
// dirty-tail benchmark to make regressions visible in benchmark output.
func BenchmarkAgentViewStreamingFullRebuild(b *testing.B) {
	model := benchmarkStreamingRenderModel(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.invalidateChatCache()
		_ = model.View()
	}
}

func BenchmarkAgentScrollDirtyHistoryUpdate(b *testing.B) {
	model := benchmarkMaxHistoryModel(b)
	cache := model.chatCache

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		cache.dirty = true
		updated, _ := model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
		if !cache.dirty {
			b.Fatal("scroll event rendered the dirty transcript")
		}
		model = updated
	}
}

// BenchmarkAgentViewDirtyMaxHistoryFullRebuild measures the exact transcript
// render a dirty scroll event used to trigger inside Update.
func BenchmarkAgentViewDirtyMaxHistoryFullRebuild(b *testing.B) {
	model := benchmarkMaxHistoryModel(b)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		model.invalidateChatCache()
		_ = model.View()
	}
}

func BenchmarkAgentPromptResponseDispatchMaxStream(b *testing.B) {
	blocks, streamBytes := benchmarkPromptStream()
	base := New(ui.DefaultTheme())
	base.loading = true
	base.state = AgentThinking
	base.streamBlocks = blocks
	base.streamBytes = streamBytes

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		model := base
		_, cmd := model.Update(acp.AgentPromptResponseMsg{})
		if cmd == nil {
			b.Fatal("prompt response did not dispatch finalization")
		}
	}
}

func BenchmarkAgentPromptResponseProjectionMaxStream(b *testing.B) {
	blocks, streamBytes := benchmarkPromptStream()
	b.ReportAllocs()
	b.SetBytes(int64(streamBytes))
	b.ResetTimer()
	for b.Loop() {
		messages := preparePromptResponse(blocks, streamBytes, nil)
		if len(messages) != 1 || len(messages[0].message.Content) != maxChatMessageBytes {
			b.Fatalf("prepared response = %#v", messages)
		}
	}
}

func benchmarkPromptStream() ([]StreamBlock, int) {
	const blockBytes = streamRenderBlockBytes
	block := strings.Repeat("x", blockBytes)
	blocks := make([]StreamBlock, maxStreamContentBytes/blockBytes)
	for i := range blocks {
		blocks[i] = StreamBlock{Kind: BlockText, Content: block}
	}
	return blocks, len(block) * len(blocks)
}

func benchmarkMaxHistoryModel(b *testing.B) Model {
	b.Helper()
	model := New(ui.DefaultTheme())
	model.SetConnected(true)
	model.SetSize(80, 30)
	payload := strings.Repeat("bounded history ", 512)
	for range maxChatMessages {
		model.AddSystemMessage(payload)
	}
	_ = model.View()
	return model
}

func benchmarkStreamingRenderModel(b *testing.B) Model {
	b.Helper()
	model := New(ui.DefaultTheme())
	model.SetConnected(true)
	model.SetSize(80, 30)
	for i := 0; i < maxChatMessages; i++ {
		model.AddSystemMessage("history retained while ACP sends streamed chunks")
	}
	for i := 0; i < maxStreamContentBytes/(2*streamRenderBlockBytes); i++ {
		model, _ = model.Update(acp.AgentTextMsg{Text: strings.Repeat("stream ", streamRenderBlockBytes/8)})
	}
	_ = model.View()
	return model
}

// TestAgentIsLoading tests IsLoading method
func TestAgentIsLoading(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.IsLoading() {
		t.Error("Expected IsLoading to be false initially")
	}

	model.loading = true
	if !model.IsLoading() {
		t.Error("Expected IsLoading to be true")
	}
}

// TestAgentSetSize tests SetSize method
func TestAgentSetSize(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.SetSize(100, 30)

	if model.width != 100 {
		t.Errorf("Expected width 100, got %d", model.width)
	}
	if model.height != 30 {
		t.Errorf("Expected height 30, got %d", model.height)
	}
}

// TestAgentSetSizeWithSmallWidth tests SetSize with small width
func TestAgentSetSizeWithSmallWidth(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.SetSize(1, 30)

	if model.width != 1 {
		t.Errorf("Expected width 1, got %d", model.width)
	}
}

// TestAgentSetConnected tests SetConnected method
func TestAgentSetConnected(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.SetConnected(true)
	if !model.connected {
		t.Error("Expected connected to be true")
	}
	if model.State() != AgentIdle {
		t.Errorf("Expected state AgentIdle, got %v", model.State())
	}

	model.SetConnected(false)
	if model.connected {
		t.Error("Expected connected to be false")
	}
	if model.State() != AgentDisconnected {
		t.Errorf("Expected state AgentDisconnected, got %v", model.State())
	}
}

// TestAgentStateMethod tests State method
func TestAgentStateMethod(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.State() != AgentDisconnected {
		t.Errorf("Expected state AgentDisconnected, got %v", model.State())
	}

	model.state = AgentThinking
	if model.State() != AgentThinking {
		t.Errorf("Expected state AgentThinking, got %v", model.State())
	}
}

// TestAgentHasPermissionPending tests HasPermissionPending method
func TestAgentHasPermissionPending(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.HasPermissionPending() {
		t.Error("Expected HasPermissionPending to be false")
	}

	model.permission = &PermissionPrompt{}
	if !model.HasPermissionPending() {
		t.Error("Expected HasPermissionPending to be true")
	}
}

// TestAgentHasPendingWrite tests HasPendingWrite method
func TestAgentHasPendingWrite(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.HasPendingWrite() {
		t.Error("Expected HasPendingWrite to be false")
	}
}

func TestAgentPendingWriteKeyDecision(t *testing.T) {
	tests := []struct {
		name         string
		key          tea.KeyPressMsg
		wantAccepted bool
		wantPending  bool
		wantDecision bool
	}{
		{
			name:         "enter accepts proposal",
			key:          tea.KeyPressMsg{Code: tea.KeyEnter},
			wantAccepted: true,
			wantDecision: true,
		},
		{
			name:         "escape rejects proposal",
			key:          tea.KeyPressMsg{Code: tea.KeyEscape},
			wantAccepted: false,
			wantDecision: true,
		},
		{
			name:        "other key keeps proposal and does not submit chat",
			key:         tea.KeyPressMsg{Code: 'x', Text: "x"},
			wantPending: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := New(ui.DefaultTheme())
			proposal := acp.AgentWriteFileMsg{
				Path:       "/workspace/example.go",
				Content:    "package example",
				ResponseCh: make(chan error, 1),
			}
			model, _ = model.Update(proposal)
			model.input.SetValue("this must not be submitted")

			model, cmd := model.Update(tt.key)

			if got := model.HasPendingWrite(); got != tt.wantPending {
				t.Errorf("HasPendingWrite() = %t, want %t", got, tt.wantPending)
			}
			if len(model.messages) != 0 {
				t.Errorf("messages = %d, want 0", len(model.messages))
			}
			if model.loading {
				t.Error("loading = true, want false")
			}
			if got := model.InputValue(); got != "this must not be submitted" {
				t.Errorf("InputValue() = %q, want input preserved", got)
			}

			if !tt.wantDecision {
				if cmd != nil {
					t.Error("command = non-nil, want nil")
				}
				return
			}
			if cmd == nil {
				t.Fatal("command = nil, want write decision command")
			}
			msg := cmd()
			decision, ok := msg.(WriteDecisionMsg)
			if !ok {
				t.Fatalf("command message = %T, want WriteDecisionMsg", msg)
			}
			if decision.Accepted != tt.wantAccepted {
				t.Errorf("decision.Accepted = %t, want %t", decision.Accepted, tt.wantAccepted)
			}
			if decision.Proposal.Path != proposal.Path || decision.Proposal.Content != proposal.Content {
				t.Errorf("decision proposal = %#v, want path/content from %#v", decision.Proposal, proposal)
			}
			if decision.Proposal.ResponseCh != proposal.ResponseCh {
				t.Error("decision proposal did not retain the original response channel")
			}
			select {
			case response := <-proposal.ResponseCh:
				t.Errorf("proposal response = %v, want no direct response", response)
			default:
			}
		})
	}
}

func TestAgentPendingWriteQueuesProposals(t *testing.T) {
	model := New(ui.DefaultTheme())
	first := acp.AgentWriteFileMsg{
		Path:       "/workspace/first.go",
		Content:    "first",
		ResponseCh: make(chan error, 1),
	}
	second := acp.AgentWriteFileMsg{
		Path:       "/workspace/second.go",
		Content:    "second",
		ResponseCh: make(chan error, 1),
	}

	model, _ = model.Update(first)
	model, _ = model.Update(second)
	if got := model.PendingWrite(); got == nil || got.Path != first.Path {
		t.Fatalf("first pending proposal = %#v, want %q", got, first.Path)
	}

	model, firstCmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if firstCmd == nil {
		t.Fatal("first decision command = nil")
	}
	firstMsg := firstCmd()
	firstDecision, ok := firstMsg.(WriteDecisionMsg)
	if !ok {
		t.Fatalf("first decision = %T, want WriteDecisionMsg", firstMsg)
	}
	if !firstDecision.Accepted || firstDecision.Proposal.Path != first.Path || firstDecision.Proposal.ResponseCh != first.ResponseCh {
		t.Errorf("first decision = %#v, want accepted first proposal", firstDecision)
	}
	if got := model.PendingWrite(); got == nil || got.Path != second.Path {
		t.Fatalf("second pending proposal = %#v, want %q", got, second.Path)
	}

	model, secondCmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if secondCmd == nil {
		t.Fatal("second decision command = nil")
	}
	secondMsg := secondCmd()
	secondDecision, ok := secondMsg.(WriteDecisionMsg)
	if !ok {
		t.Fatalf("second decision = %T, want WriteDecisionMsg", secondMsg)
	}
	if secondDecision.Accepted || secondDecision.Proposal.Path != second.Path || secondDecision.Proposal.ResponseCh != second.ResponseCh {
		t.Errorf("second decision = %#v, want rejected second proposal", secondDecision)
	}
	if model.HasPendingWrite() {
		t.Error("HasPendingWrite() = true, want false after both decisions")
	}

	firstResult := errors.New("first result")
	secondResult := errors.New("second result")
	firstDecision.Proposal.ResponseCh <- firstResult
	secondDecision.Proposal.ResponseCh <- secondResult
	if got := <-first.ResponseCh; !errors.Is(got, firstResult) {
		t.Errorf("first response = %v, want %v", got, firstResult)
	}
	if got := <-second.ResponseCh; !errors.Is(got, secondResult) {
		t.Errorf("second response = %v, want %v", got, secondResult)
	}
}

func TestAgentPruneCancelledWritesRemovesCurrentAndQueuedProposals(t *testing.T) {
	model := New(ui.DefaultTheme())
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	first := acp.AgentWriteFileMsg{
		Path:       "first.go",
		ResponseCh: make(chan error, 1),
		Context:    firstCtx,
	}
	second := acp.AgentWriteFileMsg{
		Path:       "second.go",
		ResponseCh: make(chan error, 1),
		Context:    secondCtx,
	}

	model, _ = model.Update(first)
	model, _ = model.Update(second)
	cancelFirst()
	model.PruneCancelledWrites()
	if got := model.PendingWrite(); got == nil || got.Path != second.Path {
		t.Fatalf("pending proposal after first cancellation = %#v, want second", got)
	}
	if err := <-first.ResponseCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("first cancellation response = %v, want context.Canceled", err)
	}

	cancelSecond()
	model.PruneCancelledWrites()
	if model.HasPendingWrite() {
		t.Fatal("pending write remains after both proposals were cancelled")
	}
	if err := <-second.ResponseCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("second cancellation response = %v, want context.Canceled", err)
	}
}

// TestAgentInputValue tests InputValue method
func TestAgentInputValue(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	value := model.InputValue()
	if value != "" {
		t.Errorf("Expected empty input value, got %q", value)
	}
}

// TestAgentClearInput tests ClearInput method
func TestAgentClearInput(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.input.SetValue("test")
	model.ClearInput()

	value := model.InputValue()
	if value != "" {
		t.Errorf("Expected empty input after clear, got %q", value)
	}
}

// TestAgentFocus tests Focus method
func TestAgentFocus(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	cmd := model.Focus()
	if cmd == nil {
		t.Error("Expected Focus to return a command")
	}
}

// TestAgentBlur tests Blur method
func TestAgentBlur(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Should not crash
	model.Blur()
}

// TestAgentTaggedFiles tests TaggedFiles method
func TestAgentTaggedFiles(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	files := model.TaggedFiles()
	// taggedFiles is nil initially, which is fine
	if len(files) != 0 {
		t.Errorf("Expected 0 tagged files, got %d", len(files))
	}
}

// TestAgentAddTaggedFile tests AddTaggedFile method
func TestAgentAddTaggedFile(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/test.go")

	files := model.TaggedFiles()
	if len(files) != 1 {
		t.Errorf("Expected 1 tagged file, got %d", len(files))
	}
	if files[0].Path != "/test.go" {
		t.Errorf("Expected Path '/test.go', got %q", files[0].Path)
	}
	if files[0].Name != "test.go" {
		t.Errorf("Expected Name 'test.go', got %q", files[0].Name)
	}
}

// TestAgentAddTaggedFilePreventsDuplicates tests AddTaggedFile prevents duplicates
func TestAgentAddTaggedFilePreventsDuplicates(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/test.go")
	model.AddTaggedFile("/test.go")
	model.AddTaggedFile("/test.go")

	files := model.TaggedFiles()
	if len(files) != 1 {
		t.Errorf("Expected 1 tagged file (no duplicates), got %d", len(files))
	}
}

// TestAgentAddTaggedFileExtractsFilename tests AddTaggedFile extracts filename
func TestAgentAddTaggedFileExtractsFilename(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/very/long/path/to/file.go")

	files := model.TaggedFiles()
	if len(files) != 1 {
		t.Fatalf("Expected 1 file, got %d", len(files))
	}
	if files[0].Name != "file.go" {
		t.Errorf("Expected Name 'file.go', got %q", files[0].Name)
	}
}

// TestAgentRemoveTaggedFile tests RemoveTaggedFile method
func TestAgentRemoveTaggedFile(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/test1.go")
	model.AddTaggedFile("/test2.go")
	model.AddTaggedFile("/test3.go")

	model.RemoveTaggedFile(1) // Remove middle one

	files := model.TaggedFiles()
	if len(files) != 2 {
		t.Errorf("Expected 2 tagged files, got %d", len(files))
	}
}

// TestAgentRemoveTaggedFileWithInvalidIndex tests RemoveTaggedFile with invalid index
func TestAgentRemoveTaggedFileWithInvalidIndex(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/test.go")
	model.RemoveTaggedFile(100) // Out of bounds

	files := model.TaggedFiles()
	if len(files) != 1 {
		t.Errorf("Expected 1 tagged file (no change), got %d", len(files))
	}
}

// TestAgentRemoveTaggedFileWithNegativeIndex tests RemoveTaggedFile with negative index
func TestAgentRemoveTaggedFileWithNegativeIndex(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/test.go")
	model.RemoveTaggedFile(-1) // Negative index

	files := model.TaggedFiles()
	if len(files) != 1 {
		t.Errorf("Expected 1 tagged file (no change), got %d", len(files))
	}
}

// TestAgentClearTaggedFiles tests ClearTaggedFiles method
func TestAgentClearTaggedFiles(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/test1.go")
	model.AddTaggedFile("/test2.go")
	model.ClearTaggedFiles()

	files := model.TaggedFiles()
	if len(files) != 0 {
		t.Errorf("Expected 0 tagged files after clear, got %d", len(files))
	}
}

// TestAgentCurrentModel tests CurrentModel method
func TestAgentCurrentModel(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	modelID := model.CurrentModel()
	// Initially should be empty
	if modelID != "" {
		t.Errorf("Expected empty model ID, got %q", modelID)
	}
}

// TestAgentAvailableModels tests AvailableModels method
func TestAgentAvailableModels(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	models := model.AvailableModels()
	// models is nil initially, which is fine
	if len(models) != 0 {
		t.Errorf("Expected 0 models initially, got %d", len(models))
	}
}

// TestAgentAvailableModes tests AvailableModes method
func TestAgentAvailableModes(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	modes := model.AvailableModes()
	// modes is nil initially, which is fine
	if len(modes) != 0 {
		t.Errorf("Expected 0 modes initially, got %d", len(modes))
	}
}

// TestAgentCurrentMode tests CurrentMode method
func TestAgentCurrentMode(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	modeID := model.CurrentMode()
	// Initially should be empty
	if modeID != "" {
		t.Errorf("Expected empty mode ID, got %q", modeID)
	}
}

func TestAgentModelChangedPreservesModes(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	updated, _ := model.Update(acp.AgentSessionInfoMsg{
		Models: []sdk.ModelInfo{
			{ModelId: "m1", Name: "Model 1"},
			{ModelId: "m2", Name: "Model 2"},
		},
		CurrentModel: "m1",
		Modes: []sdk.SessionMode{
			{Id: "auto", Name: "Auto"},
		},
		CurrentMode: "auto",
	})
	model = updated

	updated, _ = model.Update(acp.AgentModelChangedMsg{ModelId: "m2"})
	model = updated

	if model.currentModel != "m2" {
		t.Fatalf("expected current model to update, got %q", model.currentModel)
	}
	if model.currentMode != "auto" {
		t.Fatalf("expected current mode to be preserved, got %q", model.currentMode)
	}
	if len(model.modes) != 1 || model.modes[0].Id != "auto" {
		t.Fatalf("expected modes to be preserved, got %#v", model.modes)
	}
}

func TestAgentModeChangedPreservesModels(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	updated, _ := model.Update(acp.AgentSessionInfoMsg{
		Models: []sdk.ModelInfo{
			{ModelId: "m1", Name: "Model 1"},
		},
		CurrentModel: "m1",
		Modes: []sdk.SessionMode{
			{Id: "auto", Name: "Auto"},
			{Id: "review", Name: "Review"},
		},
		CurrentMode: "auto",
	})
	model = updated

	updated, _ = model.Update(acp.AgentModeChangedMsg{ModeId: "review"})
	model = updated

	if model.currentMode != "review" {
		t.Fatalf("expected current mode to update, got %q", model.currentMode)
	}
	if model.currentModel != "m1" {
		t.Fatalf("expected current model to be preserved, got %q", model.currentModel)
	}
	if len(model.models) != 1 || model.models[0].ModelId != "m1" {
		t.Fatalf("expected models to be preserved, got %#v", model.models)
	}
}

// TestAgentAddSystemMessage tests AddSystemMessage method
func TestAgentAddSystemMessage(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddSystemMessage("test system message")

	if len(model.messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(model.messages))
	}
	if model.messages[0].Role != RoleSystem {
		t.Errorf("Expected Role RoleSystem, got %d", model.messages[0].Role)
	}
	if model.messages[0].Content != "test system message" {
		t.Errorf("Expected Content 'test system message', got %q", model.messages[0].Content)
	}
}

// TestAgentAddMultipleSystemMessages tests adding multiple system messages
func TestAgentAddMultipleSystemMessages(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddSystemMessage("message 1")
	model.AddSystemMessage("message 2")
	model.AddSystemMessage("message 3")

	if len(model.messages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(model.messages))
	}
}

// TestAgentClearHistory tests ClearHistory method
func TestAgentClearHistory(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add some state
	model.messages = append(model.messages, ChatMessage{Role: RoleUser, Content: "test"})
	model.streamBlocks = append(model.streamBlocks, StreamBlock{Kind: BlockText, Content: "test"})
	model.toolCallMap["test"] = &ToolCallState{}
	model.scrollY = 10
	model.autoScroll = false

	model.ClearHistory()

	if len(model.messages) != 0 {
		t.Errorf("Expected 0 messages after clear, got %d", len(model.messages))
	}
	if len(model.streamBlocks) != 0 {
		t.Errorf("Expected 0 stream blocks after clear, got %d", len(model.streamBlocks))
	}
	if len(model.toolCallMap) != 0 {
		t.Errorf("Expected 0 tool calls after clear, got %d", len(model.toolCallMap))
	}
	if model.scrollY != 0 {
		t.Errorf("Expected scrollY 0 after clear, got %d", model.scrollY)
	}
	if !model.autoScroll {
		t.Error("Expected autoScroll to be true after clear")
	}
}

// TestAgentClearHistoryMultipleTimes tests ClearHistory multiple times
func TestAgentClearHistoryMultipleTimes(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.ClearHistory()
	model.ClearHistory()
	model.ClearHistory()

	// Should not crash
}

// TestAgentInitialState tests initial state
func TestAgentInitialState(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.state != AgentDisconnected {
		t.Errorf("Expected state AgentDisconnected, got %v", model.state)
	}
	if model.loading {
		t.Error("Expected loading to be false")
	}
	if model.connected {
		t.Error("Expected connected to be false")
	}
	if model.scrollY != 0 {
		t.Errorf("Expected scrollY 0, got %d", model.scrollY)
	}
	if model.maxScroll != 0 {
		t.Errorf("Expected maxScroll 0, got %d", model.maxScroll)
	}
}

// TestAgentScrollManagement tests scroll management
func TestAgentScrollManagement(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.scrollY != 0 {
		t.Errorf("Expected initial scrollY 0, got %d", model.scrollY)
	}

	model.scrollY = 10
	if model.scrollY != 10 {
		t.Errorf("Expected scrollY 10, got %d", model.scrollY)
	}
}

// TestAgentMaxScrollManagement tests maxScroll management
func TestAgentMaxScrollManagement(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.maxScroll != 0 {
		t.Errorf("Expected initial maxScroll 0, got %d", model.maxScroll)
	}

	model.maxScroll = 50
	if model.maxScroll != 50 {
		t.Errorf("Expected maxScroll 50, got %d", model.maxScroll)
	}
}

func TestAgentScrollControlsRefreshBoundsFromCurrentContent(t *testing.T) {
	tests := []struct {
		name       string
		msg        tea.Msg
		wantScroll func(maxScroll, pageHeight int) int
	}{
		{
			name: "wheel down",
			msg:  tea.MouseWheelMsg{Button: tea.MouseWheelDown},
			wantScroll: func(maxScroll, _ int) int {
				return min(3, maxScroll)
			},
		},
		{
			name: "page down",
			msg:  tea.KeyPressMsg{Text: "pgdown"},
			wantScroll: func(maxScroll, pageHeight int) int {
				return min(pageHeight, maxScroll)
			},
		},
		{
			name: "end",
			msg:  tea.KeyPressMsg{Text: "end"},
			wantScroll: func(maxScroll, _ int) int {
				return maxScroll
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := New(ui.DefaultTheme())
			model.SetSize(14, 8)
			model.autoScroll = false
			for range 12 {
				model.messages = append(model.messages, ChatMessage{
					Role:    RoleUser,
					Content: "one two three four five six seven eight nine ten",
				})
			}
			_ = model.View()    // The root View persists rendered lines through chatCache's shared pointer.
			model.maxScroll = 0 // Simulate View having rendered a stale copy of the model.

			updated, _ := model.Update(tt.msg)
			chatHeight := model.height - 3
			wantMax := len(model.buildChatLines(model.width)) - chatHeight
			if wantMax < 0 {
				wantMax = 0
			}
			if wantMax == 0 {
				t.Fatal("test setup must require scrolling")
			}
			if updated.maxScroll != wantMax {
				t.Errorf("maxScroll = %d, want %d", updated.maxScroll, wantMax)
			}
			if want := tt.wantScroll(wantMax, chatHeight); updated.scrollY != want {
				t.Errorf("scrollY = %d, want %d", updated.scrollY, want)
			}
		})
	}
}

func TestAgentScrollControlDoesNotRenderDirtyHistoryInUpdate(t *testing.T) {
	model := New(ui.DefaultTheme())
	model.SetConnected(true)
	model.SetSize(48, 20)
	for range maxChatMessages {
		model.AddSystemMessage("bounded history that must not be rendered by a scroll event")
	}
	_ = model.View()
	cache := model.chatCache
	if cache == nil || cache.fullBuilds != 1 || cache.dirty {
		t.Fatalf("initial cache = %#v, want one completed render", cache)
	}

	model.AddSystemMessage("new message invalidates the transcript")
	if !cache.dirty {
		t.Fatal("new message did not invalidate the transcript cache")
	}
	updated, _ := model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if cache.fullBuilds != 1 {
		t.Fatalf("scroll event rebuilt full history inside Update: builds=%d", cache.fullBuilds)
	}
	if !cache.dirty {
		t.Fatal("scroll event unexpectedly marked the dirty transcript as rendered")
	}

	_ = updated.View()
	if cache.fullBuilds != 2 || cache.dirty {
		t.Fatalf("View did not perform the deferred rebuild: builds=%d dirty=%v", cache.fullBuilds, cache.dirty)
	}
}

// TestAgentLoadingToggle tests loading toggle
func TestAgentLoadingToggle(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.loading {
		t.Error("Expected loading to be false initially")
	}

	model.loading = true
	if !model.loading {
		t.Error("Expected loading to be true")
	}

	model.loading = false
	if model.loading {
		t.Error("Expected loading to be false")
	}
}

// TestAgentConnectedToggle tests connected toggle
func TestAgentConnectedToggle(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.connected {
		t.Error("Expected connected to be false initially")
	}

	model.connected = true
	if !model.connected {
		t.Error("Expected connected to be true")
	}

	model.connected = false
	if model.connected {
		t.Error("Expected connected to be false")
	}
}

// TestAgentAutoScrollToggle tests autoScroll toggle
func TestAgentAutoScrollToggle(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if !model.autoScroll {
		t.Error("Expected autoScroll to be true initially")
	}

	model.autoScroll = false
	if model.autoScroll {
		t.Error("Expected autoScroll to be false")
	}

	model.autoScroll = true
	if !model.autoScroll {
		t.Error("Expected autoScroll to be true")
	}
}

// TestAgentStateTransitions tests state transitions
func TestAgentStateTransitions(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Initial state
	if model.State() != AgentDisconnected {
		t.Errorf("Expected AgentDisconnected, got %v", model.State())
	}

	// Transition to Idle
	model.state = AgentIdle
	if model.State() != AgentIdle {
		t.Errorf("Expected AgentIdle, got %v", model.State())
	}

	// Transition to Thinking
	model.state = AgentThinking
	if model.State() != AgentThinking {
		t.Errorf("Expected AgentThinking, got %v", model.State())
	}

	// Transition to Permission
	model.state = AgentPermission
	if model.State() != AgentPermission {
		t.Errorf("Expected AgentPermission, got %v", model.State())
	}
}

// TestAgentAllStates tests all agent states
func TestAgentAllStates(t *testing.T) {
	states := []AgentState{AgentDisconnected, AgentIdle, AgentThinking, AgentPermission}

	for i, state := range states {
		_ = state
		// Just verify states exist and are different
		if i > 0 && state == states[i-1] {
			t.Errorf("Expected state %d to be different from state %d", i, i-1)
		}
	}
}

// TestAgentStateValues tests AgentState values
func TestAgentStateValues(t *testing.T) {
	if AgentDisconnected != 0 {
		t.Errorf("Expected AgentDisconnected 0, got %d", AgentDisconnected)
	}
	if AgentIdle != 1 {
		t.Errorf("Expected AgentIdle 1, got %d", AgentIdle)
	}
	if AgentThinking != 2 {
		t.Errorf("Expected AgentThinking 2, got %d", AgentThinking)
	}
	if AgentPermission != 3 {
		t.Errorf("Expected AgentPermission 3, got %d", AgentPermission)
	}
}

// TestAgentChatRoleValues tests ChatRole values
func TestAgentChatRoleValues(t *testing.T) {
	if RoleUser != 0 {
		t.Errorf("Expected RoleUser 0, got %d", RoleUser)
	}
	if RoleAgent != 1 {
		t.Errorf("Expected RoleAgent 1, got %d", RoleAgent)
	}
	if RoleSystem != 2 {
		t.Errorf("Expected RoleSystem 2, got %d", RoleSystem)
	}
}

// TestAgentStreamBlockKindValues tests StreamBlockKind values
func TestAgentStreamBlockKindValues(t *testing.T) {
	if BlockText != 0 {
		t.Errorf("Expected BlockText 0, got %d", BlockText)
	}
	if BlockThought != 1 {
		t.Errorf("Expected BlockThought 1, got %d", BlockThought)
	}
	if BlockToolCall != 2 {
		t.Errorf("Expected BlockToolCall 2, got %d", BlockToolCall)
	}
}

// TestAgentMessagesSlice tests messages slice operations
func TestAgentMessagesSlice(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add messages
	model.messages = append(model.messages, ChatMessage{Role: RoleUser, Content: "msg1"})
	model.messages = append(model.messages, ChatMessage{Role: RoleAgent, Content: "msg2"})

	if len(model.messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(model.messages))
	}

	// Access message
	if model.messages[0].Content != "msg1" {
		t.Errorf("Expected messages[0].Content = 'msg1', got %q", model.messages[0].Content)
	}
}

// TestAgentStreamBlocksSlice tests streamBlocks slice operations
func TestAgentStreamBlocksSlice(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add stream blocks
	model.streamBlocks = append(model.streamBlocks, StreamBlock{Kind: BlockText, Content: "block1"})
	model.streamBlocks = append(model.streamBlocks, StreamBlock{Kind: BlockThought, Content: "block2"})

	if len(model.streamBlocks) != 2 {
		t.Errorf("Expected 2 stream blocks, got %d", len(model.streamBlocks))
	}

	// Access stream block
	if model.streamBlocks[0].Content != "block1" {
		t.Errorf("Expected streamBlocks[0].Content = 'block1', got %q", model.streamBlocks[0].Content)
	}
}

// TestAgentToolCallMapOperations tests toolCallMap operations
func TestAgentToolCallMapOperations(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add tool call
	model.toolCallMap["test-id"] = &ToolCallState{Title: "Test Tool"}
	if len(model.toolCallMap) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(model.toolCallMap))
	}

	// Access tool call
	if model.toolCallMap["test-id"].Title != "Test Tool" {
		t.Errorf("Expected toolCallMap['test-id'].Title = 'Test Tool', got %q", model.toolCallMap["test-id"].Title)
	}

	// Remove tool call
	delete(model.toolCallMap, "test-id")
	if len(model.toolCallMap) != 0 {
		t.Errorf("Expected 0 tool calls after delete, got %d", len(model.toolCallMap))
	}
}

// TestAgentTaggedFilesSlice tests taggedFiles slice operations
func TestAgentTaggedFilesSlice(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add tagged files
	model.taggedFiles = append(model.taggedFiles, TaggedFile{Path: "/test1.go", Name: "test1.go"})
	model.taggedFiles = append(model.taggedFiles, TaggedFile{Path: "/test2.go", Name: "test2.go"})

	if len(model.taggedFiles) != 2 {
		t.Errorf("Expected 2 tagged files, got %d", len(model.taggedFiles))
	}

	// Access tagged file
	if model.taggedFiles[0].Name != "test1.go" {
		t.Errorf("Expected taggedFiles[0].Name = 'test1.go', got %q", model.taggedFiles[0].Name)
	}
}

// TestAgentAlwaysAllowMapOperations tests alwaysAllow map operations
func TestAgentAlwaysAllowMapOperations(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add entry
	model.alwaysAllow["test"] = true
	if !model.alwaysAllow["test"] {
		t.Error("Expected alwaysAllow['test'] to be true")
	}

	// Remove entry
	delete(model.alwaysAllow, "test")
	if _, ok := model.alwaysAllow["test"]; ok {
		t.Error("Expected alwaysAllow['test'] to be deleted")
	}
}

// TestAgentSpinnerInitialized tests spinner initialization
func TestAgentSpinnerInitialized(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Spinner should be initialized
	_ = model.spinner
}

// TestAgentInputInitialized tests input initialization
func TestAgentInputInitialized(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Input should be initialized
	_ = model.input
}

// TestAgentLastChatLineCount tests lastChatLineCount field
func TestAgentLastChatLineCount(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.lastChatLineCount != 0 {
		t.Errorf("Expected lastChatLineCount 0, got %d", model.lastChatLineCount)
	}

	model.lastChatLineCount = 50
	if model.lastChatLineCount != 50 {
		t.Errorf("Expected lastChatLineCount 50, got %d", model.lastChatLineCount)
	}
}

// TestAgentSpinFrame tests spinFrame field
func TestAgentSpinFrame(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.spinFrame != 0 {
		t.Errorf("Expected spinFrame 0, got %d", model.spinFrame)
	}

	model.spinFrame = 5
	if model.spinFrame != 5 {
		t.Errorf("Expected spinFrame 5, got %d", model.spinFrame)
	}
}

// TestAgentLastEscTime tests lastEscTime field
func TestAgentLastEscTime(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if !model.lastEscTime.IsZero() {
		t.Error("Expected lastEscTime to be zero")
	}

	model.lastEscTime = time.Now()
	if model.lastEscTime.IsZero() {
		t.Error("Expected lastEscTime to be set")
	}
}

// TestAgentWidthHeight tests width and height fields
func TestAgentWidthHeight(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	if model.width != 0 {
		t.Errorf("Expected width 0, got %d", model.width)
	}
	if model.height != 0 {
		t.Errorf("Expected height 0, got %d", model.height)
	}

	model.width = 100
	model.height = 30
	if model.width != 100 {
		t.Errorf("Expected width 100, got %d", model.width)
	}
	if model.height != 30 {
		t.Errorf("Expected height 30, got %d", model.height)
	}
}

// TestAgentScrollYMaxScroll tests scrollY and maxScroll relationship
func TestAgentScrollYMaxScroll(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.maxScroll = 50
	model.scrollY = 100

	// scrollY can exceed maxScroll (will be clamped in rendering)
	if model.scrollY != 100 {
		t.Errorf("Expected scrollY 100, got %d", model.scrollY)
	}
}

// TestAgentMessagesWithDifferentRoles tests messages with different roles
func TestAgentMessagesWithDifferentRoles(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.messages = append(model.messages, ChatMessage{Role: RoleUser, Content: "user"})
	model.messages = append(model.messages, ChatMessage{Role: RoleAgent, Content: "agent"})
	model.messages = append(model.messages, ChatMessage{Role: RoleSystem, Content: "system"})

	if len(model.messages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(model.messages))
	}

	// Verify roles
	expectedRoles := []ChatRole{RoleUser, RoleAgent, RoleSystem}
	for i, expected := range expectedRoles {
		if model.messages[i].Role != expected {
			t.Errorf("Expected message %d role %d, got %d", i, expected, model.messages[i].Role)
		}
	}
}

// TestAgentStreamBlocksWithDifferentKinds tests streamBlocks with different kinds
func TestAgentStreamBlocksWithDifferentKinds(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.streamBlocks = append(model.streamBlocks, StreamBlock{Kind: BlockText, Content: "text"})
	model.streamBlocks = append(model.streamBlocks, StreamBlock{Kind: BlockThought, Content: "thought"})
	model.streamBlocks = append(model.streamBlocks, StreamBlock{Kind: BlockToolCall, Content: "tool"})

	if len(model.streamBlocks) != 3 {
		t.Errorf("Expected 3 stream blocks, got %d", len(model.streamBlocks))
	}

	// Verify kinds
	expectedKinds := []StreamBlockKind{BlockText, BlockThought, BlockToolCall}
	for i, expected := range expectedKinds {
		if model.streamBlocks[i].Kind != expected {
			t.Errorf("Expected stream block %d kind %d, got %d", i, expected, model.streamBlocks[i].Kind)
		}
	}
}

// TestAgentTaggedFilesWithMultipleFiles tests taggedFiles with multiple files
func TestAgentTaggedFilesWithMultipleFiles(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	files := []string{"/test1.go", "/test2.go", "/test3.go"}
	for _, f := range files {
		model.AddTaggedFile(f)
	}

	taggedFiles := model.TaggedFiles()
	if len(taggedFiles) != 3 {
		t.Errorf("Expected 3 tagged files, got %d", len(taggedFiles))
	}

	// Verify all files are present
	for i, expected := range files {
		if taggedFiles[i].Path != expected {
			t.Errorf("Expected tagged file %d path %q, got %q", i, expected, taggedFiles[i].Path)
		}
	}
}

// TestAgentToolCallMapWithMultipleEntries tests toolCallMap with multiple entries
func TestAgentToolCallMapWithMultipleEntries(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add multiple tool calls
	model.toolCallMap["id1"] = &ToolCallState{Title: "Tool 1"}
	model.toolCallMap["id2"] = &ToolCallState{Title: "Tool 2"}
	model.toolCallMap["id3"] = &ToolCallState{Title: "Tool 3"}

	if len(model.toolCallMap) != 3 {
		t.Errorf("Expected 3 tool calls, got %d", len(model.toolCallMap))
	}
}

// TestAgentAlwaysAllowWithMultipleEntries tests alwaysAllow with multiple entries
func TestAgentAlwaysAllowWithMultipleEntries(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add multiple entries
	model.alwaysAllow["tool1"] = true
	model.alwaysAllow["tool2"] = true
	model.alwaysAllow["tool3"] = true

	if len(model.alwaysAllow) != 3 {
		t.Errorf("Expected 3 alwaysAllow entries, got %d", len(model.alwaysAllow))
	}
}

// TestAgentClearHistoryPreservesToolCallMapInitialization tests ClearHistory preserves toolCallMap initialization
func TestAgentClearHistoryPreservesToolCallMapInitialization(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Clear history
	model.ClearHistory()

	// toolCallMap should still be initialized (not nil)
	if model.toolCallMap == nil {
		t.Error("Expected toolCallMap to be initialized after ClearHistory")
	}
	if len(model.toolCallMap) != 0 {
		t.Errorf("Expected 0 tool calls after ClearHistory, got %d", len(model.toolCallMap))
	}
}

// TestAgentAddTaggedFileWithEmptyPath tests AddTaggedFile with empty path
func TestAgentAddTaggedFileWithEmptyPath(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("")

	files := model.TaggedFiles()
	if len(files) != 1 {
		t.Errorf("Expected 1 file, got %d", len(files))
	}
	if files[0].Path != "" {
		t.Errorf("Expected empty Path, got %q", files[0].Path)
	}
}

// TestAgentRemoveTaggedFilePreservesOtherFiles tests RemoveTaggedFile preserves other files
func TestAgentRemoveTaggedFilePreservesOtherFiles(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	model.AddTaggedFile("/test1.go")
	model.AddTaggedFile("/test2.go")
	model.AddTaggedFile("/test3.go")

	model.RemoveTaggedFile(1) // Remove middle one

	files := model.TaggedFiles()
	if len(files) != 2 {
		t.Fatalf("Expected 2 files, got %d", len(files))
	}
	if files[0].Path != "/test1.go" {
		t.Errorf("Expected first file '/test1.go', got %q", files[0].Path)
	}
	if files[1].Path != "/test3.go" {
		t.Errorf("Expected second file '/test3.go', got %q", files[1].Path)
	}
}

// TestAgentSetConnectedPreservesOtherState tests SetConnected preserves other state
func TestAgentSetConnectedPreservesOtherState(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Set some state
	model.loading = true
	model.scrollY = 10
	model.autoScroll = false

	// Change connected
	model.SetConnected(true)

	// Other state should be preserved
	if !model.loading {
		t.Error("Expected loading to be preserved")
	}
	if model.scrollY != 10 {
		t.Errorf("Expected scrollY 10, got %d", model.scrollY)
	}
	if model.autoScroll {
		t.Error("Expected autoScroll to be preserved")
	}
}

// TestAgentAddSystemMessagePreservesOtherMessages tests AddSystemMessage preserves other messages
func TestAgentAddSystemMessagePreservesOtherMessages(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Add user message
	model.messages = append(model.messages, ChatMessage{Role: RoleUser, Content: "user"})

	// Add system message
	model.AddSystemMessage("system")

	if len(model.messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(model.messages))
	}
	if model.messages[0].Role != RoleUser {
		t.Error("Expected first message to be preserved")
	}
}

// TestAgentClearInputPreservesOtherState tests ClearInput preserves other state
func TestAgentClearInputPreservesOtherState(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Set some state
	model.loading = true
	model.scrollY = 10
	model.connected = true

	// Clear input
	model.ClearInput()

	// Other state should be preserved
	if !model.loading {
		t.Error("Expected loading to be preserved")
	}
	if model.scrollY != 10 {
		t.Errorf("Expected scrollY 10, got %d", model.scrollY)
	}
	if !model.connected {
		t.Error("Expected connected to be preserved")
	}
}

// TestAgentFieldAccess tests all Model fields are accessible
func TestAgentFieldAccess(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Access all fields
	_ = model.width
	_ = model.height
	_ = model.theme
	_ = model.messages
	_ = model.streamBlocks
	_ = model.toolCallMap
	_ = model.input
	_ = model.scrollY
	_ = model.maxScroll
	_ = model.loading
	_ = model.connected
	_ = model.state
	_ = model.permission
	_ = model.alwaysAllow
	_ = model.pendingWrite
	_ = model.pendingWrites
	_ = model.spinner
	_ = model.spinFrame
	_ = model.lastEscTime
	_ = model.autoScroll
	_ = model.models
	_ = model.currentModel
	_ = model.modes
	_ = model.currentMode
	_ = model.taggedFiles
	_ = model.lastChatLineCount
}

// TestAgentAllTypesExist tests that all agent types exist
func TestAgentAllTypesExist(t *testing.T) {
	// Just verify we can create instances of all types
	_ = ChatMessage{}
	_ = StreamBlock{}
	_ = ToolCallState{}
	_ = PermissionPrompt{}
	_ = TaggedFile{}
	_ = Model{}
}

// TestAgentAllConstantsExist tests that all agent constants exist
func TestAgentAllConstantsExist(t *testing.T) {
	// Just verify we can use all constants
	_ = RoleUser
	_ = RoleAgent
	_ = RoleSystem
	_ = BlockText
	_ = BlockThought
	_ = BlockToolCall
	_ = AgentDisconnected
	_ = AgentIdle
	_ = AgentThinking
	_ = AgentPermission
}

// TestAgentMaxToolOutputLines tests maxToolOutputLines constant
func TestAgentMaxToolOutputLines(t *testing.T) {
	if maxToolOutputLines != 100 {
		t.Errorf("Expected maxToolOutputLines 100, got %d", maxToolOutputLines)
	}
}

// TestAgentTypesCompile tests that all agent types compile
func TestAgentTypesCompile(t *testing.T) {
	// This test just verifies all types compile correctly
	var _ ChatMessage
	var _ StreamBlock
	var _ ToolCallState
	var _ PermissionPrompt
	var _ TaggedFile
	var _ Model
	var _ ChatRole
	var _ StreamBlockKind
	var _ AgentState
}

// TestAgentConstantsCompile tests that all agent constants compile
func TestAgentConstantsCompile(t *testing.T) {
	// This test just verifies all constants compile correctly
	var _ = RoleUser
	var _ = BlockText
	var _ = AgentDisconnected
	var _ = maxToolOutputLines
}

// TestAgentMethodsExist tests that all Model methods exist
func TestAgentMethodsExist(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(theme)

	// Verify methods exist and are callable
	_ = model.IsLoading()
	model.SetSize(100, 30)
	model.SetConnected(true)
	_ = model.State()
	_ = model.HasPermissionPending()
	_ = model.HasPendingWrite()
	_ = model.PendingWrite()
	model.AcceptWrite()
	model.RejectWrite()
	_ = model.InputValue()
	model.ClearInput()
	_ = model.Focus()
	model.Blur()
	_ = model.TaggedFiles()
	model.AddTaggedFile("/test.go")
	model.RemoveTaggedFile(0)
	model.ClearTaggedFiles()
	_ = model.CurrentModel()
	_ = model.AvailableModels()
	_ = model.AvailableModes()
	_ = model.CurrentMode()
	model.AddSystemMessage("test")
	model.ClearHistory()
}

func TestAgentViewAndInputFocusAcrossConnectionStates(t *testing.T) {
	model := New(ui.DefaultTheme())
	model.SetSize(40, 30)
	if view := model.View(); !strings.Contains(view, "Agent not connected") {
		t.Fatalf("disconnected view = %q, want connection hint", view)
	}
	if model.IsInputFocused() {
		t.Fatal("new agent input should not be focused")
	}
	if cmd := model.Focus(); cmd == nil || !model.IsInputFocused() {
		t.Fatal("Focus() did not focus the input")
	}
	model.Blur()
	if model.IsInputFocused() {
		t.Fatal("Blur() left the input focused")
	}

	model.SetConnected(true)
	model.currentModel = sdk.ModelId(strings.Repeat("model-", 10))
	model.loading = true
	model.AddSystemMessage("system message")
	model.permission = &PermissionPrompt{
		ToolCall:   sdk.RequestPermissionToolCall{},
		Options:    []sdk.PermissionOption{{Kind: sdk.PermissionOptionKindAllowOnce, OptionId: "once"}},
		ResponseCh: make(chan sdk.RequestPermissionResponse, 1),
	}
	model.pendingWrite = &acp.AgentWriteFileMsg{Path: "main.go", Content: "package main\n"}
	view := model.View()
	for _, want := range []string{"Agent", "system message", "Agent wants to", "Edit proposal"} {
		if !strings.Contains(view, want) {
			t.Errorf("connected view missing %q: %q", want, view)
		}
	}

	model.permission = nil
	model.pendingWrite = nil
	model.loading = false
	for _, state := range []AgentState{AgentDisconnected, AgentIdle, AgentThinking, AgentPermission} {
		model.state = state
		if got := model.View(); got == "" {
			t.Fatalf("View() for state %v is empty", state)
		}
	}
}

func TestAgentLastVisibleToolCallPrefersStream(t *testing.T) {
	model := New(ui.DefaultTheme())
	completed := &ToolCallState{ID: "completed", Title: "done"}
	streaming := &ToolCallState{ID: "streaming", Title: "active"}
	model.messages = []ChatMessage{{ToolCalls: []*ToolCallState{completed}}}
	if got := model.lastVisibleToolCall(); got != completed {
		t.Fatalf("lastVisibleToolCall() = %#v, want completed message call", got)
	}
	model.streamBlocks = []StreamBlock{{Kind: BlockToolCall, ToolCall: streaming}}
	if got := model.lastVisibleToolCall(); got != streaming {
		t.Fatalf("lastVisibleToolCall() = %#v, want streaming call", got)
	}
}

// TestAgentAllFieldsHaveZeroValues tests that all fields have proper zero values
func TestAgentAllFieldsHaveZeroValues(t *testing.T) {
	var model Model

	if model.width != 0 {
		t.Errorf("Expected zero width 0, got %d", model.width)
	}
	if model.height != 0 {
		t.Errorf("Expected zero height 0, got %d", model.height)
	}
	if model.messages != nil {
		t.Error("Expected zero messages to be nil")
	}
	if model.streamBlocks != nil {
		t.Error("Expected zero streamBlocks to be nil")
	}
	if model.toolCallMap != nil {
		t.Error("Expected zero toolCallMap to be nil")
	}
	if model.scrollY != 0 {
		t.Errorf("Expected zero scrollY 0, got %d", model.scrollY)
	}
	if model.maxScroll != 0 {
		t.Errorf("Expected zero maxScroll 0, got %d", model.maxScroll)
	}
	if model.loading {
		t.Error("Expected zero loading to be false")
	}
	if model.connected {
		t.Error("Expected zero connected to be false")
	}
	if model.state != 0 {
		t.Errorf("Expected zero state 0, got %d", model.state)
	}
	if model.permission != nil {
		t.Error("Expected zero permission to be nil")
	}
	if model.alwaysAllow != nil {
		t.Error("Expected zero alwaysAllow to be nil")
	}
	if model.pendingWrite != nil {
		t.Error("Expected zero pendingWrite to be nil")
	}
	if model.pendingWrites != nil {
		t.Error("Expected zero pendingWrites to be nil")
	}
	if model.spinFrame != 0 {
		t.Errorf("Expected zero spinFrame 0, got %d", model.spinFrame)
	}
	if !model.lastEscTime.IsZero() {
		t.Error("Expected zero lastEscTime to be zero")
	}
	if model.autoScroll {
		t.Error("Expected zero autoScroll to be false")
	}
	if model.models != nil {
		t.Error("Expected zero models to be nil")
	}
	if model.taggedFiles != nil {
		t.Error("Expected zero taggedFiles to be nil")
	}
	if model.lastChatLineCount != 0 {
		t.Errorf("Expected zero lastChatLineCount 0, got %d", model.lastChatLineCount)
	}
}
