package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/log"
	"teak/internal/session"
	"teak/internal/text"
)

// recoveryLoadedMsg carries crash-recovery records read at startup when the
// normal session-restore pipeline is not running (sessions disabled or the
// restore was superseded before it started).
type recoveryLoadedMsg struct {
	Records []preparedRecoveryRecord
}

// preparedRecoveryRecord owns an immutable snapshot built before a recovery
// message reaches Update. Recovery records are bounded but can still be
// several MiB, so neither rope construction nor content comparison belongs on
// the Bubble Tea event loop.
type preparedRecoveryRecord struct {
	FilePath   string
	Untitled   bool
	Snapshot   *text.Rope
	LineEnding text.LineEnding
}

// prepareRecoveryRecords consumes the record contents. Callers must not reuse
// or mutate records after calling it: NewOwned transfers each byte slice into
// the immutable rope without retaining a second document-sized copy.
func prepareRecoveryRecords(records []session.RecoveryRecord) []preparedRecoveryRecord {
	prepared := make([]preparedRecoveryRecord, 0, len(records))
	for i := range records {
		if len(records[i].Content) == 0 {
			continue
		}
		ending := text.LF
		if records[i].CRLF {
			ending = text.CRLF
		}
		prepared = append(prepared, preparedRecoveryRecord{
			FilePath:   records[i].FilePath,
			Untitled:   records[i].Untitled,
			Snapshot:   text.NewOwned(records[i].Content),
			LineEnding: ending,
		})
		records[i].Content = nil
	}
	return prepared
}

type recoveryComparison struct {
	EditorID uint64
	Version  int
	Current  *text.Rope
	Record   preparedRecoveryRecord
}

type recoveryComparisonResult struct {
	EditorID uint64
	Version  int
	Matches  bool
	Record   preparedRecoveryRecord
}

type recoveryComparisonsMsg struct {
	PreviouslyRecovered int
	Results             []recoveryComparisonResult
}

func compareRecoverySnapshotsCmd(comparisons []recoveryComparison, previouslyRecovered int) tea.Cmd {
	if len(comparisons) == 0 {
		return nil
	}
	return func() tea.Msg {
		results := make([]recoveryComparisonResult, 0, len(comparisons))
		for _, comparison := range comparisons {
			matches := comparison.Current == comparison.Record.Snapshot
			if !matches && comparison.Current != nil && comparison.Record.Snapshot != nil {
				matches = comparison.Current.EqualBytes(comparison.Record.Snapshot.Bytes())
			}
			results = append(results, recoveryComparisonResult{
				EditorID: comparison.EditorID,
				Version:  comparison.Version,
				Matches:  matches,
				Record:   comparison.Record,
			})
		}
		return recoveryComparisonsMsg{PreviouslyRecovered: previouslyRecovered, Results: results}
	}
}

// recoveryLoadCmd reads the workspace's recovery records off the UI goroutine.
func recoveryLoadCmd(rootDir string) tea.Cmd {
	if rootDir == "" {
		return nil
	}
	return func() tea.Msg {
		records, err := session.LoadRecovery(rootDir)
		if err != nil {
			log.Warn("recovery load failed", "err", err)
			return nil
		}
		if len(records) == 0 {
			return nil
		}
		return recoveryLoadedMsg{Records: prepareRecoveryRecords(records)}
	}
}

// handleRecoveryLoaded restores dirty and untitled buffers from a previous
// session that died before saving. Each record becomes a dirty tab so nothing
// recovered is ever mistaken for saved work.
func (m Model) handleRecoveryLoaded(msg recoveryLoadedMsg) (tea.Model, tea.Cmd) {
	recovered := 0
	comparisons := make([]recoveryComparison, 0, len(msg.Records))
	for _, record := range msg.Records {
		if record.Snapshot == nil {
			continue
		}
		if record.FilePath != "" {
			if idx := m.findEditorByPath(record.FilePath); idx >= 0 {
				ed := &m.editors[idx]
				// Never clobber live in-progress edits; skip content that is
				// already in place (saved since the recovery record was written).
				if ed.Buffer.Dirty() {
					continue
				}
				comparisons = append(comparisons, recoveryComparison{
					EditorID: ed.ID(),
					Version:  ed.Buffer.Version(),
					Current:  ed.Buffer.Rope(),
					Record:   record,
				})
				continue
			}
		}
		cmd := m.appendRecoveredTab(record)
		if cmd != nil {
			// Install the content synchronously: there is no session pipeline
			// racing for these tabs, and handling one record at a time keeps
			// the pending-load identity intact.
			if loadedMsg, ok := cmd().(FileLoadedMsg); ok {
				next, _ := m.handleFileLoaded(loadedMsg)
				m = next.(Model)
			}
		}
		recovered++
	}
	if recovered == 0 && len(comparisons) == 0 {
		return m, nil
	}
	if recovered > 0 {
		log.Info("recovered unsaved buffers", "count", recovered)
		m.status = fmt.Sprintf("Recovered %d unsaved buffer(s) from the previous session", recovered)
		m.relayout()
	}
	return m, compareRecoverySnapshotsCmd(comparisons, recovered)
}

func (m Model) handleRecoveryComparisons(msg recoveryComparisonsMsg) (tea.Model, tea.Cmd) {
	recovered := 0
	var cmds []tea.Cmd
	for _, result := range msg.Results {
		idx := m.editorIndexForAsyncMessage(result.EditorID)
		if idx < 0 || result.Matches || result.Record.Snapshot == nil {
			continue
		}
		ed := &m.editors[idx]
		if ed.Buffer.Version() != result.Version || ed.Buffer.Dirty() {
			continue
		}
		prevVersion, prevCursor := ed.Buffer.Version(), ed.Buffer.Cursor
		ed.InvalidateClipboardPaste()
		ed.Buffer.LoadRopeSnapshot(result.Record.Snapshot, result.Record.LineEnding)
		ed.Buffer.MarkDirty()
		if ed.Highlighter != nil {
			ed.Highlighter.Invalidate()
		}
		ed.Folds.SetRegions(nil)
		ed.SetSize(ed.Viewport.Width, ed.Viewport.Height)
		ed.EnsureCursorVisible()
		cmds = append(cmds, ed.ScheduleInitialTokenize(), m.syncEditorStateAfterUpdate(idx, prevVersion, prevCursor))
		recovered++
	}
	if recovered > 0 {
		total := msg.PreviouslyRecovered + recovered
		m.status = fmt.Sprintf("Recovered %d unsaved buffer(s) from the previous session", total)
		m.relayout()
	}
	return m, tea.Batch(cmds...)
}
