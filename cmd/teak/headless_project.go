package main

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	headlessProjectMaxNodes = 10_000
	headlessProjectMaxBytes = 128 << 20
	headlessProjectTimeout  = 30 * time.Second
)

type headlessProjectResponse struct {
	Workspace   string  `json:"workspace"`
	Operation   string  `json:"operation"`
	State       string  `json:"state"`
	Committed   bool    `json:"committed"`
	Path        string  `json:"path,omitempty"`
	Source      string  `json:"source,omitempty"`
	Destination string  `json:"destination,omitempty"`
	Nodes       int     `json:"nodes,omitempty"`
	Bytes       int64   `json:"bytes,omitempty"`
	DurationMS  float64 `json:"duration_ms"`
	Detail      string  `json:"detail,omitempty"`
}

type headlessProjectStatResponse struct {
	Workspace    string `json:"workspace"`
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	Kind         string `json:"kind"`
	Bytes        int64  `json:"bytes,omitempty"`
	Mode         string `json:"mode"`
}

type headlessProjectDirectory struct {
	path     string
	depth    int
	relative string
}

type headlessProjectScan struct {
	Paths int
	Bytes int64
	Items []string
}

// runHeadlessProjectContext is the context-aware entry point used by the CLI
// and REST adapter. Confirmed project mutations stop
// scanning/copying/removing when the caller disconnects.
func runHeadlessProjectContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) > 1 && headlessJSONRequested(args[1:]) {
		stderr = headlessErrorWriter{Writer: stderr, json: true}
	}
	if err := ctx.Err(); err != nil {
		return writeHeadlessError(stderr, err)
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	operation := args[0]
	switch operation {
	case "list", "stat", "mkdir", "rename", "copy", "remove":
	default:
		return writeHeadlessError(stderr, fmt.Errorf("unknown project operation %q", operation))
	}
	opts, positional, help, err := parseHeadlessArgs(args[1:])
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	if help {
		_, _ = io.WriteString(stdout, headlessUsageText)
		return 0
	}
	if operation == "list" {
		if len(positional) > 1 {
			return writeHeadlessError(stderr, errors.New("project list accepts at most one path"))
		}
	} else if operation == "stat" || operation == "mkdir" || operation == "remove" {
		if len(positional) != 1 {
			return writeHeadlessError(stderr, fmt.Errorf("project %s requires exactly one path", operation))
		}
	} else if len(positional) != 2 {
		return writeHeadlessError(stderr, fmt.Errorf("project %s requires a source and destination", operation))
	}
	if operation == "mkdir" || operation == "rename" || operation == "copy" || operation == "remove" {
		if !opts.confirm {
			return writeHeadlessError(stderr, fmt.Errorf("project %s requires --confirm", operation))
		}
	}
	rootPath, err := doctorWorkspace(opts.root)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return writeHeadlessError(stderr, fmt.Errorf("open workspace root: %w", err))
	}
	defer func() { _ = root.Close() }()

	switch operation {
	case "list":
		base := "."
		if len(positional) == 1 {
			base = positional[0]
		}
		relative, err := headlessProjectRelativePath(base, true)
		if err != nil {
			return writeHeadlessError(stderr, err)
		}
		depth := 0
		if opts.depthSet {
			depth = opts.depth
		}
		response, err := collectHeadlessProjectContextContext(ctx, root, rootPath, relative, depth)
		if err != nil {
			return writeHeadlessError(stderr, err)
		}
		if opts.json {
			return writeHeadlessJSON(stdout, response)
		}
		var body strings.Builder
		fmt.Fprintf(&body, "Workspace: %s\nEntries: %d%s\n", response.Workspace, len(response.Entries), truncatedSuffix(response.Truncated))
		for _, entry := range response.Entries {
			fmt.Fprintf(&body, "%-9s %s\n", entry.Kind, entry.Path)
		}
		return writeHeadlessText(stdout, stderr, body.String())
	case "stat":
		relative, err := headlessProjectRelativePath(positional[0], true)
		if err != nil {
			return writeHeadlessError(stderr, err)
		}
		response, err := collectHeadlessProjectStat(root, rootPath, relative)
		if err != nil {
			return writeHeadlessError(stderr, err)
		}
		if err := ctx.Err(); err != nil {
			return writeHeadlessError(stderr, err)
		}
		if opts.json {
			return writeHeadlessJSON(stdout, response)
		}
		return writeHeadlessText(stdout, stderr, fmt.Sprintf("Path: %s\nKind: %s\nMode: %s\nBytes: %d\n", response.RelativePath, response.Kind, response.Mode, response.Bytes))
	default:
		return runHeadlessProjectMutation(ctx, operation, root, rootPath, positional, opts, stdout, stderr)
	}
}

func runHeadlessProjectMutation(ctx context.Context, operation string, root *os.Root, rootPath string, positional []string, opts headlessOptions, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return writeHeadlessError(stderr, err)
	}
	source, err := headlessProjectRelativePath(positional[0], false)
	if err != nil {
		return writeHeadlessError(stderr, err)
	}
	destination := ""
	if len(positional) == 2 {
		destination, err = headlessProjectRelativePath(positional[1], false)
		if err != nil {
			return writeHeadlessError(stderr, err)
		}
	}
	if operation == "rename" || operation == "copy" {
		if source == destination {
			return writeHeadlessError(stderr, errors.New("source and destination are the same"))
		}
	}
	if (operation == "rename" || operation == "copy") && headlessProjectPathWithin(source, destination) {
		return writeHeadlessError(stderr, fmt.Errorf("cannot %s a directory inside itself", operation))
	}
	operationCtx, cancel := context.WithTimeout(ctx, headlessProjectTimeout)
	defer cancel()
	if err := operationCtx.Err(); err != nil {
		return writeHeadlessError(stderr, err)
	}
	started := time.Now()
	var nodes int
	var bytes int64
	switch operation {
	case "mkdir":
		if err := headlessProjectMkdir(root, source); err != nil {
			return writeHeadlessError(stderr, err)
		}
		nodes = 1
	case "rename":
		scan, err := scanHeadlessProject(operationCtx, root, source, false)
		if err != nil {
			return writeHeadlessError(stderr, err)
		}
		if err := operationCtx.Err(); err != nil {
			return writeHeadlessError(stderr, err)
		}
		if _, err := headlessProjectRename(root, source, destination); err != nil {
			return writeHeadlessError(stderr, err)
		}
		nodes, bytes = scan.Paths, scan.Bytes
	case "copy":
		_, err := scanHeadlessProject(operationCtx, root, source, true)
		if err != nil {
			return writeHeadlessError(stderr, err)
		}
		copied, err := copyHeadlessProject(operationCtx, root, source, destination)
		if err != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), headlessProjectTimeout)
			cleanupErr := removeHeadlessProjectTree(cleanupCtx, root, destination, headlessProjectMaxNodes)
			cleanupCancel()
			if cleanupErr != nil {
				return writeHeadlessError(stderr, fmt.Errorf("copy failed: %w (cleanup failed: %v)", err, cleanupErr))
			}
			return writeHeadlessError(stderr, fmt.Errorf("copy failed: %w", err))
		}
		// The source is allowed to change between the preflight scan and the
		// actual copy. Report what was copied, not a stale preflight estimate.
		nodes, bytes = copied.Paths, copied.Bytes
	case "remove":
		scan, err := scanHeadlessProject(operationCtx, root, source, false)
		if err != nil {
			return writeHeadlessError(stderr, err)
		}
		if err := removeHeadlessProjectTree(operationCtx, root, source, headlessProjectMaxNodes); err != nil {
			return writeHeadlessError(stderr, fmt.Errorf("remove failed: %w", err))
		}
		nodes, bytes = scan.Paths, scan.Bytes
	default:
		return writeHeadlessError(stderr, fmt.Errorf("unsupported project mutation %q", operation))
	}
	response := headlessProjectResponse{
		Workspace:   rootPath,
		Operation:   operation,
		State:       "committed",
		Committed:   true,
		Path:        headlessProjectAbsolutePath(rootPath, source),
		Source:      source,
		Destination: destination,
		Nodes:       nodes,
		Bytes:       bytes,
		DurationMS:  float64(time.Since(started)) / float64(time.Millisecond),
	}
	if opts.json {
		return writeHeadlessJSON(stdout, response)
	}
	return writeHeadlessText(stdout, stderr, fmt.Sprintf("Workspace: %s\nOperation: %s\nState: %s\nNodes: %d\nBytes: %d\n", response.Workspace, response.Operation, response.State, response.Nodes, response.Bytes))
}

func headlessProjectRelativePath(raw string, allowRoot bool) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("project path is empty")
	}
	if strings.IndexByte(raw, 0) >= 0 {
		return "", errors.New("project path contains a NUL byte")
	}
	if filepath.IsAbs(raw) {
		return "", fmt.Errorf("project path %q must be relative to the workspace", raw)
	}
	clean := filepath.Clean(raw)
	if clean == "." {
		if allowRoot {
			return ".", nil
		}
		return "", errors.New("project mutation cannot target the workspace root")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("project path %q is outside the workspace", raw)
	}
	return filepath.ToSlash(clean), nil
}

func headlessProjectAbsolutePath(root, relative string) string {
	if relative == "." {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(relative))
}

func headlessProjectPathWithin(source, destination string) bool {
	if source == "." || destination == "." {
		return false
	}
	relative, err := filepath.Rel(filepath.FromSlash(source), filepath.FromSlash(destination))
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func collectHeadlessProjectContextContext(ctx context.Context, root *os.Root, rootPath, base string, maxDepth int) (headlessContextResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxDepth < 0 || maxDepth > headlessMaxContextDepth {
		return headlessContextResponse{}, fmt.Errorf("project list depth must be between 0 and %d", headlessMaxContextDepth)
	}
	info, err := root.Lstat(base)
	if err != nil {
		return headlessContextResponse{}, fmt.Errorf("stat project directory %q: %w", base, err)
	}
	if !info.IsDir() {
		return headlessContextResponse{}, fmt.Errorf("project list target %q is not a directory", base)
	}
	entries := make([]headlessContextEntry, 0, 64)
	queue := []headlessProjectDirectory{{path: base, relative: base, depth: 0}}
	truncated := false
	visitedDirs := 0
	for len(queue) > 0 && len(entries) < headlessMaxContextEntries {
		if err := ctx.Err(); err != nil {
			return headlessContextResponse{}, err
		}
		directory := queue[0]
		queue = queue[1:]
		visitedDirs++
		remaining := headlessMaxContextEntries - len(entries)
		children, childTruncated, err := readHeadlessProjectDir(root, directory.path, remaining)
		if err != nil {
			return headlessContextResponse{}, fmt.Errorf("read project directory %q: %w", directory.relative, err)
		}
		if err := ctx.Err(); err != nil {
			return headlessContextResponse{}, err
		}
		truncated = truncated || childTruncated
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, entry := range children {
			if err := ctx.Err(); err != nil {
				return headlessContextResponse{}, err
			}
			if len(entries) >= headlessMaxContextEntries {
				truncated = true
				break
			}
			relative := path.Join(directory.relative, entry.Name())
			if directory.relative == "." {
				relative = entry.Name()
			}
			kind := "other"
			bytes := int64(0)
			isSymlink := entry.Type()&os.ModeSymlink != 0
			if isSymlink {
				kind = "symlink"
			} else if entry.IsDir() {
				kind = "directory"
			} else if childInfo, infoErr := entry.Info(); infoErr == nil && childInfo.Mode().IsRegular() {
				kind = "file"
				bytes = childInfo.Size()
			}
			entries = append(entries, headlessContextEntry{Name: entry.Name(), Path: relative, Kind: kind, Bytes: bytes})
			if maxDepth > directory.depth && !isSymlink && entry.IsDir() {
				if visitedDirs+len(queue) >= headlessMaxContextDirs {
					truncated = true
					continue
				}
				queue = append(queue, headlessProjectDirectory{path: path.Join(directory.path, entry.Name()), relative: relative, depth: directory.depth + 1})
			}
		}
	}
	if len(queue) > 0 {
		truncated = true
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return headlessContextResponse{
		Workspace:   rootPath,
		ProjectRoot: findProjectRoot(rootPath),
		Entries:     entries,
		Truncated:   truncated,
	}, nil
}

func readHeadlessProjectDir(root *os.Root, directory string, limit int) ([]os.DirEntry, bool, error) {
	if limit <= 0 {
		return nil, false, nil
	}
	dir, err := root.Open(directory)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = dir.Close() }()
	entries := make(headlessDirEntryHeap, 0, limit)
	heap.Init(&entries)
	truncated := false
	for {
		batch, readErr := dir.ReadDir(256)
		for _, entry := range batch {
			if entries.Len() < limit {
				heap.Push(&entries, entry)
				continue
			}
			truncated = true
			if entry.Name() < entries[0].Name() {
				entries[0] = entry
				heap.Fix(&entries, 0)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, false, readErr
		}
		if len(batch) == 0 {
			break
		}
	}
	return []os.DirEntry(entries), truncated, nil
}

func collectHeadlessProjectStat(root *os.Root, rootPath, relative string) (headlessProjectStatResponse, error) {
	info, err := root.Lstat(relative)
	if err != nil {
		return headlessProjectStatResponse{}, fmt.Errorf("stat project path %q: %w", relative, err)
	}
	kind := headlessProjectKind(info)
	bytes := int64(0)
	if info.Mode().IsRegular() {
		bytes = info.Size()
	}
	return headlessProjectStatResponse{
		Workspace:    rootPath,
		Path:         headlessProjectAbsolutePath(rootPath, relative),
		RelativePath: relative,
		Kind:         kind,
		Bytes:        bytes,
		Mode:         info.Mode().String(),
	}, nil
}

func headlessProjectKind(info os.FileInfo) string {
	if info.Mode()&os.ModeSymlink != 0 {
		return "symlink"
	}
	if info.IsDir() {
		return "directory"
	}
	if info.Mode().IsRegular() {
		return "file"
	}
	return "other"
}

func headlessProjectMkdir(root *os.Root, relative string) error {
	if err := headlessProjectParentDirectory(root, relative); err != nil {
		return err
	}
	if _, err := root.Lstat(relative); err == nil {
		return fmt.Errorf("project path %q already exists", relative)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect project path %q: %w", relative, err)
	}
	if err := root.Mkdir(relative, 0o755); err != nil {
		return fmt.Errorf("create project directory %q: %w", relative, err)
	}
	return nil
}

func headlessProjectRename(root *os.Root, source, destination string) (os.FileInfo, error) {
	if err := rejectHeadlessProjectSymlinkParents(root, source, true); err != nil {
		return nil, err
	}
	info, err := root.Lstat(source)
	if err != nil {
		return nil, fmt.Errorf("inspect project source %q: %w", source, err)
	}
	if info.Mode()&os.ModeSymlink == 0 && !info.Mode().IsRegular() && !info.IsDir() {
		return nil, errors.New("project source is not a regular file, directory, or symlink")
	}
	if err := headlessProjectParentDirectory(root, destination); err != nil {
		return nil, err
	}
	if _, err := root.Lstat(destination); err == nil {
		return nil, fmt.Errorf("project destination %q already exists", destination)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect project destination %q: %w", destination, err)
	}
	if err := root.Rename(source, destination); err != nil {
		return nil, fmt.Errorf("rename project path %q to %q: %w", source, destination, err)
	}
	return info, nil
}

func headlessProjectParentDirectory(root *os.Root, relative string) error {
	parent := path.Dir(relative)
	if parent == "" {
		parent = "."
	}
	if parent == "." {
		return nil
	}
	current := "."
	for _, component := range strings.Split(parent, "/") {
		current = path.Join(current, component)
		info, err := root.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect project parent %q: %w", parent, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("project parent %q contains a symlink", parent)
		}
		if !info.IsDir() {
			return fmt.Errorf("project parent %q is not a directory", parent)
		}
	}
	return nil
}

// rejectHeadlessProjectSymlinkParents keeps a root-confined operation from
// using an in-workspace symlink as an alias for a different subtree. os.Root
// prevents escaping the workspace, but an alias can still make a copy target
// become a descendant of its source and turn a bounded walk into self-copying.
func rejectHeadlessProjectSymlinkParents(root *os.Root, relative string, allowFinalSymlink bool) error {
	if relative == "." {
		return nil
	}
	components := strings.Split(relative, "/")
	current := "."
	for i, component := range components {
		current = path.Join(current, component)
		info, err := root.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) && i == len(components)-1 {
				return nil
			}
			return fmt.Errorf("inspect project path %q: %w", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if allowFinalSymlink && i == len(components)-1 {
				return nil
			}
			return fmt.Errorf("project path %q contains a symlink", relative)
		}
		if i < len(components)-1 && !info.IsDir() {
			return fmt.Errorf("project path %q has a non-directory parent", relative)
		}
	}
	return nil
}

func scanHeadlessProject(ctx context.Context, root *os.Root, source string, rejectSymlinks bool) (headlessProjectScan, error) {
	if err := rejectHeadlessProjectSymlinkParents(root, source, !rejectSymlinks); err != nil {
		return headlessProjectScan{}, err
	}
	info, err := root.Lstat(source)
	if err != nil {
		return headlessProjectScan{}, fmt.Errorf("inspect project source %q: %w", source, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if rejectSymlinks {
			return headlessProjectScan{}, fmt.Errorf("copying symlink %q is disabled", source)
		}
		return headlessProjectScan{Paths: 1, Items: []string{source}}, nil
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return headlessProjectScan{}, fmt.Errorf("project source %q is not a regular file, directory, or symlink", source)
	}
	scan := headlessProjectScan{Items: make([]string, 0, 64)}
	if err := fs.WalkDir(root.FS(), source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if rejectSymlinks {
				return fmt.Errorf("copying symlink %q is disabled", current)
			}
			scan.Paths++
			scan.Items = append(scan.Items, current)
			if scan.Paths > headlessProjectMaxNodes {
				return fmt.Errorf("project operation exceeds %d-node limit", headlessProjectMaxNodes)
			}
			return nil
		}
		if !entry.IsDir() {
			fileInfo, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if !fileInfo.Mode().IsRegular() {
				return fmt.Errorf("project path %q is a special file", current)
			}
			scan.Bytes += fileInfo.Size()
			if rejectSymlinks && scan.Bytes > headlessProjectMaxBytes {
				return fmt.Errorf("project operation exceeds %d-byte limit", headlessProjectMaxBytes)
			}
		}
		scan.Paths++
		scan.Items = append(scan.Items, current)
		if scan.Paths > headlessProjectMaxNodes {
			return fmt.Errorf("project operation exceeds %d-node limit", headlessProjectMaxNodes)
		}
		return nil
	}); err != nil {
		return headlessProjectScan{}, fmt.Errorf("scan project %q: %w", source, err)
	}
	return scan, nil
}

type headlessProjectCopyBudget struct {
	maxNodes int
	maxBytes int64
	nodes    int
	bytes    int64
}

func (b *headlessProjectCopyBudget) addNode() error {
	if b == nil {
		return errors.New("project copy budget is nil")
	}
	if b.nodes >= b.maxNodes {
		return fmt.Errorf("project operation exceeds %d-node limit", b.maxNodes)
	}
	b.nodes++
	return nil
}

func (b *headlessProjectCopyBudget) canAddBytes(size int64) error {
	if b == nil {
		return errors.New("project copy budget is nil")
	}
	if size < 0 || size > b.maxBytes-b.bytes {
		return fmt.Errorf("project operation exceeds %d-byte limit", b.maxBytes)
	}
	return nil
}

func (b *headlessProjectCopyBudget) addBytes(size int64) error {
	if err := b.canAddBytes(size); err != nil {
		return err
	}
	b.bytes += size
	return nil
}

func copyHeadlessProject(ctx context.Context, root *os.Root, source, destination string) (headlessProjectScan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	budget := headlessProjectCopyBudget{
		maxNodes: headlessProjectMaxNodes,
		maxBytes: headlessProjectMaxBytes,
	}
	if err := headlessProjectParentDirectory(root, destination); err != nil {
		return headlessProjectScan{}, err
	}
	if _, err := root.Lstat(destination); err == nil {
		return headlessProjectScan{}, fmt.Errorf("project destination %q already exists", destination)
	} else if !os.IsNotExist(err) {
		return headlessProjectScan{}, fmt.Errorf("inspect project destination %q: %w", destination, err)
	}
	info, err := root.Lstat(source)
	if err != nil {
		return headlessProjectScan{}, err
	}
	if err := budget.addNode(); err != nil {
		return headlessProjectScan{}, err
	}
	if info.IsDir() {
		if err := root.Mkdir(destination, info.Mode().Perm()); err != nil {
			return headlessProjectScan{}, err
		}
		err := fs.WalkDir(root.FS(), source, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if current == source {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("copying symlink %q is disabled", current)
			}
			relative, err := filepath.Rel(filepath.FromSlash(source), filepath.FromSlash(current))
			if err != nil {
				return err
			}
			target := path.Join(destination, relative)
			childInfo, err := entry.Info()
			if err != nil {
				return err
			}
			if err := budget.addNode(); err != nil {
				return err
			}
			if entry.IsDir() {
				return root.Mkdir(target, childInfo.Mode().Perm())
			}
			if !childInfo.Mode().IsRegular() {
				return fmt.Errorf("cannot copy special file %q", current)
			}
			_, err = copyHeadlessProjectFile(ctx, root, current, target, childInfo.Mode().Perm(), &budget)
			return err
		})
		if err != nil {
			return headlessProjectScan{}, err
		}
		if err := ctx.Err(); err != nil {
			return headlessProjectScan{}, err
		}
		return headlessProjectScan{Paths: budget.nodes, Bytes: budget.bytes}, nil
	}
	if !info.Mode().IsRegular() {
		return headlessProjectScan{}, fmt.Errorf("cannot copy project path %q", source)
	}
	_, err = copyHeadlessProjectFile(ctx, root, source, destination, info.Mode().Perm(), &budget)
	if err != nil {
		return headlessProjectScan{}, err
	}
	return headlessProjectScan{Paths: budget.nodes, Bytes: budget.bytes}, nil
}

func copyHeadlessProjectFile(ctx context.Context, root *os.Root, source, destination string, mode os.FileMode, budget *headlessProjectCopyBudget) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	src, err := root.Open(source)
	if err != nil {
		return 0, err
	}
	defer func() { _ = src.Close() }()
	dst, err := root.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return 0, err
	}
	buf := make([]byte, 32*1024)
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			_ = dst.Close()
			return copied, err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if err := budget.canAddBytes(int64(n)); err != nil {
				_ = dst.Close()
				return copied, err
			}
			written, writeErr := dst.Write(buf[:n])
			if writeErr != nil {
				_ = dst.Close()
				return copied, writeErr
			}
			if written != n {
				_ = dst.Close()
				return copied, io.ErrShortWrite
			}
			if err := budget.addBytes(int64(written)); err != nil {
				_ = dst.Close()
				return copied, err
			}
			copied += int64(written)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			_ = dst.Close()
			return copied, readErr
		}
	}
	if err := ctx.Err(); err != nil {
		_ = dst.Close()
		return copied, err
	}
	if err := dst.Close(); err != nil {
		return copied, err
	}
	return copied, nil
}

func removeHeadlessProjectTree(ctx context.Context, root *os.Root, source string, maxNodes int) error {
	scan, err := scanHeadlessProject(ctx, root, source, false)
	if err != nil {
		return err
	}
	if scan.Paths > maxNodes {
		return fmt.Errorf("project removal exceeds %d-node limit", maxNodes)
	}
	sort.SliceStable(scan.Items, func(i, j int) bool {
		depthI := strings.Count(scan.Items[i], "/")
		depthJ := strings.Count(scan.Items[j], "/")
		if depthI != depthJ {
			return depthI > depthJ
		}
		return scan.Items[i] > scan.Items[j]
	})
	for _, item := range scan.Items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := root.Remove(item); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
