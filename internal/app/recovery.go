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
	Records []session.RecoveryRecord
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
		return recoveryLoadedMsg{Records: records}
	}
}

// handleRecoveryLoaded restores dirty and untitled buffers from a previous
// session that died before saving. Each record becomes a dirty tab so nothing
// recovered is ever mistaken for saved work.
func (m Model) handleRecoveryLoaded(msg recoveryLoadedMsg) (tea.Model, tea.Cmd) {
	recovered := 0
	for _, record := range msg.Records {
		if len(record.Content) == 0 {
			continue
		}
		if record.FilePath != "" {
			if idx := m.findEditorByPath(record.FilePath); idx >= 0 {
				ed := &m.editors[idx]
				// Never clobber live in-progress edits; skip content that is
				// already in place (saved since the recovery record was written).
				if ed.Buffer.Dirty() || ed.Buffer.Rope().EqualBytes(record.Content) {
					continue
				}
				ending := text.LF
				if record.CRLF {
					ending = text.CRLF
				}
				ed.Buffer.LoadRopeSnapshot(text.New(record.Content), ending)
				ed.Buffer.MarkDirty()
				if idx < len(m.tabBar.Tabs) {
					m.tabBar.Tabs[idx].Dirty = true
				}
				recovered++
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
	if recovered == 0 {
		return m, nil
	}
	log.Info("recovered unsaved buffers", "count", recovered)
	m.status = fmt.Sprintf("Recovered %d unsaved buffer(s) from the previous session", recovered)
	m.relayout()
	return m, nil
}
