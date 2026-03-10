package editor

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"teak/internal/clipboard"
	"teak/internal/highlight"
	"teak/internal/text"
	"teak/internal/ui"
)

// TokenizeCompleteMsg carries the result of async tokenization.
//
// Performance Note: When Partial is true (viewport tokenization), only the
// visible region and a margin around it are tokenized. This provides 145x
// speedup for large files (1.8ms vs 264ms for 10K lines).
//
// Memory Note: The Lines slice is sized to match the full buffer line count
// for compatibility with existing rendering code. Lines outside the viewport
// will have nil entries, wasting ~8 bytes per line (acceptable for files
// under 500K lines, ~4MB waste).
type TokenizeCompleteMsg struct {
	Version int
	Lines   [][]highlight.StyledToken
	Partial bool // true when result is from viewport-only tokenization
}

// RequestCompletionCmd is a command that triggers completion from the app layer.
type RequestCompletionCmd struct{}

// Diagnostic represents a diagnostic message from an LSP server (decoupled from LSP types).
type Diagnostic struct {
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	Severity  int // 1=error, 2=warning, 3=info, 4=hint
	Message   string
}

// BreakpointClickMsg is emitted when the user clicks the line number gutter.
type BreakpointClickMsg struct{ Line int }

// RetokenizeMsg triggers syntax re-tokenization after edits.
// RetokenizeMsg triggers syntax re-tokenization after edits or scrolls.
//
// Performance Strategy:
//   - Edit-triggered (ViewportOnly=false): Full file tokenization
//     Needed because edits can change highlighting anywhere in the file.
//   - Scroll-triggered (ViewportOnly=true): Viewport-only tokenization
//     Provides 145x speedup for large files. Only tokenizes visible region
//     plus a 200-line margin for multi-line construct context.
//
// Debouncing:
//   - Edit-triggered: 150ms debounce (scheduleRetokenize)
//   - Scroll-triggered: Immediate (scheduleRetokenizeImmediate)
//     Scrolling should feel instant, so no debounce.
type RetokenizeMsg struct {
	Version      int  // Buffer version (for staleness detection)
	ViewportOnly bool // true for scroll-triggered (fast), false for edit-triggered (full)
}

// Editor is a sub-model managing text editing with mouse and keyboard.
type Editor struct {
	Buffer            *text.Buffer
	Viewport          Viewport
	Config            Config
	theme             ui.Theme
	dragging          bool
	Highlighter       *highlight.Highlighter
	Diagnostics       []Diagnostic
	autocomplete      Autocomplete
	hover             Hover
	signatureHelp     SignatureHelp
	contextMenu       ContextMenu
	HasLSP            bool
	TriggerCharacters []string    // from LSP server capabilities
	DebugGutter       *GutterOpts // set by app when debugging
	Folds             FoldState   // code folding state
	Wrap              *WrapLayout // word wrap layout (nil when disabled)
	lastVersion       int
	lastClickTime     time.Time
	lastClickPos      text.Position
}

// New creates a new Editor with the given buffer, theme, and config.
// The first screenful is tokenized synchronously so the first render has color.
// Call ScheduleInitialTokenize to kick off the full async tokenization.
func New(buf *text.Buffer, theme ui.Theme, cfg Config) Editor {
	var hl *highlight.Highlighter
	if buf.FilePath != "" {
		hl = highlight.New(buf.FilePath, theme)
		// Synchronously tokenize first screenful (~60 lines) so the first
		// frame renders with syntax highlighting, avoiding the unstyled flash.
		hl.TokenizePrefix(buf.Bytes(), 60)
	}

	return Editor{
		Buffer:        buf,
		Config:        cfg,
		theme:         theme,
		Highlighter:   hl,
		autocomplete:  NewAutocomplete(theme),
		hover:         NewHover(theme),
		signatureHelp: NewSignatureHelp(theme),
		contextMenu:   NewContextMenu(theme),
		lastVersion:   -1,
	}
}

// ScheduleInitialTokenize returns a command that runs full async tokenization.
// The prefix was already tokenized synchronously in New(), so this fills in
// the rest of the file. Goes directly to async Cmd, skipping RetokenizeMsg roundtrip.
func (e Editor) ScheduleInitialTokenize() tea.Cmd {
	if e.Highlighter == nil {
		return nil
	}
	hl := e.Highlighter
	content := e.Buffer.Bytes()
	version := e.Buffer.Version()
	return func() tea.Msg {
		lines := hl.TokenizeToLines(content)
		return TokenizeCompleteMsg{Version: version, Lines: lines}
	}
}

// SetSize sets the available editor dimensions.
func (e *Editor) SetSize(width, height int) {
	e.Viewport.Width = width
	e.Viewport.Height = height
	if e.Config.WordWrap && e.Buffer != nil {
		textWidth := width - gutterWidth(e.Buffer.LineCount()) - 1
		if textWidth < 1 {
			textWidth = 1
		}
		if e.Wrap == nil || e.Wrap.Width() != textWidth {
			e.Wrap = NewWrapLayout(e.Buffer.Line, e.Buffer.LineCount(), textWidth)
		}
	}
}

// Update handles input messages, returns updated editor and optional command.
func (e Editor) Update(msg tea.Msg) (Editor, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Context menu intercepts keys when visible
		if e.contextMenu.Visible {
			switch msg.String() {
			case "up":
				e.contextMenu.MoveUp()
				return e, nil
			case "down":
				e.contextMenu.MoveDown()
				return e, nil
			case "enter":
				if item := e.contextMenu.Selected(); item != nil {
					action := item.Action
					e.contextMenu.Hide()
					return e.dispatchContextMenuAction(action)
				}
				e.contextMenu.Hide()
				return e, nil
			case "esc", "escape":
				e.contextMenu.Hide()
				return e, nil
			default:
				e.contextMenu.Hide()
				return e, nil
			}
		}

		// Autocomplete intercepts some keys when visible
		if e.autocomplete.Visible {
			switch msg.String() {
			case "up":
				e.autocomplete.MoveUp()
				return e, nil
			case "down":
				e.autocomplete.MoveDown()
				return e, nil
			case "enter", "tab":
				if item := e.autocomplete.Selected(); item != nil {
					e.Buffer.InsertAtCursor([]byte(item.InsertText))
					e.Viewport.EnsureCursorVisible(e.Buffer.Cursor, e.Buffer.LineCount())
				}
				e.autocomplete.Hide()
				return e, e.scheduleRetokenize()
			case "esc", "escape":
				e.autocomplete.Hide()
				return e, nil
			}
		}
		return e.handleKeyPress(msg)
	case tea.MouseClickMsg:
		return e.handleMouseClick(msg)
	case tea.MouseMotionMsg:
		return e.handleMouseMotion(msg)
	case tea.MouseReleaseMsg:
		e.dragging = false
		return e, nil
	case tea.MouseWheelMsg:
		return e.handleMouseWheel(msg)
	case tea.PasteMsg:
		e.Buffer.InsertAtCursor([]byte(msg.Content))
		e.Viewport.EnsureCursorVisible(e.Buffer.Cursor, e.Buffer.LineCount())
		return e, e.scheduleRetokenize()
	case RetokenizeMsg:
		if e.Highlighter == nil {
			return e, nil
		}
		// Discard stale retokenize messages
		if msg.Version != e.Buffer.Version() {
			return e, nil
		}
		// Skip duplicate version (but allow viewport-only re-tokenization for scroll)
		if msg.Version == e.lastVersion && !msg.ViewportOnly {
			return e, nil
		}
		e.lastVersion = msg.Version
		// Launch async tokenization
		hl := e.Highlighter
		content := e.Buffer.Bytes()
		version := msg.Version
		if msg.ViewportOnly {
			// Scroll-triggered: only tokenize viewport region
			viewStart := e.Viewport.ScrollY
			viewEnd := e.Viewport.ScrollY + e.Viewport.Height
			return e, func() tea.Msg {
				lines := hl.TokenizeViewport(e.Buffer, viewStart, viewEnd)
				return TokenizeCompleteMsg{Version: version, Lines: lines, Partial: true}
			}
		}
		// Edit-triggered: tokenize the full file
		return e, func() tea.Msg {
			lines := hl.TokenizeToLines(content)
			return TokenizeCompleteMsg{Version: version, Lines: lines}
		}
	case TokenizeCompleteMsg:
		if e.Highlighter == nil {
			return e, nil
		}
		if msg.Version == e.lastVersion {
			if msg.Partial {
				e.Highlighter.MergeLines(msg.Lines)
			} else {
				e.Highlighter.SetLines(msg.Lines)
			}
		}
		return e, nil
	}
	return e, nil
}

func (e Editor) handleKeyPress(msg tea.KeyPressMsg) (Editor, tea.Cmd) {
	edited := false
	switch msg.String() {
	// --- Navigation ---
	case "left":
		e.Buffer.ClearSelection()
		e.Buffer.MoveCursor(text.DirLeft)
	case "right":
		e.Buffer.ClearSelection()
		e.Buffer.MoveCursor(text.DirRight)
	case "up":
		e.Buffer.ClearSelection()
		e.Buffer.MoveCursor(text.DirUp)
	case "down":
		e.Buffer.ClearSelection()
		e.Buffer.MoveCursor(text.DirDown)
	case "ctrl+left":
		e.Buffer.ClearSelection()
		e.Buffer.MoveCursorWordLeft()
	case "ctrl+right":
		e.Buffer.ClearSelection()
		e.Buffer.MoveCursorWordRight()
	case "home":
		e.Buffer.ClearSelection()
		e.Buffer.CursorToLineStart()
	case "end":
		e.Buffer.ClearSelection()
		e.Buffer.CursorToLineEnd()
	case "ctrl+home":
		e.Buffer.ClearSelection()
		e.Buffer.CursorToDocStart()
	case "ctrl+end":
		e.Buffer.ClearSelection()
		e.Buffer.CursorToDocEnd()
	case "pgup":
		e.Buffer.ClearSelection()
		target := max(0, e.Buffer.Cursor.Line-e.Viewport.Height)
		e.Buffer.Cursor.Line = target
		e.Buffer.Cursor.Col = min(e.Buffer.Cursor.Col, e.Buffer.Rope().LineLen(target))
		e.Viewport.ScrollUp(e.Viewport.Height)
		// Trigger viewport tokenization if scrolled outside tokenized range
		if e.needsRetokenize() {
			return e, e.scheduleRetokenizeImmediate()
		}
	case "pgdown":
		e.Buffer.ClearSelection()
		maxLine := e.Buffer.LineCount() - 1
		target := min(maxLine, e.Buffer.Cursor.Line+e.Viewport.Height)
		e.Buffer.Cursor.Line = target
		e.Buffer.Cursor.Col = min(e.Buffer.Cursor.Col, e.Buffer.Rope().LineLen(target))
		e.Viewport.ScrollDown(e.Viewport.Height, maxLine)
		// Trigger viewport tokenization if scrolled outside tokenized range
		if e.needsRetokenize() {
			return e, e.scheduleRetokenizeImmediate()
		}

	// --- Selection ---
	case "shift+left":
		e.Buffer.ExtendSelection(func() { e.Buffer.MoveCursor(text.DirLeft) })
	case "shift+right":
		e.Buffer.ExtendSelection(func() { e.Buffer.MoveCursor(text.DirRight) })
	case "shift+up":
		e.Buffer.ExtendSelection(func() { e.Buffer.MoveCursor(text.DirUp) })
	case "shift+down":
		e.Buffer.ExtendSelection(func() { e.Buffer.MoveCursor(text.DirDown) })
	case "ctrl+shift+left":
		e.Buffer.ExtendSelection(func() { e.Buffer.MoveCursorWordLeft() })
	case "ctrl+shift+right":
		e.Buffer.ExtendSelection(func() { e.Buffer.MoveCursorWordRight() })
	case "shift+home":
		e.Buffer.ExtendSelection(func() { e.Buffer.CursorToLineStart() })
	case "shift+end":
		e.Buffer.ExtendSelection(func() { e.Buffer.CursorToLineEnd() })
	case "ctrl+shift+home":
		e.Buffer.ExtendSelection(func() { e.Buffer.CursorToDocStart() })
	case "ctrl+shift+end":
		e.Buffer.ExtendSelection(func() { e.Buffer.CursorToDocEnd() })
	case "ctrl+a":
		e.Buffer.SelectAll()

	// --- Clipboard ---
	case "ctrl+c":
		if sel := e.Buffer.SelectedText(); len(sel) > 0 {
			_ = clipboard.Copy(string(sel))
		}
	case "ctrl+x":
		if sel := e.Buffer.SelectedText(); len(sel) > 0 {
			_ = clipboard.Copy(string(sel))
			e.Buffer.DeleteSelection()
			edited = true
		}
	case "ctrl+v":
		if content, _ := clipboard.Paste(); content != "" {
			e.Buffer.InsertAtCursor([]byte(content))
			edited = true
		}

	// --- Editing ---
	case "backspace":
		// Delete both brackets when backspacing between empty pair
		if IsBetweenBrackets(e.Buffer, e.Buffer.Cursor) {
			start := text.Position{Line: e.Buffer.Cursor.Line, Col: e.Buffer.Cursor.Col - 1}
			end := text.Position{Line: e.Buffer.Cursor.Line, Col: e.Buffer.Cursor.Col + 1}
			e.Buffer.ReplaceRange(start, end, nil)
			e.Buffer.SetCursor(start)
			edited = true
			break
		}
		e.Buffer.Backspace()
		edited = true
	case "ctrl+backspace":
		e.Buffer.BackspaceWord()
		edited = true
	case "delete":
		e.Buffer.Delete()
		edited = true
	case "ctrl+delete":
		e.Buffer.DeleteWord()
		edited = true
	case "enter":
		if e.Config.AutoIndent {
			e.Buffer.InsertNewlineWithIndent()
		} else {
			e.Buffer.InsertNewline()
		}
		edited = true
	case "tab":
		e.Buffer.InsertAtCursor(text.IndentString(e.Config.TabSize))
		edited = true
	case "shift+tab":
		e.Buffer.DedentLine(e.Config.TabSize)
		edited = true
	case "ctrl+z":
		e.Buffer.Undo()
		edited = true
	case "ctrl+shift+z", "ctrl+y":
		e.Buffer.Redo()
		edited = true

	// --- New shortcuts ---
	case "ctrl+/":
		e.Buffer.ToggleLineComment(e.Config.CommentPrefix)
		edited = true
	case "alt+up":
		e.Buffer.MoveLineUp()
		edited = true
	case "alt+down":
		e.Buffer.MoveLineDown()
		edited = true
	case "alt+shift+up":
		e.Buffer.DuplicateLineUp()
		edited = true
	case "alt+shift+down":
		e.Buffer.DuplicateLineDown()
		edited = true
	case "ctrl+shift+k":
		e.Buffer.DeleteLine()
		edited = true
	case "ctrl+d":
		e.Buffer.SelectNextOccurrence()
		edited = true
	case "ctrl+u":
		e.Buffer.SelectAllOccurrences()
		edited = true
	case "ctrl+alt+up":
		e.Buffer.AddCursorAbove()
		edited = true
	case "ctrl+alt+down":
		e.Buffer.AddCursorBelow()
		edited = true
	case "ctrl+shift+l":
		e.Buffer.SplitSelectionIntoLines()
		edited = true
	case "ctrl+l":
		e.Buffer.SelectLine()
	case "ctrl+]":
		e.Buffer.IndentLines(e.Config.TabSize)
		edited = true

	case "esc", "escape":
		e.hover.Hide()
		e.signatureHelp.Hide()
	default:
		if msg.Text != "" {
			ch := msg.Text[0]
			// Skip over closing bracket if it's already the next character
			if len(msg.Text) == 1 && IsCloseBracket(ch) {
				line := e.Buffer.Line(e.Buffer.Cursor.Line)
				if e.Buffer.Cursor.Col < len(line) && line[e.Buffer.Cursor.Col] == ch {
					e.Buffer.MoveCursor(text.DirRight)
					break
				}
			}
			e.Buffer.InsertAtCursor([]byte(msg.Text))
			// Auto-close bracket
			if len(msg.Text) == 1 {
				if close := AutoClosePair(ch); close != 0 {
					e.Buffer.InsertAtCursor([]byte{close})
					e.Buffer.MoveCursor(text.DirLeft)
				}
			}
			edited = true
		}
	}
	e.Viewport.EnsureCursorVisible(e.Buffer.Cursor, e.Buffer.LineCount())
	if edited {
		e.hover.Hide()
		e.signatureHelp.Hide()
		if e.Highlighter != nil {
			e.Highlighter.Invalidate()
		}
		return e, tea.Batch(e.scheduleRetokenize(), e.TriggerCompletion())
	}
	return e, nil
}

// TriggerCompletion returns a command that triggers completion if appropriate.
// Call this after text input to show completions automatically.
func (e Editor) TriggerCompletion() tea.Cmd {
	// Only trigger if we're in a valid file with LSP
	if !e.HasLSP || e.Buffer.FilePath == "" {
		return nil
	}

	// Check if we're at a position that should trigger completion
	line := e.Buffer.Line(e.Buffer.Cursor.Line)
	if e.Buffer.Cursor.Col <= 0 || e.Buffer.Cursor.Col > len(line) {
		return nil
	}

	// Get the character before cursor
	prevCol := e.Buffer.Cursor.Col - 1
	if prevCol < 0 {
		return nil
	}
	ch := rune(line[prevCol])

	// Trigger on identifier characters (a-z, A-Z, 0-9, _)
	if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') || ch == '_' {
		return func() tea.Msg { return RequestCompletionCmd{} }
	}

	// Check LSP-advertised trigger characters first
	if len(e.TriggerCharacters) > 0 {
		s := string(ch)
		for _, tc := range e.TriggerCharacters {
			if s == tc {
				return func() tea.Msg { return RequestCompletionCmd{} }
			}
		}
	} else {
		// Fallback: trigger on common characters when no LSP info available
		if ch == '.' || ch == ':' {
			return func() tea.Msg { return RequestCompletionCmd{} }
		}
	}

	return nil
}

func (e Editor) handleMouseClick(msg tea.MouseClickMsg) (Editor, tea.Cmd) {
	m := msg.Mouse()

	// Left-click dismisses context menu
	if e.contextMenu.Visible && m.Button == tea.MouseLeft {
		e.contextMenu.Hide()
		return e, nil
	}

	// Right-click opens context menu
	if m.Button == tea.MouseRight {
		pos := e.screenToBuffer(m.X, m.Y)
		// Only move cursor if no selection (preserve selection for cut/copy)
		if e.Buffer.Selections == nil || e.Buffer.Selections.Count() == 0 || e.Buffer.Selections.Primary().IsEmpty() {
			e.Buffer.Cursor = pos
		}
		e.contextMenu.Show(e.buildEditorMenuItems(), m.X, m.Y)
		return e, nil
	}

	if m.Button == tea.MouseLeft {
		// Compute gutter column boundaries
		markerW := 0
		if e.DebugGutter != nil {
			markerW = 3 // 1 leading space + 2-cell icon + 1 trailing space
		}
		baseW := gutterWidth(e.Buffer.LineCount())
		foldW := 0
		if len(e.Folds.Regions) > 0 {
			foldW = 2 // 2-cell Nerd Font chevron
		}
		foldCol := markerW + baseW   // start of fold column
		gutterEnd := foldCol + foldW // end of gutter (before padding)

		// Click on fold indicator column → toggle fold
		if foldW > 0 && m.X >= foldCol && m.X < gutterEnd {
			pos := e.screenToBuffer(m.X, m.Y)
			e.Folds.Toggle(pos.Line)
			return e, nil
		}

		// Click on marker or line number area → toggle breakpoint
		if m.X < foldCol {
			pos := e.screenToBuffer(m.X, m.Y)
			return e, func() tea.Msg { return BreakpointClickMsg{Line: pos.Line} }
		}

		pos := e.screenToBuffer(m.X, m.Y)
		if m.Mod == tea.ModShift {
			anchor := e.Buffer.Cursor
			if e.Buffer.Selections != nil && e.Buffer.Selections.Count() > 0 {
				anchor = e.Buffer.Selections.Primary().Anchor
			}
			e.Buffer.SetSelection(anchor, pos)
		} else {
			now := time.Now()
			// Double-click detection: same position within 400ms
			if pos == e.lastClickPos && now.Sub(e.lastClickTime) < 400*time.Millisecond {
				e.Buffer.Cursor = pos
				e.Buffer.SelectWordAtCursor()
				e.lastClickTime = time.Time{} // reset to prevent triple-click
				return e, nil
			}
			e.lastClickTime = now
			e.lastClickPos = pos
			e.Buffer.ClearSelection()
			e.Buffer.Cursor = pos
			e.dragging = true
		}
	}
	return e, nil
}

func (e Editor) handleMouseMotion(msg tea.MouseMotionMsg) (Editor, tea.Cmd) {
	if !e.dragging {
		return e, nil
	}
	m := msg.Mouse()
	pos := e.screenToBuffer(m.X, m.Y)
	anchor := e.Buffer.Cursor
	if e.Buffer.Selections != nil && e.Buffer.Selections.Count() > 0 {
		anchor = e.Buffer.Selections.Primary().Anchor
	}
	e.Buffer.SetSelection(anchor, pos)
	return e, nil
}

func (e Editor) handleMouseWheel(msg tea.MouseWheelMsg) (Editor, tea.Cmd) {
	m := msg.Mouse()
	switch m.Button {
	case tea.MouseWheelUp:
		e.Viewport.ScrollUp(3)
	case tea.MouseWheelDown:
		e.Viewport.ScrollDown(3, e.Buffer.LineCount()-1)
	}
	if e.needsRetokenize() {
		return e, e.scheduleRetokenizeImmediate()
	}
	return e, nil
}

// scheduleRetokenizeImmediate sends a RetokenizeMsg without debounce,
// used when scrolling past the tokenized range (user is waiting to see color).
func (e Editor) scheduleRetokenizeImmediate() tea.Cmd {
	if e.Highlighter == nil {
		return nil
	}
	version := e.Buffer.Version()
	return func() tea.Msg {
		return RetokenizeMsg{Version: version, ViewportOnly: true}
	}
}

func (e Editor) scheduleRetokenize() tea.Cmd {
	if e.Highlighter == nil {
		return nil
	}
	version := e.Buffer.Version()
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		return RetokenizeMsg{Version: version}
	})
}

// needsRetokenize checks if the viewport has scrolled outside the tokenized range.
func (e Editor) needsRetokenize() bool {
	if e.Highlighter == nil {
		return false
	}
	start, end := e.Highlighter.TokenizedRange()
	if start < 0 {
		return false // no viewport-scoped tokenization done yet
	}
	viewStart := e.Viewport.ScrollY
	viewEnd := e.Viewport.ScrollY + e.Viewport.Height
	return viewStart < start || viewEnd > end
}

// View renders the editor content.
func (e Editor) View() string {
	if e.Wrap != nil && e.Config.WordWrap {
		return e.Viewport.RenderWithWrap(e.Buffer, e.theme, e.Highlighter, e.Diagnostics, e.DebugGutter, e.Wrap)
	}
	if len(e.Folds.Regions) > 0 {
		return e.Viewport.RenderWithFolds(e.Buffer, e.theme, e.Highlighter, e.Diagnostics, e.DebugGutter, &e.Folds)
	}
	return e.Viewport.Render(e.Buffer, e.theme, e.Highlighter, e.Diagnostics, e.DebugGutter)
}

// effectiveGutterWidth computes the total gutter width matching what Render produces.
func (e Editor) effectiveGutterWidth() int {
	w := gutterWidth(e.Buffer.LineCount())
	if e.DebugGutter != nil {
		w += 3 // breakpoint marker column (1 leading space + 2-cell icon + 1 trailing space)
	}
	if len(e.Folds.Regions) > 0 {
		w += 2 // fold indicator column (2-cell Nerd Font chevron)
	}
	return w + 1 // +1 for gutter padding
}

// visibleLinesForClick returns the visible lines slice when folds are active, nil otherwise.
func (e Editor) visibleLinesForClick() []int {
	if len(e.Folds.Regions) == 0 {
		return nil
	}
	startLine := e.Viewport.foldedScrollStart(&e.Folds, e.Buffer.LineCount())
	return e.Folds.VisibleLines(startLine, e.Viewport.Height, e.Buffer.LineCount())
}

// screenToBuffer maps screen coordinates to buffer position, handling wrap/fold modes.
func (e Editor) screenToBuffer(screenX, screenY int) text.Position {
	gw := e.effectiveGutterWidth()
	if e.Wrap != nil && e.Config.WordWrap {
		return e.Viewport.ScreenToBufferPositionWrap(screenX, screenY, e.Buffer, gw, e.Wrap)
	}
	return e.Viewport.ScreenToBufferPosition(screenX, screenY, e.Buffer, gw, e.visibleLinesForClick())
}

// CursorPosition returns the screen position for the cursor.
func (e Editor) CursorPosition() (int, int) {
	gw := e.effectiveGutterWidth()
	lineContent := e.Buffer.Line(e.Buffer.Cursor.Line)
	col := e.Buffer.Cursor.Col
	if col > len(lineContent) {
		col = len(lineContent)
	}
	displayCol := displayWidth(string(lineContent[:col]))

	// Word wrap mode: cursor position accounts for wrapped visual rows
	if e.Wrap != nil && e.Config.WordWrap {
		textWidth := e.Viewport.Width - gw
		if textWidth < 1 {
			textWidth = 1
		}
		wrapRow := 0
		if textWidth > 0 {
			wrapRow = displayCol / textWidth
		}
		x := displayCol - wrapRow*textWidth + gw
		visualRow := e.Wrap.VisualRow(e.Buffer.Cursor.Line) + wrapRow - e.Wrap.VisualRow(e.Viewport.ScrollY)
		return x, visualRow
	}

	x := displayCol - e.Viewport.ScrollX + gw

	// When folds are active, map buffer line to screen row via visible lines
	if len(e.Folds.Regions) > 0 {
		visLines := e.visibleLinesForClick()
		for i, vl := range visLines {
			if vl == e.Buffer.Cursor.Line {
				return x, i
			}
		}
	}
	y := e.Buffer.Cursor.Line - e.Viewport.ScrollY
	return x, y
}

// ShowAutocomplete displays completion items.
func (e *Editor) ShowAutocomplete(items []AutocompleteItem) {
	e.autocomplete.Show(items)
}

// HideAutocomplete dismisses the autocomplete popup.
func (e *Editor) HideAutocomplete() {
	e.autocomplete.Hide()
}

// ShowHover displays hover information.
func (e *Editor) ShowHover(content string) {
	e.hover.Show(content)
}

// HideHover dismisses the hover popup.
func (e *Editor) HideHover() {
	e.hover.Hide()
}

// ShowSignatureHelp displays signature help.
func (e *Editor) ShowSignatureHelp(help *SignatureData) {
	e.signatureHelp.Show(help)
}

// HideSignatureHelp dismisses the signature help popup.
func (e *Editor) HideSignatureHelp() {
	e.signatureHelp.Hide()
}

// SignatureHelpView returns the signature help popup rendering if visible.
func (e Editor) SignatureHelpView() string {
	return e.signatureHelp.View()
}

// AutocompleteView returns the autocomplete popup rendering if visible.
func (e Editor) AutocompleteView() string {
	return e.autocomplete.View()
}

// HoverView returns the hover popup rendering if visible.
func (e Editor) HoverView() string {
	return e.hover.View()
}

// IsAutocompleteVisible returns whether autocomplete popup is showing.
func (e Editor) IsAutocompleteVisible() bool {
	return e.autocomplete.Visible
}

// ContextMenuView returns the context menu popup rendering if visible.
func (e Editor) ContextMenuView() string {
	return e.contextMenu.View()
}

// IsContextMenuVisible returns whether the context menu is showing.
func (e Editor) IsContextMenuVisible() bool {
	return e.contextMenu.Visible
}

// ContextMenuPosition returns the screen position of the context menu.
func (e Editor) ContextMenuPosition() (int, int) {
	return e.contextMenu.X, e.contextMenu.Y
}

// HideContextMenu dismisses the context menu.
func (e *Editor) HideContextMenu() {
	e.contextMenu.Hide()
}

// ClickContextMenuItem handles a mouse click at the given menu-relative Y index.
// Returns the action string if an item was selected, or "" if dismissed.
func (e *Editor) ClickContextMenuItem(relY int) (Editor, tea.Cmd, string) {
	if item := e.contextMenu.SelectAt(relY); item != nil {
		action := item.Action
		e.contextMenu.Hide()
		ed, cmd := e.dispatchContextMenuAction(action)
		return ed, cmd, action
	}
	e.contextMenu.Hide()
	return *e, nil, ""
}

// ContextMenuItemCount returns the number of visible context menu items.
func (e Editor) ContextMenuItemCount() int {
	return e.contextMenu.ItemCount()
}

// buildEditorMenuItems returns context menu items based on current editor state.
func (e Editor) buildEditorMenuItems() []ContextMenuItem {
	hasSelection := e.Buffer.Selections != nil && e.Buffer.Selections.Count() > 0 && !e.Buffer.Selections.Primary().IsEmpty()

	items := []ContextMenuItem{
		{Label: "Cut", Shortcut: "Ctrl+X", Action: "cut", Disabled: !hasSelection},
		{Label: "Copy", Shortcut: "Ctrl+C", Action: "copy", Disabled: !hasSelection},
		{Label: "Paste", Shortcut: "Ctrl+V", Action: "paste"},
		{Label: ""}, // separator
		{Label: "Select All", Shortcut: "Ctrl+A", Action: "select_all"},
	}

	if e.HasLSP {
		items = append(items,
			ContextMenuItem{Label: ""}, // separator
			ContextMenuItem{Label: "Go to Definition", Shortcut: "F12", Action: "goto_definition"},
			ContextMenuItem{Label: "Find References", Action: "find_references"},
			ContextMenuItem{Label: "Rename Symbol", Action: "rename_symbol"},
		)
	}

	items = append(items,
		ContextMenuItem{Label: ""}, // separator
		ContextMenuItem{Label: "Undo", Shortcut: "Ctrl+Z", Action: "undo"},
		ContextMenuItem{Label: "Redo", Shortcut: "Ctrl+Y", Action: "redo"},
		ContextMenuItem{Label: ""}, // separator
		ContextMenuItem{Label: "Toggle Comment", Shortcut: "Ctrl+/", Action: "toggle_comment"},
	)

	return items
}

// dispatchContextMenuAction handles editor-local actions and returns a message for app-level ones.
func (e Editor) dispatchContextMenuAction(action string) (Editor, tea.Cmd) {
	switch action {
	case "cut":
		if sel := e.Buffer.SelectedText(); len(sel) > 0 {
			_ = clipboard.Copy(string(sel))
			e.Buffer.DeleteSelection()
			e.Viewport.EnsureCursorVisible(e.Buffer.Cursor, e.Buffer.LineCount())
			if e.Highlighter != nil {
				e.Highlighter.Invalidate()
			}
			return e, e.scheduleRetokenize()
		}
		return e, nil
	case "copy":
		if sel := e.Buffer.SelectedText(); len(sel) > 0 {
			_ = clipboard.Copy(string(sel))
		}
		return e, nil
	case "paste":
		if content, _ := clipboard.Paste(); content != "" {
			e.Buffer.InsertAtCursor([]byte(content))
			e.Viewport.EnsureCursorVisible(e.Buffer.Cursor, e.Buffer.LineCount())
			if e.Highlighter != nil {
				e.Highlighter.Invalidate()
			}
			return e, e.scheduleRetokenize()
		}
		return e, nil
	case "select_all":
		e.Buffer.SelectAll()
		return e, nil
	case "undo":
		e.Buffer.Undo()
		e.Viewport.EnsureCursorVisible(e.Buffer.Cursor, e.Buffer.LineCount())
		if e.Highlighter != nil {
			e.Highlighter.Invalidate()
		}
		return e, e.scheduleRetokenize()
	case "redo":
		e.Buffer.Redo()
		e.Viewport.EnsureCursorVisible(e.Buffer.Cursor, e.Buffer.LineCount())
		if e.Highlighter != nil {
			e.Highlighter.Invalidate()
		}
		return e, e.scheduleRetokenize()
	case "toggle_comment":
		e.Buffer.ToggleLineComment(e.Config.CommentPrefix)
		e.Viewport.EnsureCursorVisible(e.Buffer.Cursor, e.Buffer.LineCount())
		if e.Highlighter != nil {
			e.Highlighter.Invalidate()
		}
		return e, e.scheduleRetokenize()
	default:
		// LSP actions dispatch to the app layer
		return e, func() tea.Msg {
			return ContextMenuActionMsg{Action: action}
		}
	}
}
