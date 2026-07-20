package editor

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/clipboard"
	"teak/internal/text"
)

// ClipboardOperationLimitMsg lets the app render a clear status instead of
// materialising an arbitrarily large selection on the UI goroutine.
type ClipboardOperationLimitMsg struct {
	EditorID  uint64
	Operation string
	MaxBytes  int
}

// ClipboardCopyPreparedMsg is produced after a bounded immutable selection is
// materialised and stored in the in-process clipboard fallback. Cut is applied
// only when the live buffer still matches the captured version and selection.
type ClipboardCopyPreparedMsg struct {
	EditorID   uint64
	Generation uint64
	Version    int
	Start      text.Position
	End        text.Position
	Content    string
	Cut        bool
	Err        error
}

// ClipboardPasteResultMsg is delivered after an OS clipboard read. EditorID
// and Generation make the result safe to route after a tab switch, close, or
// newer paste request.
type ClipboardPasteResultMsg struct {
	EditorID   uint64
	Generation uint64
	Content    string
	Err        error
}

// PastePreparedMsg carries an immutable result built away from Bubble Tea's
// Update loop. SourceErr retains a clipboard integration failure so the app
// can report that the local fallback was used after the edit is committed.
type PastePreparedMsg struct {
	EditorID   uint64
	Generation uint64
	Version    int
	Rope       *text.Rope
	Cursor     text.Position
	Selections []text.Selection
	Primary    int
	SourceErr  error
	Err        error
}

// ClipboardCopyResultMsg reports whether the deferred OS integration worked.
// The local fallback is already available regardless of Err.
type ClipboardCopyResultMsg struct {
	EditorID       uint64
	FallbackStored bool
	Err            error
}

func copyToClipboardCmd(editorID uint64, content string) tea.Cmd {
	return func() tea.Msg {
		// Store has already updated the in-process fallback in Update. Only the
		// OS integration, which can block for up to its timeout, runs here.
		return ClipboardCopyResultMsg{EditorID: editorID, FallbackStored: true, Err: clipboard.CopyToSystem(content)}
	}
}

func clipboardCopyRejectedCmd(editorID uint64, err error) tea.Cmd {
	return func() tea.Msg { return ClipboardCopyResultMsg{EditorID: editorID, Err: err} }
}

func clipboardOperationLimitCmd(editorID uint64, operation string) tea.Cmd {
	return func() tea.Msg {
		return ClipboardOperationLimitMsg{EditorID: editorID, Operation: operation, MaxBytes: clipboard.MaxClipboardBytes}
	}
}

func prepareClipboardCopyCmd(editorID, generation uint64, version int, snapshot *text.Rope, start, end text.Position, startOffset, endOffset int, cut bool) tea.Cmd {
	return func() tea.Msg {
		content := string(snapshot.Slice(startOffset, endOffset).Bytes())
		return ClipboardCopyPreparedMsg{
			EditorID: editorID, Generation: generation, Version: version,
			Start: start, End: end, Content: content, Cut: cut, Err: clipboard.Store(content),
		}
	}
}

func pasteFromClipboardCmd(editorID, generation uint64) tea.Cmd {
	return func() tea.Msg {
		content, err := clipboard.Paste()
		return ClipboardPasteResultMsg{
			EditorID: editorID, Generation: generation, Content: content, Err: err,
		}
	}
}

func (e *Editor) requestClipboardPaste() tea.Cmd {
	e.clipboardGeneration++
	return pasteFromClipboardCmd(e.id, e.clipboardGeneration)
}

// InvalidateClipboardPaste rejects any in-flight clipboard read. Call this
// before replacing a buffer while retaining the Editor instance (for example,
// an explicit reload of a file changed on disk).
func (e *Editor) InvalidateClipboardPaste() {
	e.clipboardGeneration++
}

func (e Editor) acceptsClipboardPaste(msg ClipboardPasteResultMsg) bool {
	return msg.EditorID == e.id && msg.Generation == e.clipboardGeneration
}

// preparePasteCmd validates and applies an input payload to an immutable
// snapshot outside Update. It intentionally returns only the new rope/cursor:
// large async edits collapse multi-cursor selection state just like the other
// background full-document transforms.
func preparePasteCmd(editorID, generation uint64, version int, snapshot *text.Rope, cursor text.Position, selections []text.Selection, primary text.Selection, content string, sourceErr error) tea.Cmd {
	return func() tea.Msg {
		result := PastePreparedMsg{EditorID: editorID, Generation: generation, Version: version, SourceErr: sourceErr}
		if err := clipboard.Validate(content); err != nil {
			result.Err = err
			return result
		}

		candidate := text.NewBufferFromRope(snapshot)
		if len(selections) > 0 {
			candidate.SetSelection(selections[0].Anchor, selections[0].Head)
			for _, selection := range selections[1:] {
				candidate.Selections.Add(selection)
			}
			for i, selection := range candidate.Selections.All() {
				if selection == primary {
					candidate.Selections.SetPrimary(i)
					break
				}
			}
			candidate.Cursor = candidate.Selections.PrimaryCursor()
		} else {
			candidate.SetCursor(cursor)
		}
		candidate.InsertAtCursor([]byte(content))
		result.Rope = candidate.Rope()
		result.Cursor = candidate.Cursor
		result.Selections = append([]text.Selection(nil), candidate.Selections.All()...)
		result.Primary = candidate.Selections.PrimaryIndex()
		return result
	}
}
