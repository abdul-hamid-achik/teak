package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/lsp"
	"teak/internal/text"
)

// A workspace edit can otherwise ask the UI goroutine to retain a 64 MiB
// snapshot for every changed document. Keep both the input and output sides
// bounded across the whole request, not merely per file.
const maxWorkspaceEditAggregateBytes int64 = 128 << 20

const maxQueuedWorkspaceEdits = 16

type workspaceEditBufferSnapshot struct {
	EditorID uint64
	Path     string
	Version  int
	Cursor   text.Position
	Rope     *text.Rope
}

type workspaceEditPreparedDocument struct {
	Path         string
	RelativePath string
	EditorID     uint64
	Version      int
	Source       *text.Rope
	Result       *text.Rope
	Cursor       text.Position
	Applied      int
	Closed       bool
	Data         []byte // only populated for a closed document, off the UI path
	SourceDigest [sha256.Size]byte
	HasDigest    bool
}

type workspaceEditPreparation struct {
	RootInfo            os.FileInfo
	Documents           []workspaceEditPreparedDocument
	Operation           *lsp.WorkspaceFileOperation
	OperationSourceInfo os.FileInfo
}

// workspaceEditContinuation is deliberately UI-only. In particular, an LSP
// applyEdit response is sent exactly once from the result path, while a code
// action command starts only after its edit has committed.
type workspaceEditContinuation struct {
	Context       context.Context
	Claim         func() bool
	Respond       func(bool, string)
	CodeAction    *lsp.CodeAction
	CodeActionURI string
}

type workspaceEditRequest struct {
	Edit         lsp.WorkspaceEdit
	Continuation workspaceEditContinuation
}

// workspaceEditState serializes disk commits. A newer request queues instead
// of racing an older one, so a result generation is meaningful and every
// server applyEdit callback receives exactly one eventual decision.
type workspaceEditState struct {
	inFlight   bool
	generation uint64
	ctx        context.Context
	cancel     context.CancelFunc
	queued     []workspaceEditRequest
}

type workspaceEditPreparedMsg struct {
	Generation   uint64
	Preparation  workspaceEditPreparation
	Continuation workspaceEditContinuation
	Err          error
}

type workspaceEditCommitResultMsg struct {
	Generation   uint64
	Preparation  workspaceEditPreparation
	Continuation workspaceEditContinuation
	Err          error
}

func (m Model) startWorkspaceEditAsync(edit lsp.WorkspaceEdit, continuation workspaceEditContinuation) (Model, tea.Cmd) {
	if m.workspaceEdits.inFlight {
		if len(m.workspaceEdits.queued) >= maxQueuedWorkspaceEdits {
			if continuation.Respond != nil {
				continuation.Respond(false, "too many pending workspace edits")
			}
			m.status = "Workspace edit rejected: too many pending requests"
			return m, nil
		}
		m.workspaceEdits.queued = append(m.workspaceEdits.queued, workspaceEditRequest{Edit: edit, Continuation: continuation})
		m.status = "Workspace edit queued..."
		return m, nil
	}
	return m.startNextWorkspaceEdit(workspaceEditRequest{Edit: edit, Continuation: continuation})
}

func (m Model) startNextWorkspaceEdit(request workspaceEditRequest) (Model, tea.Cmd) {
	m.workspaceEdits.inFlight = true
	m.workspaceEdits.generation++
	generation := m.workspaceEdits.generation
	parentCtx := request.Continuation.Context
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(parentCtx)
	m.workspaceEdits.ctx = ctx
	m.workspaceEdits.cancel = cancel
	snapshots := make([]workspaceEditBufferSnapshot, 0, len(m.editors))
	targetPaths := workspaceEditTargetPaths(m.rootDir, request.Edit)
	seenPaths := make(map[string]struct{}, len(m.editors))
	for index := range m.editors {
		buf := m.editors[index].Buffer
		if buf.FilePath == "" {
			continue
		}
		absolutePath, err := filepath.Abs(buf.FilePath)
		if err != nil {
			continue
		}
		absolutePath = filepath.Clean(absolutePath)
		if _, duplicate := seenPaths[absolutePath]; duplicate && targetPaths[absolutePath] {
			if request.Continuation.Respond != nil {
				request.Continuation.Respond(false, "workspace edit target is open in multiple tabs")
			}
			m.workspaceEdits.inFlight = false
			m.workspaceEdits.ctx = nil
			m.workspaceEdits.cancel()
			m.workspaceEdits.cancel = nil
			m.status = "Workspace edit rejected: target is open in multiple tabs"
			return m, nil
		}
		seenPaths[absolutePath] = struct{}{}
		snapshots = append(snapshots, workspaceEditBufferSnapshot{
			EditorID: m.editors[index].ID(),
			Path:     absolutePath,
			Version:  buf.Version(),
			Cursor:   buf.Cursor,
			Rope:     buf.Rope(),
		})
	}
	rootDir, pinnedRoot := m.rootDir, m.agentWriteRoot
	m.status = "Preparing workspace edit..."
	return m, prepareWorkspaceEditCmd(ctx, generation, rootDir, pinnedRoot, request.Edit, snapshots, request.Continuation)
}

func workspaceEditTargetPaths(rootDir string, edit lsp.WorkspaceEdit) map[string]bool {
	targets := make(map[string]bool, len(edit.Changes)+len(edit.DocumentChanges))
	addURI := func(uri string) {
		_, path, err := workspaceRelativePathForRoot(rootDir, lsp.URIToPath(uri))
		if err == nil {
			targets[filepath.Clean(path)] = true
		}
	}
	for uri := range edit.Changes {
		addURI(uri)
	}
	for _, change := range edit.DocumentChanges {
		if change.FileOperation != nil {
			switch change.FileOperation.Kind {
			case lsp.FileOpDelete, lsp.FileOpCreate:
				addURI(change.FileOperation.URI)
			case lsp.FileOpRename:
				addURI(change.FileOperation.OldURI)
				addURI(change.FileOperation.NewURI)
			}
			continue
		}
		addURI(change.URI)
	}
	return targets
}

func prepareWorkspaceEditCmd(
	ctx context.Context,
	generation uint64,
	rootDir string,
	pinnedRoot *os.Root,
	edit lsp.WorkspaceEdit,
	snapshots []workspaceEditBufferSnapshot,
	continuation workspaceEditContinuation,
) tea.Cmd {
	return func() tea.Msg {
		preparation, err := prepareWorkspaceEdit(ctx, rootDir, pinnedRoot, edit, snapshots)
		return workspaceEditPreparedMsg{
			Generation: generation, Preparation: preparation, Continuation: continuation, Err: err,
		}
	}
}

// prepareWorkspaceEdit owns all filesystem and rope work. The resulting ropes
// share immutable leaves with the snapshots, so Update only swaps a prepared
// pointer after rechecking the editor identities and versions it captured.
func prepareWorkspaceEdit(
	ctx context.Context,
	rootDir string,
	pinnedRoot *os.Root,
	edit lsp.WorkspaceEdit,
	snapshots []workspaceEditBufferSnapshot,
) (workspaceEditPreparation, error) {
	if err := ctx.Err(); err != nil {
		return workspaceEditPreparation{}, err
	}
	if len(edit.DocumentChanges) == 0 && len(edit.Changes) == 0 {
		return workspaceEditPreparation{}, nil
	}
	if len(edit.DocumentChanges) > 0 {
		fileOperations := 0
		for _, change := range edit.DocumentChanges {
			if change.FileOperation != nil {
				fileOperations++
			}
		}
		if fileOperations > 0 && len(edit.DocumentChanges) != 1 {
			return workspaceEditPreparation{}, fmt.Errorf("mixed or multi-file operations are not atomic")
		}
	}

	root, closeRoot, err := openWorkspaceEditRoot(rootDir, pinnedRoot)
	if err != nil {
		return workspaceEditPreparation{}, err
	}
	if closeRoot {
		defer func() { _ = root.Close() }()
	}
	rootInfo, err := root.Stat(".")
	if err != nil || !rootInfo.IsDir() {
		if err == nil {
			err = fmt.Errorf("workspace root is not a directory")
		}
		return workspaceEditPreparation{}, err
	}

	if len(edit.DocumentChanges) == 1 && edit.DocumentChanges[0].FileOperation != nil {
		op := *edit.DocumentChanges[0].FileOperation
		if err := preflightWorkspaceFileOperationAtRoot(root, rootDir, op); err != nil {
			return workspaceEditPreparation{}, err
		}
		preparation := workspaceEditPreparation{RootInfo: rootInfo, Operation: &op}
		if op.Kind == lsp.FileOpDelete || op.Kind == lsp.FileOpRename {
			uri := op.URI
			if op.Kind == lsp.FileOpRename {
				uri = op.OldURI
			}
			relativePath, _, err := workspaceRelativePathForRoot(rootDir, lsp.URIToPath(uri))
			if err != nil {
				return workspaceEditPreparation{}, err
			}
			preparation.OperationSourceInfo, err = root.Stat(relativePath)
			if err != nil {
				return workspaceEditPreparation{}, err
			}
		}
		return preparation, nil
	}

	snapshotByPath := make(map[string]workspaceEditBufferSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		path, err := filepath.Abs(snapshot.Path)
		if err == nil {
			snapshotByPath[filepath.Clean(path)] = snapshot
		}
	}

	type workingDocument struct {
		workspaceEditPreparedDocument
		buffer *text.Buffer
	}
	working := make(map[string]*workingDocument)
	ordered := make([]*workingDocument, 0)
	budget := workspaceEditBudget{}
	var inputBytes int64

	prepareOne := func(uri string, edits []lsp.TextEdit, expectedVersion *int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		relativePath, path, err := workspaceRelativePathForRoot(rootDir, lsp.URIToPath(uri))
		if err != nil {
			return err
		}
		path = filepath.Clean(path)
		doc := working[path]
		if doc == nil {
			doc = &workingDocument{workspaceEditPreparedDocument: workspaceEditPreparedDocument{
				Path: path, RelativePath: relativePath,
			}}
			if snapshot, open := snapshotByPath[path]; open {
				if expectedVersion != nil && snapshot.Version != *expectedVersion {
					return fmt.Errorf("document version mismatch for %s: got %d, want %d", filepath.Base(path), snapshot.Version, *expectedVersion)
				}
				if snapshot.Rope == nil || snapshot.Rope.Len() > int(maxWorkspaceEditAggregateBytes-inputBytes) {
					return fmt.Errorf("workspace edit exceeds %d MiB aggregate document budget", maxWorkspaceEditAggregateBytes>>20)
				}
				inputBytes += int64(snapshot.Rope.Len())
				doc.EditorID, doc.Version, doc.Source = snapshot.EditorID, snapshot.Version, snapshot.Rope
				doc.buffer = text.NewBufferFromRope(snapshot.Rope)
				doc.buffer.SetCursor(snapshot.Cursor)
			} else {
				if expectedVersion != nil {
					return fmt.Errorf("cannot verify document version for unopened file %s", filepath.Base(path))
				}
				remaining := maxWorkspaceEditAggregateBytes - inputBytes
				data, err := readWorkspaceEditFile(ctx, root, relativePath, remaining)
				if err != nil {
					return fmt.Errorf("read workspace edit file %s: %w", filepath.Base(path), err)
				}
				inputBytes += int64(len(data))
				doc.Closed = true
				doc.SourceDigest, doc.HasDigest = sha256.Sum256(data), true
				doc.buffer = text.NewBufferFromBytes(data)
			}
			working[path] = doc
			ordered = append(ordered, doc)
		} else if expectedVersion != nil && (doc.EditorID == 0 || doc.Version != *expectedVersion) {
			return fmt.Errorf("document version mismatch for %s", filepath.Base(path))
		}

		budget.documents++
		budget.edits += len(edits)
		if budget.documents > maxWorkspaceEditDocuments {
			return fmt.Errorf("workspace edit exceeds %d document changes", maxWorkspaceEditDocuments)
		}
		if budget.edits > maxWorkspaceTextEdits {
			return fmt.Errorf("workspace edit exceeds %d text edits", maxWorkspaceTextEdits)
		}
		for _, textEdit := range edits {
			if len(textEdit.NewText) > maxWorkspaceNewTextBytes-budget.newBytes {
				return fmt.Errorf("workspace edit exceeds %d bytes of replacement text", maxWorkspaceNewTextBytes)
			}
			budget.newBytes += len(textEdit.NewText)
		}
		if err := validateTextEditRanges(doc.buffer, edits); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		doc.Applied += applyTextEditsToBuffer(doc.buffer, edits)
		if int64(doc.buffer.Rope().Len()) > maxWorkspaceEditAggregateBytes {
			return fmt.Errorf("%s would exceed the workspace edit aggregate budget", filepath.Base(path))
		}
		return nil
	}

	if len(edit.DocumentChanges) > 0 {
		for _, change := range edit.DocumentChanges {
			if err := prepareOne(change.URI, change.Edits, change.Version); err != nil {
				return workspaceEditPreparation{}, err
			}
		}
	} else {
		uris := make([]string, 0, len(edit.Changes))
		for uri := range edit.Changes {
			uris = append(uris, uri)
		}
		slices.Sort(uris)
		for _, uri := range uris {
			if err := prepareOne(uri, edit.Changes[uri], nil); err != nil {
				return workspaceEditPreparation{}, err
			}
		}
	}

	var outputBytes int64
	closedDocuments := 0
	for _, doc := range ordered {
		if doc.Closed {
			closedDocuments++
		}
	}
	// There is no portable transaction spanning a live Rope swap and an
	// atomic filesystem replace, nor one spanning two filesystem replaces.
	// Reject those combinations before any mutation rather than acknowledge a
	// partially applied workspace edit after a late write/cancellation error.
	if closedDocuments > 1 {
		return workspaceEditPreparation{}, errors.New("workspace edit touches multiple unopened files and cannot be applied atomically")
	}
	if closedDocuments > 0 && closedDocuments != len(ordered) {
		return workspaceEditPreparation{}, errors.New("workspace edit mixes open and unopened files and cannot be applied atomically")
	}
	preparation := workspaceEditPreparation{RootInfo: rootInfo, Documents: make([]workspaceEditPreparedDocument, 0, len(ordered))}
	for _, doc := range ordered {
		if err := ctx.Err(); err != nil {
			return workspaceEditPreparation{}, err
		}
		doc.Result, doc.Cursor = doc.buffer.Rope(), doc.buffer.Cursor
		outputBytes += int64(doc.Result.Len())
		if outputBytes > maxWorkspaceEditAggregateBytes {
			return workspaceEditPreparation{}, fmt.Errorf("workspace edit exceeds %d MiB aggregate document budget", maxWorkspaceEditAggregateBytes>>20)
		}
		if doc.Closed {
			doc.Data = doc.Result.Bytes()
			doc.Result = nil // closed commits consume Data; do not retain two full copies
		}
		preparation.Documents = append(preparation.Documents, doc.workspaceEditPreparedDocument)
	}
	return preparation, nil
}

func openWorkspaceEditRoot(rootDir string, pinnedRoot *os.Root) (*os.Root, bool, error) {
	if pinnedRoot != nil {
		return pinnedRoot, false, nil
	}
	if rootDir == "" {
		return nil, false, fmt.Errorf("workspace root is unavailable")
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, false, fmt.Errorf("open workspace root: %w", err)
	}
	return root, true, nil
}

func workspaceRelativePathForRoot(rootDir, path string) (string, string, error) {
	if rootDir == "" || path == "" {
		return "", "", fmt.Errorf("workspace path is unavailable")
	}
	rootPath, err := filepath.Abs(rootDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace root: %w", err)
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace path: %w", err)
	}
	relativePath, err := filepath.Rel(rootPath, absolutePath)
	if err != nil {
		return "", "", fmt.Errorf("relativize workspace path: %w", err)
	}
	relativePath = filepath.Clean(relativePath)
	if relativePath == "." || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) || filepath.IsAbs(relativePath) {
		return "", "", fmt.Errorf("path %q is outside workspace %q", path, rootPath)
	}
	return relativePath, filepath.Join(rootPath, relativePath), nil
}

func readWorkspaceEditFile(ctx context.Context, root *os.Root, relativePath string, remaining int64) ([]byte, error) {
	if remaining <= 0 {
		return nil, fmt.Errorf("workspace edit aggregate budget exhausted")
	}
	file, err := openWorkspaceEditInput(root, relativePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	limit := min(maxEditorFileBytes, remaining)
	data, _, err := readOpenedEditorFile(ctx, file, relativePath, limit)
	return data, err
}

func workspaceEditSnapshotsCurrent(m Model, preparation workspaceEditPreparation) bool {
	for _, doc := range preparation.Documents {
		if doc.EditorID == 0 {
			if workspaceEditEditorIndexForPath(m, doc.Path) >= 0 {
				return false // it became live after this background snapshot
			}
			continue
		}
		index := m.editorIndexForAsyncMessage(doc.EditorID)
		if index < 0 || !workspaceEditSamePath(m.editors[index].Buffer.FilePath, doc.Path) || m.editors[index].Buffer.Version() != doc.Version || m.editors[index].Buffer.Rope() != doc.Source {
			return false
		}
	}
	return true
}

func workspaceEditEditorIndexForPath(m Model, path string) int {
	for index := range m.editors {
		if workspaceEditSamePath(m.editors[index].Buffer.FilePath, path) {
			return index
		}
	}
	return -1
}

func workspaceEditSamePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func workspaceEditCommitCmd(ctx context.Context, generation uint64, rootDir string, pinnedRoot *os.Root, preparation workspaceEditPreparation, continuation workspaceEditContinuation) tea.Cmd {
	return func() tea.Msg {
		err := ctx.Err()
		var root *os.Root
		var closeRoot bool
		if err == nil {
			root, closeRoot, err = openWorkspaceEditRoot(rootDir, pinnedRoot)
		}
		if err == nil && closeRoot {
			defer func() { _ = root.Close() }()
		}
		if err == nil {
			var currentRoot os.FileInfo
			currentRoot, err = root.Stat(".")
			if err == nil && (preparation.RootInfo == nil || !os.SameFile(currentRoot, preparation.RootInfo)) {
				err = errors.New("workspace root changed while applying edit")
			}
		}
		if err == nil && preparation.Operation != nil {
			err = ctx.Err()
		}
		if err == nil && preparation.Operation != nil {
			err = applyWorkspaceFileOperationAtRoot(root, rootDir, *preparation.Operation, preparation.OperationSourceInfo)
		}
		if err == nil {
			// Verify every closed input before changing any of them. This closes
			// the prepare→commit window for normal external edits and avoids a
			// partial workspace edit when just one source became stale.
			var verifiedBytes int64
			for _, doc := range preparation.Documents {
				if !doc.Closed || !doc.HasDigest {
					continue
				}
				remaining := maxWorkspaceEditAggregateBytes - verifiedBytes
				data, readErr := readWorkspaceEditFile(ctx, root, doc.RelativePath, remaining)
				if readErr != nil {
					err = fmt.Errorf("revalidate workspace edit file %s: %w", filepath.Base(doc.Path), readErr)
					break
				}
				verifiedBytes += int64(len(data))
				if sha256.Sum256(data) != doc.SourceDigest {
					err = fmt.Errorf("workspace file changed while edit was being prepared: %s", filepath.Base(doc.Path))
					break
				}
			}
		}
		if err == nil {
			for _, doc := range preparation.Documents {
				if !doc.Closed {
					continue
				}
				if err = ctx.Err(); err != nil {
					break
				}
				if err = writeAgentFileAtomicRoot(ctx, root, doc.RelativePath, doc.Data); err != nil {
					break
				}
			}
		}
		return workspaceEditCommitResultMsg{Generation: generation, Preparation: preparation, Continuation: continuation, Err: err}
	}
}

func applyWorkspaceFileOperationAtRoot(root *os.Root, rootDir string, op lsp.WorkspaceFileOperation, sourceInfo os.FileInfo) error {
	switch op.Kind {
	case lsp.FileOpCreate:
		target, _, err := workspaceRelativePathForRoot(rootDir, lsp.URIToPath(op.URI))
		if err != nil {
			return err
		}
		if err := mkdirAllAgentWrite(root, filepath.Dir(target), 0o755); err != nil {
			return err
		}
		file, err := root.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		return file.Close()
	case lsp.FileOpDelete:
		target, _, err := workspaceRelativePathForRoot(rootDir, lsp.URIToPath(op.URI))
		if err != nil {
			return err
		}
		info, err := root.Stat(target)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || (sourceInfo != nil && !os.SameFile(info, sourceInfo)) {
			return fmt.Errorf("delete source changed while workspace edit was being prepared")
		}
		return root.Remove(target)
	case lsp.FileOpRename:
		oldRelative, _, err := workspaceRelativePathForRoot(rootDir, lsp.URIToPath(op.OldURI))
		if err != nil {
			return err
		}
		newRelative, _, err := workspaceRelativePathForRoot(rootDir, lsp.URIToPath(op.NewURI))
		if err != nil {
			return err
		}
		if err := mkdirAllAgentWrite(root, filepath.Dir(newRelative), 0o755); err != nil {
			return err
		}
		info, err := root.Stat(oldRelative)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || (sourceInfo != nil && !os.SameFile(info, sourceInfo)) {
			return fmt.Errorf("rename source changed while workspace edit was being prepared")
		}
		if _, err := root.Stat(newRelative); err == nil {
			return fmt.Errorf("rename target appeared while workspace edit was being prepared")
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect rename target: %w", err)
		}
		// os.Root does not expose a portable no-replace rename. The immediate
		// recheck above prevents ordinary races, but a hostile concurrent
		// creator can still win between it and Renameat/Root.Rename.
		return renameWorkspacePath(root, oldRelative, newRelative)
	default:
		return fmt.Errorf("unsupported workspace file operation %q", op.Kind)
	}
}

func (m Model) handleWorkspaceEditPrepared(msg workspaceEditPreparedMsg) (tea.Model, tea.Cmd) {
	if !m.workspaceEdits.inFlight || msg.Generation != m.workspaceEdits.generation {
		return m, nil
	}
	if msg.Continuation.Context != nil && msg.Continuation.Context.Err() != nil {
		return m.completeWorkspaceEdit(msg.Generation, msg.Continuation, false, msg.Continuation.Context.Err().Error(), 0)
	}
	if msg.Err != nil {
		return m.completeWorkspaceEdit(msg.Generation, msg.Continuation, false, msg.Err.Error(), 0)
	}
	if !workspaceEditSnapshotsCurrent(m, msg.Preparation) {
		return m.completeWorkspaceEdit(msg.Generation, msg.Continuation, false, "document changed while workspace edit was being prepared", 0)
	}
	if msg.Continuation.Claim != nil && !msg.Continuation.Claim() {
		return m.completeWorkspaceEdit(msg.Generation, msg.Continuation, false, "workspace edit timed out before it could be applied", 0)
	}

	type previousEditorState struct {
		version int
		cursor  text.Position
	}
	previous := make(map[uint64]previousEditorState, len(msg.Preparation.Documents))
	applied := 0
	needsCommit := msg.Preparation.Operation != nil
	for _, doc := range msg.Preparation.Documents {
		applied += doc.Applied
		if doc.Closed {
			needsCommit = true
			continue
		}
		index := m.editorIndexForAsyncMessage(doc.EditorID)
		if index < 0 {
			return m.completeWorkspaceEdit(msg.Generation, msg.Continuation, false, "target editor was closed", 0)
		}
		previous[doc.EditorID] = previousEditorState{version: m.editors[index].Buffer.Version(), cursor: m.editors[index].Buffer.Cursor}
		m.editors[index].Buffer.ReplaceRopeSnapshot(doc.Result, doc.Cursor)
		if m.editors[index].Highlighter != nil {
			m.editors[index].Highlighter.Invalidate()
		}
	}

	var postMutation []tea.Cmd
	for editorID, before := range previous {
		index := m.editorIndexForAsyncMessage(editorID)
		if index < 0 || m.editors[index].Buffer.Version() == before.version {
			continue
		}
		id, version := m.editors[index].ID(), m.editors[index].Buffer.Version()
		postMutation = append(postMutation,
			func() tea.Msg { return editor.RetokenizeMsg{EditorID: id, Version: version} },
			m.syncEditorStateAfterUpdate(index, before.version, before.cursor),
		)
	}

	if !needsCommit {
		return m.completeWorkspaceEdit(msg.Generation, msg.Continuation, true, "", applied, tea.Batch(postMutation...))
	}
	m.status = "Applying workspace edit..."
	commitCtx := m.workspaceEdits.ctx
	if commitCtx == nil {
		return m.completeWorkspaceEdit(msg.Generation, msg.Continuation, false, "workspace edit was cancelled", applied, tea.Batch(postMutation...))
	}
	commit := workspaceEditCommitCmd(commitCtx, msg.Generation, m.rootDir, m.agentWriteRoot, msg.Preparation, msg.Continuation)
	return m, tea.Batch(append(postMutation, commit)...)
}

func (m Model) handleWorkspaceEditCommitResult(msg workspaceEditCommitResultMsg) (tea.Model, tea.Cmd) {
	if !m.workspaceEdits.inFlight || msg.Generation != m.workspaceEdits.generation {
		return m, nil
	}
	applied := 0
	for _, doc := range msg.Preparation.Documents {
		applied += doc.Applied
	}
	if msg.Preparation.Operation != nil {
		if msg.Err == nil {
			applied++
			if msg.Preparation.Operation.Kind == lsp.FileOpRename {
				_, oldPath, oldErr := workspaceRelativePathForRoot(m.rootDir, lsp.URIToPath(msg.Preparation.Operation.OldURI))
				_, newPath, newErr := workspaceRelativePathForRoot(m.rootDir, lsp.URIToPath(msg.Preparation.Operation.NewURI))
				if oldErr == nil && newErr == nil {
					if index := m.findEditorByPath(oldPath); index >= 0 {
						m.editors[index].Buffer.FilePath = newPath
					}
				}
			}
		}
	}
	if msg.Err != nil {
		return m.completeWorkspaceEdit(msg.Generation, msg.Continuation, false, msg.Err.Error(), applied)
	}
	return m.completeWorkspaceEdit(msg.Generation, msg.Continuation, true, "", applied)
}

func (m Model) completeWorkspaceEdit(generation uint64, continuation workspaceEditContinuation, success bool, reason string, applied int, cmds ...tea.Cmd) (tea.Model, tea.Cmd) {
	if !m.workspaceEdits.inFlight || generation != m.workspaceEdits.generation {
		return m, nil
	}
	if m.workspaceEdits.cancel != nil {
		m.workspaceEdits.cancel()
		m.workspaceEdits.cancel = nil
	}
	m.workspaceEdits.ctx = nil
	m.workspaceEdits.inFlight = false
	finishedAny, finishedCmd := m.finishWorkspaceEdit(continuation, success, reason, applied, cmds...)
	finished := finishedAny.(Model)
	if len(finished.workspaceEdits.queued) == 0 {
		return finished, finishedCmd
	}
	next := finished.workspaceEdits.queued[0]
	finished.workspaceEdits.queued[0] = workspaceEditRequest{}
	finished.workspaceEdits.queued = finished.workspaceEdits.queued[1:]
	updated, nextCmd := finished.startNextWorkspaceEdit(next)
	return updated, tea.Batch(finishedCmd, nextCmd)
}

// finishWorkspaceEdit is the only place where an incoming applyEdit callback
// is invoked. The LSP client additionally protects the callback with sync.Once
// in case its transport timeout wins the race.
func (m Model) finishWorkspaceEdit(continuation workspaceEditContinuation, success bool, reason string, applied int, cmds ...tea.Cmd) (tea.Model, tea.Cmd) {
	if continuation.Respond != nil {
		continuation.Respond(success, reason)
	}
	if !success {
		if continuation.CodeAction != nil {
			m.status = fmt.Sprintf("Code action %q was not applied; server command not executed", continuation.CodeAction.Title)
		} else {
			m.status = fmt.Sprintf("Workspace edit rejected: %s", reason)
		}
		return m, tea.Batch(cmds...)
	}

	if continuation.CodeAction != nil {
		if continuation.CodeAction.Command == nil {
			m.status = fmt.Sprintf("Applied: %s", continuation.CodeAction.Title)
			return m, tea.Batch(cmds...)
		}
		updated, command := m.executeSelectedCodeActionForFile(*continuation.CodeAction, continuation.CodeActionURI)
		return updated, tea.Sequence(tea.Batch(cmds...), command)
	}
	if applied > 0 {
		m.status = fmt.Sprintf("Workspace edit applied: %d change(s)", applied)
	} else {
		m.status = "Workspace edit completed"
	}
	return m, tea.Batch(cmds...)
}
