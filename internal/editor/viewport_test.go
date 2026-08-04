package editor

import (
	"bytes"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/highlight"
	"teak/internal/text"
	"teak/internal/ui"
)

func TestViewportRender(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	theme := ui.DefaultTheme()

	viewport := Viewport{
		Width:  80,
		Height: 24,
	}

	result := viewport.Render(buf, theme, nil, nil, nil)
	if result == "" {
		t.Error("expected non-empty render result")
	}
}

func TestViewportRenderWithGutter(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello\nworld\ntest"))
	theme := ui.DefaultTheme()

	viewport := Viewport{
		Width:  80,
		Height: 10,
	}

	result := viewport.Render(buf, theme, nil, nil, nil)
	lines := splitLines(result)
	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d", len(lines))
	}
}

func TestViewportRenderWithScroll(t *testing.T) {
	content := ""
	for i := 1; i <= 50; i++ {
		content += "line " + string(rune('0'+i)) + "\n"
	}
	buf := text.NewBufferFromBytes([]byte(content))
	theme := ui.DefaultTheme()

	viewport := Viewport{
		Width:   80,
		Height:  10,
		ScrollY: 5,
	}

	result := viewport.Render(buf, theme, nil, nil, nil)
	if result == "" {
		t.Error("expected non-empty render result")
	}
}

func TestViewportRenderWithSyntaxHighlighting(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("package main"))
	buf.FilePath = "test.go"
	theme := ui.DefaultTheme()

	hl := highlight.New("test.go", theme)
	hl.TokenizePrefix(buf.Bytes(), 60)

	viewport := Viewport{
		Width:  80,
		Height: 10,
	}

	result := viewport.Render(buf, theme, hl, nil, nil)
	if result == "" {
		t.Error("expected non-empty render result")
	}
}

func TestViewportRenderWithDiagnostics(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello\nworld\ntest"))
	theme := ui.DefaultTheme()

	diagnostics := []Diagnostic{
		{StartLine: 0, EndLine: 0, Severity: 1, Message: "error"},
		{StartLine: 1, EndLine: 1, Severity: 2, Message: "warning"},
	}

	viewport := Viewport{
		Width:  80,
		Height: 10,
	}

	result := viewport.Render(buf, theme, nil, diagnostics, nil)
	if result == "" {
		t.Error("expected non-empty render result")
	}
}

func TestViewportRenderWithSelection(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	theme := ui.DefaultTheme()
	buf.SetSelection(text.Position{Line: 0, Col: 0}, text.Position{Line: 0, Col: 5})

	viewport := Viewport{
		Width:  80,
		Height: 10,
	}

	result := viewport.Render(buf, theme, nil, nil, nil)
	if result == "" {
		t.Error("expected non-empty render result")
	}
}

func TestViewportRenderWithWrapHighlightsSelection(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		anchor   text.Position
		head     text.Position
		width    int
		selected string
	}{
		{
			name:     "selection starts on first wrapped row",
			content:  "abcdefgh",
			anchor:   text.Position{Line: 0, Col: 0},
			head:     text.Position{Line: 0, Col: 4},
			width:    3,
			selected: "abc",
		},
		{
			name:     "selection continues onto later wrapped row",
			content:  "abcdefgh",
			anchor:   text.Position{Line: 0, Col: 1},
			head:     text.Position{Line: 0, Col: 6},
			width:    3,
			selected: "def",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := text.NewBufferFromBytes([]byte(tt.content))
			buf.SetSelection(tt.anchor, tt.head)
			theme := ui.DefaultTheme()
			wrap := NewWrapLayout(buf.Line, buf.LineCount(), tt.width)
			viewport := Viewport{Width: 20, Height: 4}

			got := viewport.RenderWithWrap(buf, theme, nil, nil, nil, wrap)
			if want := theme.Selection.Render(tt.selected); !strings.Contains(got, want) {
				t.Errorf("wrapped selection rendering does not contain selection-styled %q:\n%s", tt.selected, got)
			}
		})
	}
}

func TestWrapSegmentBoundsKeepsWideRunesWithinRows(t *testing.T) {
	tests := []struct {
		name      string
		segment   int
		wantText  string
		wantStart int
		wantEnd   int
	}{
		{name: "first narrow rune", segment: 0, wantText: "a", wantStart: 0, wantEnd: 1},
		{name: "wide rune moves to next row", segment: 1, wantText: "你", wantStart: 1, wantEnd: 4},
		{name: "trailing narrow rune", segment: 2, wantText: "a", wantStart: 4, wantEnd: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotText, gotStart, gotEnd := wrapSegmentBounds("a你a", tt.segment, 2)
			if gotText != tt.wantText || gotStart != tt.wantStart || gotEnd != tt.wantEnd {
				t.Errorf(
					"wrapSegmentBounds(segment=%d) = (%q,%d,%d), want (%q,%d,%d)",
					tt.segment,
					gotText,
					gotStart,
					gotEnd,
					tt.wantText,
					tt.wantStart,
					tt.wantEnd,
				)
			}
		})
	}
}

func TestWrapLayoutCountsRowsWithoutSplittingWideRunes(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("a你a"))
	wrap := NewWrapLayout(buf.Line, buf.LineCount(), 2)

	if got := wrap.LineRows(0); got != 3 {
		t.Errorf("LineRows(0) = %d, want 3", got)
	}
}

func TestWrapLayoutWindowsFormerSegmentBudget(t *testing.T) {
	line := bytes.Repeat([]byte("x"), largeWrapTestSegments+1)
	wrap := NewWrapLayout(func(int) []byte { return line }, 1, 1)
	if wrap.Degraded() {
		t.Fatal("pathological one-column line must stay wrapped")
	}
	if got := wrap.LineRows(0); got != len(line) {
		t.Fatalf("wrapped rows = %d, want %d", got, len(line))
	}
	if got := len(wrap.segmentStarts); got != 0 {
		t.Fatalf("new layout eagerly retained %d segment entries", got)
	}
	if _, _, _, ok := wrap.SegmentBounds(0, len(line)-1, len(line)); !ok {
		t.Fatal("deep segment must remain addressable")
	}
	if got := len(wrap.segmentStarts); got > maxWrapSegmentWindow+1 {
		t.Fatalf("active segment window = %d entries, want at most %d", got, maxWrapSegmentWindow+1)
	}
}

func TestWrapLayoutSupportsFormerLineBudget(t *testing.T) {
	wrap := NewWrapLayout(func(int) []byte { return nil }, largeWrapTestLines+1, 80)
	if wrap.Degraded() {
		t.Fatal("large line count must not disable word wrap")
	}
	if got, want := wrap.TotalRows(), largeWrapTestLines+1; got != want {
		t.Fatalf("TotalRows() = %d, want %d", got, want)
	}
	if line, segment := wrap.BufferLine(largeWrapTestLines); line != largeWrapTestLines || segment != 0 {
		t.Fatalf("BufferLine(last) = (%d, %d), want (%d, 0)", line, segment, largeWrapTestLines)
	}
}

func TestWrapLayoutCachesDeepSegmentBounds(t *testing.T) {
	line := strings.Repeat("ab", 8_000) + "\t你" + string([]byte{0xff})
	buf := text.NewBufferFromBytes([]byte(line))
	wrap := NewWrapLayoutWithTabSize(buf.Line, buf.LineCount(), 7, 4)
	segment := wrap.LineRows(0) - 2

	start, end, displayStart, ok := wrap.SegmentBounds(0, segment, len(line))
	if !ok {
		t.Fatalf("SegmentBounds(%d) reported no segment", segment)
	}
	wantStart, wantEnd, _, wantOK := wrappedLineSegmentWithTabs(line, segment, 7, 4)
	if !wantOK || start != wantStart || end != wantEnd {
		t.Fatalf("SegmentBounds(%d) = (%d,%d), want (%d,%d)", segment, start, end, wantStart, wantEnd)
	}
	if got := displayColumn([]byte(line), start, 4); displayStart != got {
		t.Fatalf("segment display start = %d, want %d", displayStart, got)
	}
}

func TestWrapLayoutPositionForByteMatchesPackedTabsWideRunesAndInvalidUTF8(t *testing.T) {
	line := "a\t你b" + string([]byte{0xff}) + "c"
	buf := text.NewBufferFromBytes([]byte(line))
	wrap := NewWrapLayoutWithTabSize(buf.Line, buf.LineCount(), 4, 4)

	for _, col := range []int{0, 1, 2, 5, 6, 7, len(line)} {
		wantRow, wantCol := wrappedPositionWithTabs(line, col, 4, 4)
		gotRow, gotCol := wrap.PositionForByte(0, col, []byte(line))
		if gotRow != wantRow || gotCol != wantCol {
			t.Errorf("PositionForByte(%d) = (%d,%d), want (%d,%d)", col, gotRow, gotCol, wantRow, wantCol)
		}
	}
}

func TestViewportWrapScrollsWithinSingleLongLine(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("abcdefghijklmnopqrstuvwx"))
	wrap := NewWrapLayout(buf.Line, buf.LineCount(), 4)
	viewport := Viewport{Width: 12, Height: 2, WrapScrollY: 3}

	rendered := ansi.Strip(viewport.RenderWithWrap(buf, ui.DefaultTheme(), nil, nil, nil, wrap))
	if !strings.Contains(rendered, "mnop") || strings.Contains(rendered, "abcd") {
		t.Fatalf("RenderWithWrap() did not start at visual row 3:\n%s", rendered)
	}
	if got := viewport.ScreenToBufferPositionWrap(0, 0, buf, 0, wrap); got != (text.Position{Line: 0, Col: 12}) {
		t.Fatalf("wrapped click after visual scroll = %#v, want byte column 12", got)
	}
}

func TestRenderTokenByteRangePreservesTokenStyles(t *testing.T) {
	bold := lipgloss.NewStyle().Bold(true)
	italic := lipgloss.NewStyle().Italic(true)
	tokens := []highlight.StyledToken{
		{Text: "abc", Style: bold},
		{Text: "def", Style: italic},
	}

	got := renderTokenByteRange("abcdef", tokens, 1, 5, lipgloss.NewStyle())
	want := bold.Render("bc") + italic.Render("de")
	if got != want {
		t.Errorf("renderTokenByteRange() = %q, want %q", got, want)
	}
}

func TestRenderTokenByteRangeWithIndexedStartsPreservesDeepStyle(t *testing.T) {
	line := strings.Repeat("a", 128) + "XY"
	plain := lipgloss.NewStyle()
	bold := lipgloss.NewStyle().Bold(true)
	tokens := []highlight.StyledToken{
		{Text: strings.Repeat("a", 128), Style: plain},
		{Text: "XY", Style: bold},
	}
	starts := tokenByteStarts(tokens)
	got, _ := renderTokenByteRangeWithTabsAtDisplayIndexed(line, tokens, starts, 128, len(line), 128, plain, 4)
	if want := bold.Render("XY"); got != want {
		t.Fatalf("indexed token rendering = %q, want %q", got, want)
	}
}

func TestStyleRenderPairForThemeRows(t *testing.T) {
	for name, style := range map[string]lipgloss.Style{
		"editor":      ui.DefaultTheme().Editor,
		"cursor line": ui.DefaultTheme().CursorLine,
	} {
		_, _, ok := styleRenderPair(style)
		if !ok {
			t.Fatalf("styleRenderPair(%s) did not recognize a theme row style", name)
		}
	}
}

func TestWrappedTokenFastPathMatchesLegacyCells(t *testing.T) {
	theme := ui.DefaultTheme()
	hl := highlight.New("test.go", theme)
	line := "func main() {\treturn \"value\" }"
	buf := text.NewBufferFromBytes([]byte(line))
	hl.Tokenize(buf.Bytes())
	v := Viewport{TabSize: 4}
	tokens := hl.Line(0)
	starts := tokenByteStarts(tokens)

	for _, isCursorLine := range []bool{false, true} {
		name := "editor"
		baseStyle := theme.Editor
		if isCursorLine {
			name = "cursor"
			baseStyle = theme.CursorLine
		}
		t.Run(name, func(t *testing.T) {
			basePair, backgroundPair := v.wrapStylePairFor(theme, isCursorLine)
			got, gotDisplay := renderTokenByteRangeWithTabsAtDisplayIndexedWithPairs(line, tokens, starts, 0, len(line), 0, baseStyle, 4, basePair, backgroundPair)
			want, wantDisplay := renderTokenByteRangeWithTabsAtDisplayIndexedLegacy(line, tokens, starts, 0, len(line), 0, baseStyle, 4)
			if gotDisplay != wantDisplay {
				t.Fatalf("display end = %d, want %d", gotDisplay, wantDisplay)
			}
			gotCells := styledCells(got, gotDisplay)
			wantCells := styledCells(want, wantDisplay)
			if len(gotCells) != len(wantCells) {
				t.Fatalf("cell count = %d, want %d\n got %q\nwant %q", len(gotCells), len(wantCells), got, want)
			}
			for i := range gotCells {
				if !gotCells[i].Equal(&wantCells[i]) {
					t.Fatalf("cell %d differs: got %#v, want %#v\n got %q\nwant %q", i, gotCells[i], wantCells[i], got, want)
				}
			}
		})
	}
}

func styledCells(rendered string, width int) []uv.Cell {
	buffer := uv.NewScreenBuffer(width, 1)
	uv.NewStyledString(rendered).Draw(buffer, uv.Rect(0, 0, width, 1))
	return buffer.Lines[0]
}

func TestViewportCachesWrappedTokenStartsByBufferVersion(t *testing.T) {
	tokens := []highlight.StyledToken{{Text: "alpha"}, {Text: "beta"}}
	viewport := Viewport{}
	first := viewport.wrapTokenStarts(0, 1, tokens)
	second := viewport.wrapTokenStarts(0, 1, tokens)
	if len(first) == 0 || &first[0] != &second[0] {
		t.Fatal("wrapped token offsets were rebuilt for an unchanged buffer version")
	}
	third := viewport.wrapTokenStarts(0, 2, tokens)
	if &first[0] == &third[0] {
		t.Fatal("wrapped token offsets were not invalidated for a new buffer version")
	}
}

func TestViewportRenderWithCursorLine(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	theme := ui.DefaultTheme()
	buf.Cursor = text.Position{Line: 0, Col: 5}

	viewport := Viewport{
		Width:  80,
		Height: 10,
	}

	result := viewport.Render(buf, theme, nil, nil, nil)
	if result == "" {
		t.Error("expected non-empty render result")
	}
}

func TestViewportRenderNarrowWidth(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	theme := ui.DefaultTheme()

	viewport := Viewport{
		Width:  5,
		Height: 10,
	}

	result := viewport.Render(buf, theme, nil, nil, nil)
	if result == "" {
		t.Error("expected non-empty render result")
	}
}

func TestViewportRenderLineWithTokens(t *testing.T) {
	viewport := Viewport{
		Width:   80,
		Height:  10,
		ScrollX: 0,
	}

	tokens := []highlight.StyledToken{
		{Text: "hello", Style: ui.DefaultTheme().Editor},
		{Text: " world", Style: ui.DefaultTheme().Editor},
	}

	result := viewport.renderLineWithTokens(tokens, false, 80, ui.DefaultTheme())
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestViewportRenderLineWithTokensScrollX(t *testing.T) {
	viewport := Viewport{
		Width:   80,
		Height:  10,
		ScrollX: 3,
	}

	tokens := []highlight.StyledToken{
		{Text: "hello", Style: ui.DefaultTheme().Editor},
		{Text: " world", Style: ui.DefaultTheme().Editor},
	}

	result := viewport.renderLineWithTokens(tokens, false, 80, ui.DefaultTheme())
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestViewportRenderLineWithTokensCursorLine(t *testing.T) {
	viewport := Viewport{
		Width:  80,
		Height: 10,
	}

	tokens := []highlight.StyledToken{
		{Text: "hello", Style: ui.DefaultTheme().Editor},
	}

	result := viewport.renderLineWithTokens(tokens, true, 80, ui.DefaultTheme())
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestViewportRenderLineWithTokensTruncate(t *testing.T) {
	viewport := Viewport{
		Width:  10,
		Height: 10,
	}

	tokens := []highlight.StyledToken{
		{Text: "hello world this is a long line", Style: ui.DefaultTheme().Editor},
	}

	result := viewport.renderLineWithTokens(tokens, false, 10, ui.DefaultTheme())
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestViewportSelectionRange(t *testing.T) {
	sel := &text.Selection{
		Anchor: text.Position{Line: 0, Col: 2},
		Head:   text.Position{Line: 1, Col: 3},
	}

	// Test overlap on line 0
	start, end := selectionRange(sel, 0, 10)
	if start != 2 {
		t.Errorf("expected start 2, got %d", start)
	}
	if end != 10 {
		t.Errorf("expected end 10, got %d", end)
	}

	// Test overlap on line 1
	start, end = selectionRange(sel, 1, 10)
	if start != 0 {
		t.Errorf("expected start 0, got %d", start)
	}
	if end != 3 {
		t.Errorf("expected end 3, got %d", end)
	}

	// Test no overlap
	start, _ = selectionRange(sel, 5, 10)
	if start != -1 {
		t.Errorf("expected start -1, got %d", start)
	}
}

func TestViewportSelectionRangeNil(t *testing.T) {
	start, end := selectionRange(nil, 0, 10)
	if start != -1 || end != -1 {
		t.Errorf("expected -1, -1 for nil selection, got %d, %d", start, end)
	}
}

func TestViewportSelectionRangeEmpty(t *testing.T) {
	sel := &text.Selection{
		Anchor: text.Position{Line: 0, Col: 5},
		Head:   text.Position{Line: 0, Col: 5},
	}
	start, end := selectionRange(sel, 0, 10)
	if start != -1 || end != -1 {
		t.Errorf("expected -1, -1 for empty selection, got %d, %d", start, end)
	}
}

func TestViewportRenderLineWithSelection(t *testing.T) {
	viewport := Viewport{
		Width:  80,
		Height: 10,
	}

	lineContent := "hello world"
	lineBytes := []byte(lineContent)

	result := viewport.renderLineWithSelection(lineContent, lineBytes, 0, 5, false, 80, ui.DefaultTheme())
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestViewportRenderLineWithSelectionScrollX(t *testing.T) {
	viewport := Viewport{
		Width:   80,
		Height:  10,
		ScrollX: 2,
	}

	lineContent := "hello world"
	lineBytes := []byte(lineContent)

	result := viewport.renderLineWithSelection(lineContent, lineBytes, 0, 5, false, 80, ui.DefaultTheme())
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestViewportRenderLineWithSelectionTruncate(t *testing.T) {
	viewport := Viewport{
		Width:  10,
		Height: 10,
	}

	lineContent := "hello world this is long"
	lineBytes := []byte(lineContent)

	result := viewport.renderLineWithSelection(lineContent, lineBytes, 0, 5, false, 10, ui.DefaultTheme())
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestViewportApplyScrollXCount(t *testing.T) {
	result, remaining := applyScrollXCount("hello", 0)
	if result != "hello" || remaining != 0 {
		t.Errorf("expected 'hello', 0, got %q, %d", result, remaining)
	}

	result, remaining = applyScrollXCount("hello", 2)
	if result != "llo" || remaining != 0 {
		t.Errorf("expected 'llo', 0, got %q, %d", result, remaining)
	}

	result, remaining = applyScrollXCount("hello", 10)
	if result != "" || remaining != 5 {
		t.Errorf("expected '', 5, got %q, %d", result, remaining)
	}
}

func TestViewportScreenToBufferPosition(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	viewport := Viewport{
		Width:   80,
		Height:  10,
		ScrollX: 0,
		ScrollY: 0,
	}

	pos := viewport.ScreenToBufferPosition(10, 0, buf, 4, nil)
	if pos.Line != 0 {
		t.Errorf("expected line 0, got %d", pos.Line)
	}
	if pos.Col < 1 {
		t.Errorf("expected col >= 1, got %d", pos.Col)
	}
}

func TestViewportScreenToBufferPositionScrollY(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("line1\nline2\nline3\nline4\nline5"))
	viewport := Viewport{
		Width:   80,
		Height:  10,
		ScrollX: 0,
		ScrollY: 2,
	}

	pos := viewport.ScreenToBufferPosition(5, 0, buf, 4, nil)
	if pos.Line != 2 {
		t.Errorf("expected line 2, got %d", pos.Line)
	}
}

func TestViewportScreenToBufferPositionBounds(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	viewport := Viewport{
		Width:  80,
		Height: 10,
	}

	// Test negative Y
	pos := viewport.ScreenToBufferPosition(5, -5, buf, 4, nil)
	if pos.Line != 0 {
		t.Errorf("expected line 0, got %d", pos.Line)
	}

	// Test beyond buffer
	pos = viewport.ScreenToBufferPosition(5, 100, buf, 4, nil)
	if pos.Line != 0 {
		t.Errorf("expected line 0, got %d", pos.Line)
	}

	// Test negative X
	pos = viewport.ScreenToBufferPosition(-5, 0, buf, 4, nil)
	if pos.Col != 0 {
		t.Errorf("expected col 0, got %d", pos.Col)
	}
}

func TestViewportEnsureCursorVisible(t *testing.T) {
	_ = text.NewBufferFromBytes([]byte("line1\nline2\nline3\nline4\nline5"))
	viewport := Viewport{
		Width:  80,
		Height: 3,
	}

	// Cursor below viewport
	cursor := text.Position{Line: 4, Col: 0}
	viewport.EnsureCursorVisible(cursor, 5)
	if viewport.ScrollY < 2 {
		t.Errorf("expected ScrollY >= 2, got %d", viewport.ScrollY)
	}
}

func TestViewportEnsureCursorVisibleScrollUp(t *testing.T) {
	_ = text.NewBufferFromBytes([]byte("line1\nline2\nline3\nline4\nline5"))
	viewport := Viewport{
		Width:   80,
		Height:  3,
		ScrollY: 3,
	}

	// Cursor above viewport
	cursor := text.Position{Line: 0, Col: 0}
	viewport.EnsureCursorVisible(cursor, 5)
	if viewport.ScrollY != 0 {
		t.Errorf("expected ScrollY 0, got %d", viewport.ScrollY)
	}
}

func TestViewportScrollUp(t *testing.T) {
	viewport := Viewport{
		Width:   80,
		Height:  10,
		ScrollY: 5,
	}

	viewport.ScrollUp(2)
	if viewport.ScrollY != 3 {
		t.Errorf("expected ScrollY 3, got %d", viewport.ScrollY)
	}

	// Test clamping at 0
	viewport.ScrollY = 1
	viewport.ScrollUp(5)
	if viewport.ScrollY != 0 {
		t.Errorf("expected ScrollY 0, got %d", viewport.ScrollY)
	}
}

func TestViewportScrollDown(t *testing.T) {
	viewport := Viewport{
		Width:   80,
		Height:  10,
		ScrollY: 0,
	}

	viewport.ScrollDown(2, 20)
	if viewport.ScrollY != 2 {
		t.Errorf("expected ScrollY 2, got %d", viewport.ScrollY)
	}

	// Test clamping at max
	viewport.ScrollY = 15
	viewport.ScrollDown(10, 20)
	if viewport.ScrollY != 11 {
		t.Errorf("expected ScrollY 11, got %d", viewport.ScrollY)
	}
}

func TestViewportScrollDownNegativeMax(t *testing.T) {
	viewport := Viewport{
		Width:   80,
		Height:  20,
		ScrollY: 0,
	}

	viewport.ScrollDown(5, 5)
	if viewport.ScrollY != 0 {
		t.Errorf("expected ScrollY 0, got %d", viewport.ScrollY)
	}
}

func TestApplyScrollX(t *testing.T) {
	result := applyScrollX("hello", 0)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}

	result = applyScrollX("hello", 2)
	if result != "llo" {
		t.Errorf("expected 'llo', got %q", result)
	}

	result = applyScrollX("hello", 10)
	if result != "" {
		t.Errorf("expected '', got %q", result)
	}
}

func TestTruncateToWidth(t *testing.T) {
	result := truncateToWidth("hello", 10)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}

	result = truncateToWidth("hello world", 5)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}

	result = truncateToWidth("hello", 0)
	if result != "" {
		t.Errorf("expected '', got %q", result)
	}
}

func TestDisplayWidth(t *testing.T) {
	w := displayWidth("hello")
	if w != 5 {
		t.Errorf("expected 5, got %d", w)
	}

	w = displayWidth("hello world")
	if w != 11 {
		t.Errorf("expected 11, got %d", w)
	}
}

func TestDisplayWidthUnicode(t *testing.T) {
	w := displayWidth("你好")
	if w != 4 {
		t.Errorf("expected 4, got %d", w)
	}

	w = displayWidth("🎉")
	if w != 2 {
		t.Errorf("expected 2, got %d", w)
	}
}

// Helper function
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	lines = append(lines, s[start:])
	return lines
}
