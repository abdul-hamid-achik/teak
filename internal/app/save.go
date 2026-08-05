package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/lsp"
	"teak/internal/text"
)

type saveDiskExpectation uint8

const (
	saveDiskUnchecked saveDiskExpectation = iota
	saveDiskExact
	saveDiskMissing
)

var errSaveDestinationChanged = errors.New("save destination changed since it was loaded")

type pendingSaveRequest struct {
	TabIndex           int
	EditorID           uint64
	Path               string
	SaveAs             bool
	PreviousPath       string
	Snapshot           *text.Rope
	SnapshotVersion    int
	LineEnding         text.LineEnding
	QueuedSnapshot     *text.Rope
	QueuedVersion      int
	QueuedPath         string
	QueuedSaveAs       bool
	QueuedPreviousPath string
	CloseAfter         bool
	QuitAfter          bool
	StatusNote         string
	FormattingTried    bool
	// AuthorizedConflictGeneration is the exact conflict for which the user
	// chose Overwrite. Completion clears only this generation; a later watcher
	// observation remains unresolved regardless of watermark ordering.
	AuthorizedConflictGeneration uint64
	DiskExpectation              saveDiskExpectation
	ExpectedDiskSnapshot         *text.Rope
}

type savePreconditionFailedMsg struct {
	Path      string
	RequestID int
	Snapshot  *text.Rope
	Missing   bool
	Err       error
}

func formattingOptions(cfg editor.Config) lsp.FormattingOptions {
	return lsp.FormattingOptions{
		TabSize:      cfg.TabSize,
		InsertSpaces: !cfg.InsertTabs,
	}
}

func (m *Model) nextSaveID() int {
	requestID := m.nextSaveRequestID
	m.nextSaveRequestID++
	return requestID
}

func (m *Model) beginSaveForTab(tabIndex int, closeAfter, quitAfter bool) tea.Cmd {
	if tabIndex < 0 || tabIndex >= len(m.editors) {
		return nil
	}

	path := m.editors[tabIndex].Buffer.FilePath
	if path == "" {
		return nil
	}
	return m.beginSaveSnapshotForTab(tabIndex, path, false, "", closeAfter, quitAfter)
}

// beginSaveAsForTab captures an immutable snapshot for a new destination but
// intentionally leaves the buffer's identity untouched until that write has
// succeeded. This keeps the UI responsive and prevents a failed Save As from
// silently redirecting future saves to a path that was never written.
func (m *Model) beginSaveAsForTab(tabIndex int, path string) tea.Cmd {
	if tabIndex < 0 || tabIndex >= len(m.editors) || path == "" {
		return nil
	}
	var err error
	path, err = m.normalizeSaveAsDestination(path)
	if err != nil {
		m.status = fmt.Sprintf("Save As blocked: %v", err)
		return nil
	}
	oldPath := m.editors[tabIndex].Buffer.FilePath
	if path == oldPath {
		return m.beginSaveForTab(tabIndex, false, false)
	}
	if m.saveAsDestinationReservedByOtherEditor(m.editors[tabIndex].ID(), path) {
		m.status = fmt.Sprintf(
			"Save As blocked: %s is reserved by another save",
			filepath.Base(path),
		)
		return nil
	}
	for index := range m.editors {
		if index == tabIndex {
			continue
		}
		if cleanExternalConflictPath(m.editors[index].Buffer.FilePath) == cleanExternalConflictPath(path) {
			m.status = fmt.Sprintf(
				"Save As blocked: %s is already open in another tab",
				filepath.Base(path),
			)
			return nil
		}
	}
	return m.beginSaveSnapshotForTab(tabIndex, path, true, oldPath, false, false)
}

func (m *Model) beginSaveSnapshotForTab(tabIndex int, path string, saveAs bool, previousPath string, closeAfter, quitAfter bool) tea.Cmd {
	return m.beginSaveSnapshotForTabAuthorized(
		tabIndex,
		path,
		saveAs,
		previousPath,
		closeAfter,
		quitAfter,
		0,
	)
}

func (m *Model) beginSaveSnapshotForTabAuthorized(
	tabIndex int,
	path string,
	saveAs bool,
	previousPath string,
	closeAfter, quitAfter bool,
	authorizedConflictGeneration uint64,
) tea.Cmd {
	if tabIndex < 0 || tabIndex >= len(m.editors) || path == "" {
		return nil
	}
	// Never silently overwrite a disk version that changed while the buffer had
	// local edits. Save As starts with an expected-missing destination and asks
	// for explicit confirmation if a regular file already exists there.
	if conflict, exists := m.externalConflicts[cleanExternalConflictPath(path)]; exists &&
		(authorizedConflictGeneration == 0 || conflict.Generation != authorizedConflictGeneration) {
		*m = m.showExternalConflictConfirmation(path)
		m.status = fmt.Sprintf("Save blocked: %s changed on disk; choose Reload or Overwrite", filepath.Base(path))
		return nil
	}

	ed := m.editors[tabIndex]
	snapshot := ed.Buffer.Rope()
	version := ed.Buffer.Version()
	ending := ed.Buffer.LineEnding()
	diskExpectation := saveDiskExact
	expectedDiskSnapshot := ed.Buffer.SavedRope()
	if saveAs {
		diskExpectation = saveDiskMissing
		expectedDiskSnapshot = nil
	}
	if authorizedConflictGeneration != 0 {
		conflict := m.externalConflicts[cleanExternalConflictPath(path)]
		switch {
		case conflict.Snapshot != nil:
			diskExpectation = saveDiskExact
			expectedDiskSnapshot = conflict.Snapshot
		case conflict.Missing:
			diskExpectation = saveDiskMissing
			expectedDiskSnapshot = nil
		default:
			// The user explicitly authorized an overwrite after an unreadable
			// change, so there is no trustworthy byte baseline to compare.
			diskExpectation = saveDiskUnchecked
			expectedDiskSnapshot = nil
		}
	}
	for requestID, req := range m.pendingSaves {
		if req.EditorID != ed.ID() {
			continue
		}
		req.CloseAfter = req.CloseAfter || closeAfter
		req.QuitAfter = req.QuitAfter || quitAfter
		if authorizedConflictGeneration != 0 {
			req.AuthorizedConflictGeneration = authorizedConflictGeneration
			req.DiskExpectation = diskExpectation
			req.ExpectedDiskSnapshot = expectedDiskSnapshot
		}

		// Once Save As is in flight, a regular Save should follow its chosen
		// destination rather than resurrecting the old path. A later Save As,
		// however, deliberately supersedes that destination.
		queuedPath, queuedSaveAs, queuedPreviousPath := path, saveAs, previousPath
		if req.SaveAs && !saveAs {
			queuedPath, queuedSaveAs, queuedPreviousPath = req.Path, true, req.PreviousPath
		}
		if req.Snapshot == snapshot && req.SnapshotVersion == version &&
			req.Path == queuedPath && req.SaveAs == queuedSaveAs {
			m.pendingSaves[requestID] = req
			return nil
		}
		req.QueuedSnapshot = snapshot
		req.QueuedVersion = version
		req.QueuedPath = queuedPath
		req.QueuedSaveAs = queuedSaveAs
		req.QueuedPreviousPath = queuedPreviousPath
		req.LineEnding = ending
		m.pendingSaves[requestID] = req
		return nil
	}

	requestID := m.nextSaveID()
	m.pendingSaves[requestID] = pendingSaveRequest{
		TabIndex:                     tabIndex,
		EditorID:                     ed.ID(),
		Path:                         path,
		SaveAs:                       saveAs,
		PreviousPath:                 previousPath,
		Snapshot:                     snapshot,
		SnapshotVersion:              version,
		LineEnding:                   ending,
		CloseAfter:                   closeAfter,
		QuitAfter:                    quitAfter,
		AuthorizedConflictGeneration: authorizedConflictGeneration,
		DiskExpectation:              diskExpectation,
		ExpectedDiskSnapshot:         expectedDiskSnapshot,
	}
	return m.startSaveRequest(requestID)
}

func (m *Model) startSaveRequest(requestID int) tea.Cmd {
	req, ok := m.pendingSaves[requestID]
	if !ok {
		return nil
	}

	tabIndex := m.saveRequestEditorIndex(req)
	if tabIndex < 0 {
		delete(m.pendingSaves, requestID)
		return nil
	}
	if req.Snapshot == nil {
		req.Snapshot = m.editors[tabIndex].Buffer.Rope()
		req.SnapshotVersion = m.editors[tabIndex].Buffer.Version()
		req.LineEnding = m.editors[tabIndex].Buffer.LineEnding()
		if req.EditorID == 0 {
			req.EditorID = m.editors[tabIndex].ID()
		}
		m.pendingSaves[requestID] = req
	}

	ed := m.editors[tabIndex]
	if m.appCfg.Editor.FormatOnSave && !req.FormattingTried {
		req.FormattingTried = true
		m.pendingSaves[requestID] = req
		return m.requestFormattingSnapshot(req.Path, ed.Config, requestID, req.SnapshotVersion, req.Snapshot)
	}

	watcher := m.watcher
	path, snapshot, ending := req.Path, req.Snapshot, req.LineEnding
	expectation, expected := req.DiskExpectation, req.ExpectedDiskSnapshot
	ctx := m.externalBackgroundContext()
	return func() tea.Msg {
		observed, missing, err := verifySaveDiskPrecondition(ctx, path, expectation, expected)
		if err != nil {
			return savePreconditionFailedMsg{
				Path:      path,
				RequestID: requestID,
				Snapshot:  observed,
				Missing:   missing,
				Err:       err,
			}
		}
		if watcher != nil {
			watcher.expectOwnWrite(path, snapshot)
		}
		if err := text.WriteRopeAtomicallyWithLineEnding(path, snapshot, ending); err != nil {
			if watcher != nil {
				watcher.cancelOwnWrite(path, snapshot)
			}
			return FileErrorMsg{Path: path, RequestID: requestID, Err: err}
		}
		var watermark uint64
		if watcher != nil {
			watermark = watcher.completeOwnWrite()
		}
		return FileSavedMsg{
			Path:             path,
			RequestID:        requestID,
			WatcherWatermark: watermark,
		}
	}
}

func verifySaveDiskPrecondition(
	ctx context.Context,
	path string,
	expectation saveDiskExpectation,
	expected *text.Rope,
) (*text.Rope, bool, error) {
	switch expectation {
	case saveDiskUnchecked:
		return nil, false, nil
	case saveDiskMissing:
		_, err := os.Lstat(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			return nil, true, nil
		case err != nil:
			return nil, false, fmt.Errorf("%w: %v", errSaveDestinationChanged, err)
		default:
			data, readErr := readEditorFile(ctx, path)
			if readErr != nil {
				return nil, false, fmt.Errorf("%w: destination now exists: %v", errSaveDestinationChanged, readErr)
			}
			return text.NewOwned(data), false, fmt.Errorf("%w: destination now exists", errSaveDestinationChanged)
		}
	case saveDiskExact:
		if expected == nil {
			return nil, false, fmt.Errorf("%w: missing expected snapshot", errSaveDestinationChanged)
		}
		observed, equal, err := compareSaveDestination(ctx, path, expected)
		if err != nil {
			return nil, errors.Is(err, os.ErrNotExist), fmt.Errorf("%w: %v", errSaveDestinationChanged, err)
		}
		if !equal {
			return observed, false, errSaveDestinationChanged
		}
		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("%w: invalid expectation", errSaveDestinationChanged)
	}
}

// compareSaveDestination streams the common unchanged case leaf-by-leaf. It
// materializes a Rope only when bytes differ, preserving the external version
// for the conflict dialog without making every Ctrl+S copy a large document.
func compareSaveDestination(ctx context.Context, path string, expected *text.Rope) (*text.Rope, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	file, err := openEditorInput(path)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%w: %s", errEditorFileNotRegular, path)
	}
	if info.Size() > maxEditorFileBytes {
		return nil, false, errEditorFileTooLarge
	}
	if info.Size() == int64(expected.Len()) {
		reader := bufio.NewReaderSize(contextReader{ctx: ctx, reader: file}, 64<<10)
		equal, err := expected.EqualReader(reader)
		if err != nil {
			return nil, false, err
		}
		if equal {
			return nil, true, nil
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, false, err
		}
	}
	data, _, err := readOpenedEditorFile(ctx, file, path, maxEditorFileBytes)
	if err != nil {
		return nil, false, err
	}
	// Buffer snapshots hold LF-normalized content; compare against the same
	// convention so a CRLF file that still matches the save baseline is not
	// reported as externally modified.
	normalized, _ := text.NormalizeLineEndings(data)
	if expected.EqualBytes(normalized) {
		return nil, true, nil
	}
	return text.NewOwned(normalized), false, nil
}

func (m *Model) startQueuedSave(requestID int, req pendingSaveRequest) tea.Cmd {
	if req.QueuedSnapshot == nil {
		return nil
	}
	previousSnapshot := req.Snapshot
	req.Snapshot = req.QueuedSnapshot
	req.SnapshotVersion = req.QueuedVersion
	req.Path = req.QueuedPath
	req.SaveAs = req.QueuedSaveAs
	req.PreviousPath = req.QueuedPreviousPath
	req.QueuedSnapshot = nil
	req.QueuedVersion = 0
	req.QueuedPath = ""
	req.QueuedSaveAs = false
	req.QueuedPreviousPath = ""
	req.StatusNote = ""
	req.FormattingTried = false
	req.AuthorizedConflictGeneration = 0
	if req.SaveAs {
		req.DiskExpectation = saveDiskMissing
		req.ExpectedDiskSnapshot = nil
	} else {
		req.DiskExpectation = saveDiskExact
		req.ExpectedDiskSnapshot = previousSnapshot
	}
	if req.SaveAs &&
		(m.saveAsDestinationOpenInAnotherEditor(req.EditorID, req.Path) ||
			m.saveAsDestinationReservedByOtherEditor(req.EditorID, req.Path)) {
		delete(m.pendingSaves, requestID)
		if req.QuitAfter {
			m.cancelQuitAfterSaves()
		}
		m.status = fmt.Sprintf(
			"Queued Save As cancelled: %s is already open or reserved",
			filepath.Base(req.Path),
		)
		return nil
	}
	m.pendingSaves[requestID] = req
	return m.startSaveRequest(requestID)
}

func (m Model) saveRequestEditorIndex(req pendingSaveRequest) int {
	if req.EditorID != 0 {
		for i := range m.editors {
			if m.editors[i].ID() == req.EditorID && (req.SaveAs || m.editors[i].Buffer.FilePath == req.Path) {
				return i
			}
		}
		return -1
	}

	tabIndex := req.TabIndex
	if tabIndex < 0 || tabIndex >= len(m.editors) || m.editors[tabIndex].Buffer.FilePath != req.Path {
		tabIndex = m.findEditorByPath(req.Path)
		if tabIndex < 0 {
			return -1
		}
	}
	return tabIndex
}

func (m *Model) completeSaveRequest(requestID int) (pendingSaveRequest, bool) {
	req, ok := m.pendingSaves[requestID]
	if ok {
		delete(m.pendingSaves, requestID)
	}
	return req, ok
}

func (m Model) hasPendingQuitAfterSaves() bool {
	for _, req := range m.pendingSaves {
		if req.QuitAfter {
			return true
		}
	}
	return false
}

func (m *Model) cancelQuitAfterSaves() {
	for requestID, req := range m.pendingSaves {
		if req.QuitAfter {
			req.QuitAfter = false
			m.pendingSaves[requestID] = req
		}
	}
}

func (m *Model) setPendingSaveNote(requestID int, note string) {
	req, ok := m.pendingSaves[requestID]
	if !ok {
		return
	}
	req.StatusNote = note
	req.FormattingTried = true
	m.pendingSaves[requestID] = req
}

func saveSuccessStatus(path, note string) string {
	status := fmt.Sprintf("Saved %s", path)
	if note == "" {
		return status
	}
	return fmt.Sprintf("%s (%s)", status, note)
}

func formatResultNote(status lsp.FormatStatus, err error) string {
	switch status {
	case lsp.FormatNoOp:
		return "no formatting changes"
	case lsp.FormatUnsupported:
		return "formatting not supported"
	case lsp.FormatError:
		if err != nil {
			return fmt.Sprintf("formatting failed: %v", err)
		}
		return "formatting failed"
	default:
		return ""
	}
}

// ensureFormattingDocumentSnapshot synchronizes the immutable document
// snapshot captured when formatting was requested. It deliberately accepts the
// manager snapshot rather than a Model: formatting runs in a tea.Cmd and must
// not observe a later root-model state.
func ensureFormattingDocumentSnapshot(mgr *lsp.Manager, filePath string, version int, snapshot *text.Rope, client *lsp.Client, budgets ...*lspMaterializationBudget) (bool, error) {
	if snapshot == nil {
		return false, nil
	}
	uri := lsp.FileURI(filePath)
	budget := sharedLSPMaterializations
	if len(budgets) > 0 {
		budget = lspMaterializationsFor(budgets[0])
	}
	generation := uint64(0)
	if mgr != nil {
		generation = mgr.DocumentGeneration(filePath)
	}

	if syncedVersion, ok := client.DocumentVersion(uri); ok {
		// Never send an older didChange after normal editor input has already
		// advanced the server document. The format result would be stale too.
		if syncedVersion > version {
			return false, nil
		}
		if syncedVersion == version {
			return true, nil
		}
		content, ok := budget.materializeSnapshot(snapshot, nil)
		if !ok {
			return false, nil
		}
		if mgr != nil {
			mgr.ChangeDocument(filePath, generation, version, content)
		} else {
			client.DidChange(uri, version, content)
		}
		return true, nil
	}

	if _, ok := client.DocumentVersion(uri); !ok {
		content, materialized := budget.materializeSnapshot(snapshot, nil)
		if !materialized {
			return false, nil
		}
		langID := ""
		if mgr != nil {
			if serverCfg := mgr.ConfigForFile(filePath); serverCfg != nil {
				langID = serverCfg.LanguageID
			}
		}
		if mgr != nil {
			if err := mgr.OpenDocument(filePath, generation, langID, version, content); err != nil {
				return false, err
			}
		} else {
			client.DidOpen(uri, langID, version, content)
		}
		return true, nil
	}
	return true, nil
}

func (m Model) requestFormatting(filePath string, cfg editor.Config, requestID int) tea.Cmd {
	idx := m.findEditorByPath(filePath)
	if idx < 0 {
		return nil
	}
	buf := m.editors[idx].Buffer
	return m.requestFormattingSnapshot(filePath, cfg, requestID, buf.Version(), buf.Rope())
}

func (m Model) requestFormattingSnapshot(filePath string, cfg editor.Config, requestID, baseVersion int, snapshot *text.Rope) tea.Cmd {
	if filePath == "" {
		return nil
	}

	mgr := m.lspMgr
	if mgr == nil {
		return nil
	}
	budget := m.lspMaterializationBudget()
	options := formattingOptions(cfg)
	return func() tea.Msg {
		client, err := mgr.EnsureClient(filePath)
		if err != nil {
			return lsp.FormatResultMsg{
				RequestID:      requestID,
				FilePath:       filePath,
				BaseVersion:    baseVersion,
				HasBaseVersion: true,
				Status:         lsp.FormatError,
				Err:            err,
			}
		}
		if client == nil {
			return lsp.FormatResultMsg{RequestID: requestID, FilePath: filePath, BaseVersion: baseVersion, HasBaseVersion: true, Status: lsp.FormatUnsupported}
		}
		if !client.SupportsFormatting() {
			return lsp.FormatResultMsg{RequestID: requestID, FilePath: filePath, BaseVersion: baseVersion, HasBaseVersion: true, Status: lsp.FormatUnsupported}
		}

		synced, syncErr := ensureFormattingDocumentSnapshot(mgr, filePath, baseVersion, snapshot, client, budget)
		if syncErr != nil {
			return lsp.FormatResultMsg{
				RequestID:      requestID,
				FilePath:       filePath,
				BaseVersion:    baseVersion,
				HasBaseVersion: true,
				Status:         lsp.FormatError,
				Err:            syncErr,
			}
		}
		if !synced {
			return lsp.FormatResultMsg{RequestID: requestID, FilePath: filePath, BaseVersion: baseVersion, HasBaseVersion: true, Status: lsp.FormatNoOp}
		}

		edits, err := client.Formatting(lsp.FileURI(filePath), options)
		if err != nil {
			return lsp.FormatResultMsg{
				RequestID:      requestID,
				FilePath:       filePath,
				BaseVersion:    baseVersion,
				HasBaseVersion: true,
				Status:         lsp.FormatError,
				Err:            err,
			}
		}
		if len(edits) == 0 {
			return lsp.FormatResultMsg{RequestID: requestID, FilePath: filePath, BaseVersion: baseVersion, HasBaseVersion: true, Status: lsp.FormatNoOp}
		}
		return lsp.FormatResultMsg{
			RequestID:      requestID,
			FilePath:       filePath,
			BaseVersion:    baseVersion,
			HasBaseVersion: true,
			Status:         lsp.FormatApplied,
			Edits:          edits,
		}
	}
}
