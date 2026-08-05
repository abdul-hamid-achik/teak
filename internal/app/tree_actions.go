package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"teak/internal/clipboard"
	"teak/internal/plugin"
	"teak/internal/search"
)

// treeActionKind identifies the filesystem mutation that produced a result.
type treeActionKind uint8

const (
	treeActionCreate treeActionKind = iota + 1
	treeActionDelete
	treeActionRename
	treeActionCopy
	treeActionMove
)

// treeActionRequest is a complete, immutable filesystem request. Its paths
// are relative to Root, so every operation remains confined even if a
// directory is replaced with a symlink between Update and command execution.
type treeActionRequest struct {
	Generation              uint64
	Kind                    treeActionKind
	Root                    *os.Root
	RootPath                string
	RelativePath            string
	ParentRelativePath      string
	DestinationRelativePath string
	SourcePath              string
	Path                    string
	Name                    string
	IsFolder                bool
	DirtyPaths              []string
}

type treeActionOutcome struct {
	Path        string
	TargetIsDir bool
	Committed   bool
	CleanupErr  error
	Err         error
}

// treeActionResultMsg is delivered after all potentially blocking filesystem
// calls have completed. Committed means the filesystem mutation happened even
// when a later cleanup step or cancellation reported an error.
type treeActionResultMsg struct {
	Generation  uint64
	Kind        treeActionKind
	SourcePath  string
	Path        string
	Name        string
	IsFolder    bool
	TargetIsDir bool
	Committed   bool
	CleanupErr  error
	Err         error
}

var errTreeTargetHasUnsavedChanges = errors.New("an affected file has unsaved changes")

const (
	treeTrashPayloadName    = "payload"
	treeTrashCreateAttempts = 32
	treeCleanupNodeBudget   = 128
	treeCleanupMaxBudget    = 2_048
	treeCleanupMaxOpenDirs  = 256
)

var (
	treeTrashSequence atomic.Uint64
)

// runTreeAction is a narrow test seam for simulating a slow filesystem. The
// production implementation always performs its work in a tea.Cmd.
var runTreeAction = executeTreeAction

// treeActionAfterCommit is a test seam for cancellation arriving immediately
// after the filesystem's logical commit. Production leaves it as a no-op.
var treeActionAfterCommit = func(treeActionRequest) {}

// treeCopyPathResultMsg separates a potentially slow system clipboard command
// from the Bubble Tea update loop. Copy may succeed only in Teak's in-process
// fallback when the operating-system clipboard is unavailable, so the result
// carries the error for an honest status message.
type treeCopyPathResultMsg struct {
	Path string
	Err  error
}

func treeCopyPathCmd(path string) tea.Cmd {
	return func() tea.Msg {
		return treeCopyPathResultMsg{Path: path, Err: clipboard.Copy(path)}
	}
}

func (m Model) handleTreeCopyPathResult(msg treeCopyPathResultMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.status = fmt.Sprintf("Copied to Teak clipboard (%s; system clipboard unavailable)", msg.Path)
		return m, nil
	}
	m.status = fmt.Sprintf("Copied: %s", msg.Path)
	return m, nil
}

// validTreeEntryName deliberately accepts a single filename only. A tree
// create dialog is not a path prompt: accepting separators makes an accidental
// or malicious ../../ escape far too easy and makes its destination ambiguous.
func validTreeEntryName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("name must not be empty, a single dot, or two dots")
	}
	if strings.IndexByte(name, 0) >= 0 {
		return fmt.Errorf("name contains a NUL byte")
	}
	if strings.ContainsAny(name, `/\\`) || filepath.Base(name) != name {
		return fmt.Errorf("name must not contain path separators")
	}
	return nil
}

// treeRelativeDirectory is the directory variant of workspaceRelativePath.
// It permits the workspace root itself ("."), but rejects a lexical escape.
// Actual traversal is still performed through os.Root below, which rejects a
// symlink that points outside the pinned workspace.
func (m *Model) treeRelativeDirectory(path string) (string, string, error) {
	if m.rootDir == "" || path == "" {
		return "", "", fmt.Errorf("workspace directory is unavailable")
	}
	root, err := filepath.Abs(m.rootDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace root: %w", err)
	}
	dir, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve destination directory: %w", err)
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return "", "", fmt.Errorf("relativize destination directory: %w", err)
	}
	rel = filepath.Clean(rel)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("directory is outside the workspace")
	}
	return rel, root, nil
}

// startTreeCreate validates the UI request and snapshots every value needed by
// the background command. It intentionally performs no filesystem I/O.
func (m *Model) startTreeCreate(dir, name string, isFolder bool) tea.Cmd {
	if err := validTreeEntryName(name); err != nil {
		m.status = fmt.Sprintf("Invalid name: %v", err)
		return nil
	}
	relDir, rootPath, err := m.treeRelativeDirectory(dir)
	if err != nil {
		if isFolder {
			m.status = fmt.Sprintf("Error creating folder: %v", err)
		} else {
			m.status = fmt.Sprintf("Error creating file: %v", err)
		}
		return nil
	}
	relPath := filepath.Join(relDir, name)
	return m.startTreeAction(treeActionRequest{
		Kind:               treeActionCreate,
		Root:               m.agentWriteRoot,
		RootPath:           rootPath,
		RelativePath:       relPath,
		ParentRelativePath: relDir,
		Path:               filepath.Join(rootPath, relPath),
		Name:               name,
		IsFolder:           isFolder,
	})
}

// startTreeTransfer prepares a rename, copy, or move without touching the
// filesystem on the UI goroutine. destinationDir is resolved to an absolute
// workspace path and name is always one path component.
func (m *Model) startTreeTransfer(target, destinationDir, name string, kind treeActionKind) tea.Cmd {
	if kind != treeActionRename && kind != treeActionCopy && kind != treeActionMove {
		m.status = "Invalid tree transfer"
		return nil
	}
	if err := validTreeEntryName(name); err != nil {
		m.status = fmt.Sprintf("Invalid name: %v", err)
		return nil
	}
	sourceRelative, sourcePath, err := m.workspaceRelativePath(target)
	if err != nil {
		m.status = fmt.Sprintf("Error preparing file operation: %v", err)
		return nil
	}
	destinationRelativeDir, rootPath, err := m.treeRelativeDirectory(destinationDir)
	if err != nil {
		m.status = fmt.Sprintf("Error preparing file operation: %v", err)
		return nil
	}
	destinationRelative := filepath.Join(destinationRelativeDir, name)
	destinationPath := filepath.Join(rootPath, destinationRelative)
	if filepath.Clean(sourcePath) == filepath.Clean(destinationPath) {
		m.status = "Source and destination are the same"
		return nil
	}
	if kind != treeActionCopy {
		for _, request := range m.pendingSaves {
			if isTreeTargetPath(sourcePath, request.Path, true) ||
				isTreeTargetPath(sourcePath, request.PreviousPath, true) {
				m.status = "File operation blocked: save in progress"
				return nil
			}
		}
	}
	return m.startTreeAction(treeActionRequest{
		Kind:                    kind,
		Root:                    m.agentWriteRoot,
		RootPath:                rootPath,
		RelativePath:            sourceRelative,
		ParentRelativePath:      destinationRelativeDir,
		DestinationRelativePath: destinationRelative,
		SourcePath:              sourcePath,
		Path:                    destinationPath,
		Name:                    name,
	})
}

func (m *Model) startTreeRename(target, name string) tea.Cmd {
	return m.startTreeTransfer(target, filepath.Dir(target), name, treeActionRename)
}

func (m *Model) startTreeCopy(target, name string) tea.Cmd {
	return m.startTreeTransfer(target, filepath.Dir(target), name, treeActionCopy)
}

func (m *Model) startTreeMove(target, destinationDir string) tea.Cmd {
	if destinationDir == "" {
		m.status = "Move destination is empty"
		return nil
	}
	if !filepath.IsAbs(destinationDir) {
		destinationDir = filepath.Join(m.rootDir, destinationDir)
	}
	return m.startTreeTransfer(target, destinationDir, filepath.Base(target), treeActionMove)
}

func isTreeTargetPath(target, candidate string, targetIsDir bool) bool {
	if candidate == "" {
		return false
	}
	target = filepath.Clean(target)
	if absoluteCandidate, err := filepath.Abs(candidate); err == nil {
		candidate = absoluteCandidate
	}
	candidate = filepath.Clean(candidate)
	if target == candidate {
		return true
	}
	if !targetIsDir {
		return false
	}
	rel, err := filepath.Rel(target, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// relocatedTreePath maps an open/project state path from a committed tree
// move or rename to its new location. It returns false for unrelated paths so
// callers can update only the affected tabs and path-keyed caches.
func relocatedTreePath(source, destination, candidate string, targetIsDir bool) (string, bool) {
	if source == "" || destination == "" || candidate == "" || !isTreeTargetPath(source, candidate, targetIsDir) {
		return "", false
	}
	if !targetIsDir {
		return filepath.Clean(destination), true
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return "", false
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", false
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(source, candidate)
	if err != nil {
		return "", false
	}
	if relative == "." {
		return filepath.Clean(destination), true
	}
	return filepath.Join(destination, relative), true
}

// treeCleanupFrame is one open directory in the fast post-order traversal.
// This stack is deliberately capped by treeCleanupMaxOpenDirs. Deeper trees
// switch to nextTreeCleanupLeaf, which holds only one descriptor.
type treeCleanupFrame struct {
	path       string
	name       string
	parentRoot *os.Root
	root       *os.Root
	dir        *os.File
}

func closeTreeCleanupFrames(frames []treeCleanupFrame) {
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i].dir != nil {
			_ = frames[i].dir.Close()
		}
		if frames[i].root != nil {
			_ = frames[i].root.Close()
		}
	}
}

// nextTreeCleanupLeaf finds one leaf (a non-directory or an empty directory)
// without recursion. It keeps only one directory descriptor open at a time.
// The current path is bounded by the operating system's path-length limit; it
// is deliberately not a stack of every ancestor, so a deeply nested tree does
// not exhaust file descriptors. os.Root keeps every re-opened path confined to
// the workspace, and Lstat ensures ordinary symlinks are unlinked as leaves.
func nextTreeCleanupLeaf(root *os.Root, relativePath string) (string, bool, error) {
	path := relativePath
	for {
		info, err := root.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return "", true, nil
			}
			return "", false, err
		}
		if !info.IsDir() {
			return path, false, nil
		}

		dir, err := root.Open(path)
		if err != nil {
			return "", false, err
		}
		entries, readErr := dir.ReadDir(1)
		closeErr := dir.Close()
		if closeErr != nil {
			return "", false, closeErr
		}
		if len(entries) == 1 {
			path = filepath.Join(path, entries[0].Name())
			continue
		}
		if readErr == nil || errors.Is(readErr, io.EOF) {
			return path, false, nil
		}
		return "", false, readErr
	}
}

// removeTreeRootFlatBudget removes up to budget leaves without retaining an
// ancestor stack. It is the resumable fallback beyond the descriptor cap.
func removeTreeRootFlatBudget(root *os.Root, relativePath string, budget int) (complete bool, err error) {
	if budget <= 0 {
		return false, nil
	}
	for removed := 0; removed < budget; {
		leaf, gone, findErr := nextTreeCleanupLeaf(root, relativePath)
		if findErr != nil {
			return false, findErr
		}
		if gone {
			return true, nil
		}
		if removeErr := root.Remove(leaf); removeErr != nil {
			if os.IsNotExist(removeErr) {
				continue
			}
			return false, removeErr
		}
		removed++
	}
	_, statErr := root.Lstat(relativePath)
	if os.IsNotExist(statErr) {
		return true, nil
	}
	if statErr != nil {
		return false, statErr
	}
	return false, nil
}

// removeTreeRootBudget removes up to budget filesystem nodes with an
// iterative post-order traversal. On a path deeper than treeCleanupMaxOpenDirs
// it switches to the descriptor-bounded fallback instead of abandoning the
// renamed trash tree. A false complete result is safe to run again: already
// removed children stay gone, so cleanupTreeRoot eventually completes it.
func removeTreeRootBudget(root *os.Root, relativePath string, budget int) (complete bool, err error) {
	if budget <= 0 {
		return false, nil
	}
	frames := []treeCleanupFrame{{path: relativePath, name: relativePath, parentRoot: root}}
	defer func() { closeTreeCleanupFrames(frames) }()

	for len(frames) > 0 {
		if budget == 0 {
			return false, nil
		}
		last := len(frames) - 1
		frame := &frames[last]
		if frame.dir == nil {
			info, statErr := frame.parentRoot.Lstat(frame.name)
			if statErr != nil {
				if os.IsNotExist(statErr) {
					frames = frames[:last]
					continue
				}
				return false, statErr
			}
			budget--
			if !info.IsDir() {
				if removeErr := frame.parentRoot.Remove(frame.name); removeErr != nil && !os.IsNotExist(removeErr) {
					return false, removeErr
				}
				frames = frames[:last]
				continue
			}
			if len(frames) >= treeCleanupMaxOpenDirs {
				// A complete fallback removes this one deep subtree. We return
				// after it either way so the next bounded pass re-opens the
				// shallower frames from a known state.
				_, fallbackErr := removeTreeRootFlatBudget(root, frame.path, treeCleanupMaxBudget)
				return false, fallbackErr
			}
			frameRoot, openErr := frame.parentRoot.OpenRoot(frame.name)
			if openErr != nil {
				return false, openErr
			}
			dir, openErr := frameRoot.Open(".")
			if openErr != nil {
				_ = frameRoot.Close()
				return false, openErr
			}
			frame.root = frameRoot
			frame.dir = dir
			continue
		}

		entries, readErr := frame.dir.ReadDir(1)
		if len(entries) == 1 {
			budget--
			frames = append(frames, treeCleanupFrame{
				path:       filepath.Join(frame.path, entries[0].Name()),
				name:       entries[0].Name(),
				parentRoot: frame.root,
			})
			continue
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return false, readErr
		}
		if closeErr := frame.dir.Close(); closeErr != nil {
			return false, closeErr
		}
		frame.dir = nil
		if closeErr := frame.root.Close(); closeErr != nil {
			return false, closeErr
		}
		frame.root = nil
		if removeErr := frame.parentRoot.Remove(frame.name); removeErr != nil && !os.IsNotExist(removeErr) {
			return false, removeErr
		}
		frames = frames[:last]
	}
	return true, nil
}

func cleanupTreeRoot(root *os.Root, relativePath string) error {
	budget := treeCleanupNodeBudget
	for {
		complete, err := removeTreeRootBudget(root, relativePath, budget)
		if err != nil || complete {
			return err
		}
		// A deep fallback is restart-safe, and the ordinary traversal grows only
		// to a fixed ceiling so each pass keeps descriptor and work bounds.
		if budget < treeCleanupMaxBudget {
			budget *= 2
			if budget > treeCleanupMaxBudget {
				budget = treeCleanupMaxBudget
			}
		}
	}
}

// moveTreeTargetToTrash commits deletion atomically from Teak's point of
// view: a root-confined rename makes the requested path disappear before any
// recursive cleanup starts. Each operation owns one hidden container, which
// cleanup removes in full so successful deletes leave no persistent trash
// directory in the workspace.
func moveTreeTargetToTrash(root *os.Root, relativePath string) (string, error) {
	for attempt := 0; attempt < treeTrashCreateAttempts; attempt++ {
		container := fmt.Sprintf(".teak-trash-%016x", treeTrashSequence.Add(1))
		if err := root.Mkdir(container, 0o700); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", fmt.Errorf("reserve tree trash: %w", err)
		}
		payload := filepath.Join(container, treeTrashPayloadName)
		if err := root.Rename(relativePath, payload); err != nil {
			_ = root.Remove(container)
			return "", err
		}
		return container, nil
	}
	return "", errors.New("reserve unique tree trash path")
}

// startTreeDelete snapshots dirty editor paths for the command. The command
// determines the target type with a root-confined Lstat, keeping filesystem
// inspection out of Bubble Tea's Update loop.
func (m *Model) startTreeDelete(target string) tea.Cmd {
	if rootPath, rootErr := filepath.Abs(m.rootDir); rootErr == nil {
		if targetPath, targetErr := filepath.Abs(target); targetErr == nil && filepath.Clean(targetPath) == filepath.Clean(rootPath) {
			m.status = "Cannot delete workspace root"
			return nil
		}
	}
	relativePath, absolutePath, err := m.workspaceRelativePath(target)
	if err != nil {
		m.status = fmt.Sprintf("Error deleting: %v", err)
		return nil
	}
	if relativePath == "." {
		m.status = "Cannot delete workspace root"
		return nil
	}
	dirtyPaths := make([]string, 0, len(m.editors))
	for _, ed := range m.editors {
		if ed.Buffer.Dirty() {
			dirtyPaths = append(dirtyPaths, ed.Buffer.FilePath)
		}
	}
	return m.startTreeAction(treeActionRequest{
		Kind:         treeActionDelete,
		Root:         m.agentWriteRoot,
		RootPath:     m.rootDir,
		RelativePath: relativePath,
		Path:         absolutePath,
		DirtyPaths:   dirtyPaths,
	})
}

// startTreeAction is the single handoff from Update to filesystem work.
// Destructive operations are serialized rather than superseded: once a
// command reaches its filesystem commit point, hiding it behind a newer UI
// request would leave the model lying about the workspace.
func (m *Model) startTreeAction(request treeActionRequest) tea.Cmd {
	if m.treeActionInFlight {
		m.status = "Another file operation is already in progress"
		return nil
	}
	m.treeActionGeneration++
	request.Generation = m.treeActionGeneration
	ctx, cancel := context.WithCancel(context.Background())
	m.treeActionCancel = cancel
	m.treeActionInFlight = true
	return treeActionCmd(ctx, request)
}

func (m *Model) cancelTreeAction() {
	if m.treeActionCancel != nil {
		m.treeActionCancel()
		m.treeActionCancel = nil
	}
}

func treeActionCmd(ctx context.Context, request treeActionRequest) tea.Cmd {
	return func() tea.Msg {
		outcome := runTreeAction(ctx, request)
		if !outcome.Committed && outcome.Err == nil && ctx.Err() != nil {
			outcome.Err = ctx.Err()
		}
		path := outcome.Path
		if path == "" {
			path = request.Path
		}
		return treeActionResultMsg{
			Generation:  request.Generation,
			Kind:        request.Kind,
			SourcePath:  request.SourcePath,
			Path:        path,
			Name:        request.Name,
			IsFolder:    request.IsFolder,
			TargetIsDir: outcome.TargetIsDir,
			Committed:   outcome.Committed,
			CleanupErr:  outcome.CleanupErr,
			Err:         outcome.Err,
		}
	}
}

func executeTreeAction(ctx context.Context, request treeActionRequest) treeActionOutcome {
	if err := ctx.Err(); err != nil {
		return treeActionOutcome{Err: err}
	}
	// Every command owns its Root descriptor. A committed delete may continue
	// cleanup after Model.cleanup closes agentWriteRoot, so sharing that handle
	// would turn a post-commit cleanup into a race with shutdown.
	var (
		root *os.Root
		err  error
	)
	if request.Root != nil {
		root, err = request.Root.OpenRoot(".")
	} else {
		root, err = os.OpenRoot(request.RootPath)
	}
	if err != nil {
		return treeActionOutcome{Err: fmt.Errorf("open workspace root: %w", err)}
	}
	defer func() { _ = root.Close() }()
	if err := ctx.Err(); err != nil {
		return treeActionOutcome{Err: err}
	}

	switch request.Kind {
	case treeActionCreate:
		return executeTreeCreate(ctx, root, request)
	case treeActionDelete:
		return executeTreeDelete(ctx, root, request)
	case treeActionRename, treeActionMove:
		return executeTreeMove(ctx, root, request)
	case treeActionCopy:
		return executeTreeCopy(ctx, root, request)
	default:
		return treeActionOutcome{Err: fmt.Errorf("unsupported tree action %d", request.Kind)}
	}
}

func executeTreeMove(ctx context.Context, root *os.Root, request treeActionRequest) treeActionOutcome {
	if request.RelativePath == "." || request.DestinationRelativePath == "." {
		return treeActionOutcome{Err: errors.New("cannot move or rename the workspace root")}
	}
	info, err := root.Lstat(request.RelativePath)
	if err != nil {
		return treeActionOutcome{Err: err}
	}
	if !info.Mode().IsRegular() && !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return treeActionOutcome{Err: errors.New("source is not a regular file, directory, or symlink")}
	}
	if info.IsDir() && isTreeTargetPath(request.SourcePath, request.Path, true) {
		return treeActionOutcome{Err: errors.New("cannot move a directory inside itself")}
	}
	parentInfo, err := root.Stat(request.ParentRelativePath)
	if err != nil {
		return treeActionOutcome{Err: fmt.Errorf("inspect destination directory: %w", err)}
	}
	if !parentInfo.IsDir() {
		return treeActionOutcome{Err: errors.New("destination is not a directory")}
	}
	if _, err := root.Lstat(request.DestinationRelativePath); err == nil {
		return treeActionOutcome{Err: errors.New("destination already exists")}
	} else if !os.IsNotExist(err) {
		return treeActionOutcome{Err: fmt.Errorf("inspect destination: %w", err)}
	}
	if err := ctx.Err(); err != nil {
		return treeActionOutcome{Err: err}
	}
	if err := root.Rename(request.RelativePath, request.DestinationRelativePath); err != nil {
		return treeActionOutcome{Err: err}
	}
	treeActionAfterCommit(request)
	return treeActionOutcome{
		Path:        request.Path,
		TargetIsDir: info.IsDir(),
		Committed:   true,
	}
}

func executeTreeCopy(ctx context.Context, root *os.Root, request treeActionRequest) treeActionOutcome {
	if request.RelativePath == "." || request.DestinationRelativePath == "." {
		return treeActionOutcome{Err: errors.New("cannot copy the workspace root")}
	}
	info, err := root.Lstat(request.RelativePath)
	if err != nil {
		return treeActionOutcome{Err: err}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return treeActionOutcome{Err: errors.New("copying symlinks is disabled; move the link instead")}
	}
	parentInfo, err := root.Stat(request.ParentRelativePath)
	if err != nil {
		return treeActionOutcome{Err: fmt.Errorf("inspect destination directory: %w", err)}
	}
	if !parentInfo.IsDir() {
		return treeActionOutcome{Err: errors.New("destination is not a directory")}
	}
	if _, err := root.Lstat(request.DestinationRelativePath); err == nil {
		return treeActionOutcome{Err: errors.New("destination already exists")}
	} else if !os.IsNotExist(err) {
		return treeActionOutcome{Err: fmt.Errorf("inspect destination: %w", err)}
	}
	if err := ctx.Err(); err != nil {
		return treeActionOutcome{Err: err}
	}
	if err := copyTreeRoot(ctx, root, request.RelativePath, request.DestinationRelativePath, info); err != nil {
		cleanupErr := cleanupTreeRoot(root, request.DestinationRelativePath)
		if cleanupErr != nil {
			err = fmt.Errorf("copy failed: %w (cleanup failed: %v)", err, cleanupErr)
		}
		return treeActionOutcome{Err: err}
	}
	treeActionAfterCommit(request)
	return treeActionOutcome{
		Path:        request.Path,
		TargetIsDir: info.IsDir(),
		Committed:   true,
	}
}

// copyTreeRoot copies one regular file or a directory tree using only the
// pinned root. It rejects symlinks and special files instead of accidentally
// copying a device or following a link with surprising semantics.
func copyTreeRoot(ctx context.Context, root *os.Root, source, destination string, sourceInfo os.FileInfo) error {
	if sourceInfo.IsDir() {
		if err := root.Mkdir(destination, sourceInfo.Mode().Perm()); err != nil {
			return err
		}
		return fs.WalkDir(root.FS(), source, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if path == source {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("copying symlink %q is disabled", path)
			}
			relative, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			target := filepath.Join(destination, relative)
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return root.Mkdir(target, info.Mode().Perm())
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("cannot copy special file %q", path)
			}
			return copyTreeFile(ctx, root, path, target, info.Mode().Perm())
		})
	}
	if !sourceInfo.Mode().IsRegular() {
		return errors.New("cannot copy special file")
	}
	return copyTreeFile(ctx, root, source, destination, sourceInfo.Mode().Perm())
}

func copyTreeFile(ctx context.Context, root *os.Root, source, destination string, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	src, err := root.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := root.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return ctx.Err()
}

func executeTreeCreate(ctx context.Context, root *os.Root, request treeActionRequest) treeActionOutcome {
	info, err := root.Stat(request.ParentRelativePath)
	if err != nil {
		return treeActionOutcome{Err: fmt.Errorf("inspect destination directory: %w", err)}
	}
	if !info.IsDir() {
		return treeActionOutcome{Err: errors.New("destination is not a directory")}
	}
	if err := ctx.Err(); err != nil {
		return treeActionOutcome{Err: err}
	}
	if request.IsFolder {
		err = root.Mkdir(request.RelativePath, 0o755)
		if err == nil {
			treeActionAfterCommit(request)
			return treeActionOutcome{Path: request.Path, Committed: true}
		}
	} else {
		var file *os.File
		file, err = root.OpenFile(request.RelativePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			closeErr := file.Close()
			treeActionAfterCommit(request)
			return treeActionOutcome{Path: request.Path, Committed: true, Err: closeErr}
		}
	}
	if err != nil {
		return treeActionOutcome{Err: err}
	}
	return treeActionOutcome{Err: errors.New("tree create did not commit")}
}

func executeTreeDelete(ctx context.Context, root *os.Root, request treeActionRequest) treeActionOutcome {
	if request.RelativePath == "." {
		return treeActionOutcome{Err: errors.New("cannot delete workspace root")}
	}
	info, err := root.Lstat(request.RelativePath)
	if err != nil {
		return treeActionOutcome{Err: err}
	}
	if err := ctx.Err(); err != nil {
		return treeActionOutcome{Err: err}
	}
	for _, dirtyPath := range request.DirtyPaths {
		if isTreeTargetPath(request.Path, dirtyPath, info.IsDir()) {
			return treeActionOutcome{Err: errTreeTargetHasUnsavedChanges}
		}
	}
	if err := ctx.Err(); err != nil {
		return treeActionOutcome{Err: err}
	}
	trashPath, err := moveTreeTargetToTrash(root, request.RelativePath)
	if err != nil {
		return treeActionOutcome{Err: err}
	}
	// The rename is the logical commit. From this point cancellation cannot
	// truthfully turn the result into "nothing happened", so cleanup ignores
	// ctx and reports only an honest post-commit warning.
	treeActionAfterCommit(request)
	cleanupErr := cleanupTreeRoot(root, trashPath)
	return treeActionOutcome{
		Path:        request.Path,
		TargetIsDir: info.IsDir(),
		Committed:   true,
		CleanupErr:  cleanupErr,
	}
}

func (m Model) handleTreeActionResult(msg treeActionResultMsg) (tea.Model, tea.Cmd) {
	if !m.treeActionInFlight || msg.Generation != m.treeActionGeneration {
		return m, nil
	}
	m.cancelTreeAction()
	m.treeActionInFlight = false
	if msg.Err != nil && !msg.Committed {
		if errors.Is(msg.Err, context.Canceled) {
			return m, nil
		}
		switch {
		case msg.Kind == treeActionDelete && errors.Is(msg.Err, errTreeTargetHasUnsavedChanges):
			m.status = "Cannot delete: an affected file has unsaved changes"
		case msg.Kind == treeActionCreate && msg.IsFolder:
			m.status = fmt.Sprintf("Error creating folder: %v", msg.Err)
		case msg.Kind == treeActionCreate:
			m.status = fmt.Sprintf("Error creating file: %v", msg.Err)
		case msg.Kind == treeActionDelete:
			m.status = fmt.Sprintf("Error deleting: %v", msg.Err)
		case msg.Kind == treeActionRename:
			m.status = fmt.Sprintf("Error renaming: %v", msg.Err)
		case msg.Kind == treeActionMove:
			m.status = fmt.Sprintf("Error moving: %v", msg.Err)
		case msg.Kind == treeActionCopy:
			m.status = fmt.Sprintf("Error copying: %v", msg.Err)
		}
		return m, nil
	}

	switch msg.Kind {
	case treeActionCreate:
		warning := msg.Err
		if msg.IsFolder {
			m.status = treeCreateStatus(msg.Name, true, warning)
			refreshCmd := m.queueTreeRefresh()
			if m.watcher != nil {
				m.watcher.WatchDir(msg.Path)
			}
			return m, refreshCmd
		}
		search.InvalidateSemanticIndex(m.rootDir)
		m.status = treeCreateStatus(msg.Name, false, warning)
		refreshCmd := m.queueTreeRefresh()
		openedModel, openCmd := m.openFilePinned(msg.Path)
		opened := openedModel.(Model)
		return opened, tea.Batch(
			opened.triggerPluginEvents(opened.pluginEvent(plugin.EventBufNew, msg.Path)),
			refreshCmd,
			openCmd,
		)

	case treeActionDelete:
		var cmds []tea.Cmd
		closedAny := false
		preservedDirty := 0
		for i := len(m.editors) - 1; i >= 0; i-- {
			if !isTreeTargetPath(msg.Path, m.editors[i].Buffer.FilePath, msg.TargetIsDir) {
				continue
			}
			if m.editors[i].Buffer.Dirty() {
				preservedDirty++
				continue
			}
			var cmd tea.Cmd
			updated, cmd := m.closeTab(i)
			m = updated.(Model)
			cmds = append(cmds, cmd)
			closedAny = true
		}
		search.InvalidateSemanticIndex(m.rootDir)
		m.status = treeDeleteStatus(msg.Path, msg.CleanupErr, preservedDirty)
		cmds = append(cmds, m.queueTreeRefresh())
		if !closedAny && preservedDirty == 0 {
			cmds = append(cmds, m.triggerPluginEvents(m.pluginEvent(plugin.EventBufDelete, msg.Path)))
		}
		return m, tea.Batch(cmds...)

	case treeActionRename, treeActionMove, treeActionCopy:
		search.InvalidateSemanticIndex(m.rootDir)
		var label string
		switch msg.Kind {
		case treeActionRename:
			label = "Renamed"
		case treeActionMove:
			label = "Moved"
		default:
			label = "Copied"
		}
		m.status = fmt.Sprintf("%s: %s", label, filepath.Base(msg.Path))
		var cmds []tea.Cmd
		if msg.Kind != treeActionCopy {
			cmds = append(cmds, m.reconcileTreeTransfer(msg.SourcePath, msg.Path, msg.TargetIsDir))
		}
		cmds = append(cmds, m.queueTreeRefresh())
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

func treeCreateStatus(name string, folder bool, warning error) string {
	prefix := "Created"
	if folder {
		prefix = "Created folder"
	}
	status := fmt.Sprintf("%s: %s", prefix, name)
	if warning != nil {
		return fmt.Sprintf("%s (post-commit warning: %v)", status, warning)
	}
	return status
}

func treeDeleteStatus(path string, cleanupErr error, preservedDirty int) string {
	status := fmt.Sprintf("Deleted: %s", filepath.Base(path))
	if cleanupErr != nil {
		status = fmt.Sprintf("%s (cleanup warning: %v)", status, cleanupErr)
	}
	if preservedDirty > 0 {
		suffix := "buffers"
		if preservedDirty == 1 {
			suffix = "buffer"
		}
		status = fmt.Sprintf("%s; kept %d modified %s open", status, preservedDirty, suffix)
	}
	return status
}

// deleteLastRune is used by text-entry overlays. Slicing by byte corrupts
// UTF-8 after a single Backspace; invalid input is still reduced safely.
func deleteLastRune(value string) string {
	if value == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(value)
	if size <= 0 {
		return ""
	}
	return value[:len(value)-size]
}
