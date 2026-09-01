package app

import (
	"container/heap"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"teak/internal/filetree"
	"teak/internal/overlay"
)

// maxQuickOpenFiles bounds the amount of memory and picker work a single scan
// can create. A project that exceeds this can still be opened through the
// tree; Quick Open deliberately remains responsive.
const maxQuickOpenFiles = 20_000

const (
	quickOpenBatchSize      = 256
	maxQuickOpenDirectories = 2_048
	maxQuickOpenEntries     = 100_000
)

var errQuickOpenLimit = errors.New("quick open file limit reached")

type directoryBatchReader func(context.Context, string, int, func([]os.DirEntry) bool) error

// maxStringHeap retains the lexicographically smallest paths without keeping
// every candidate from a large project in memory.
type maxStringHeap []string

func (h maxStringHeap) Len() int           { return len(h) }
func (h maxStringHeap) Less(i, j int) bool { return h[i] > h[j] }
func (h maxStringHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *maxStringHeap) Push(value any)    { *h = append(*h, value.(string)) }
func (h *maxStringHeap) Pop() any {
	old := *h
	n := len(old)
	value := old[n-1]
	*h = old[:n-1]
	return value
}

// quickOpenReadDirBatches is a narrow seam for exercising cancellation and
// scan limits without constructing huge on-disk directories.
var quickOpenReadDirBatches directoryBatchReader = readDirBatches

// readDirBatches visits a directory incrementally. Unlike os.ReadDir and
// filepath.WalkDir it never builds a slice containing every entry in a large
// workspace directory before the caller can enforce its budget or cancel.
func readDirBatches(ctx context.Context, path string, batchSize int, visit func([]os.DirEntry) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, readErr := dir.ReadDir(batchSize)
		if len(entries) > 0 && !visit(entries) {
			return nil
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

// walkProjectFilesContext walks a project without making the event loop wait
// for it. It observes cancellation on every visited path and stops once the
// bounded result set is full.
func walkProjectFilesContext(ctx context.Context, rootDir string, limit int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ignorePatterns, err := filetree.LoadGitignorePatternsContext(ctx, rootDir)
	if err != nil {
		return nil, err
	}
	var candidates maxStringHeap
	heap.Init(&candidates)
	fileLimitReached := false
	stack := []string{rootDir}
	directories, entriesSeen := 0, 0
	var scanErr error

	for len(stack) > 0 && scanErr == nil {
		if err := ctx.Err(); err != nil {
			scanErr = err
			break
		}
		if directories >= maxQuickOpenDirectories {
			scanErr = errQuickOpenLimit
			break
		}
		dir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		directories++

		readErr := quickOpenReadDirBatches(ctx, dir, quickOpenBatchSize, func(batch []os.DirEntry) bool {
			for _, entry := range batch {
				if err := ctx.Err(); err != nil {
					scanErr = err
					return false
				}
				if entriesSeen >= maxQuickOpenEntries {
					scanErr = errQuickOpenLimit
					return false
				}
				entriesSeen++
				name := entry.Name()
				path := filepath.Join(dir, name)
				rel, relErr := filepath.Rel(rootDir, path)
				if relErr != nil {
					continue
				}

				if entry.IsDir() {
					if !shouldSkipDir(name) && !filetree.MatchesGitignore(rel, ignorePatterns, true) {
						stack = append(stack, path)
					}
					continue
				}
				if strings.HasPrefix(name, ".") || filetree.MatchesGitignore(rel, ignorePatterns, false) {
					continue
				}
				if limit <= 0 {
					heap.Push(&candidates, rel)
					continue
				}
				if candidates.Len() < limit {
					heap.Push(&candidates, rel)
					continue
				}
				fileLimitReached = true
				if rel < candidates[0] {
					candidates[0] = rel
					heap.Fix(&candidates, 0)
				}
			}
			return true
		})
		if readErr != nil && scanErr == nil {
			// Unreadable directories are normal in heterogeneous workspaces; keep
			// scanning siblings, but preserve cancellation as a hard stop.
			if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
				scanErr = readErr
			}
		}
	}

	files := append([]string(nil), candidates...)
	sort.Strings(files)
	if scanErr != nil {
		return files, scanErr
	}
	if fileLimitReached {
		return files, errQuickOpenLimit
	}
	return files, nil
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".svn", ".hg", ".DS_Store",
		"node_modules", "vendor", "__pycache__",
		".next", ".nuxt", "dist", "build",
		".idea", ".vscode", ".cache", "coverage":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// quickOpenCmd walks the project directory in the background. A generation
// lets the model discard stale scans while the context releases filesystem
// work as soon as the picker closes or a scan is superseded.
func quickOpenCmd(ctx context.Context, rootDir string, generation int) tea.Cmd {
	return func() tea.Msg {
		files, err := walkProjectFilesContext(ctx, rootDir, maxQuickOpenFiles)
		return FileListMsg{Files: files, Generation: generation, Err: err}
	}
}

// filesToPickerItems converts file paths to picker items.
func filesToPickerItems(files []string) []overlay.PickerItem {
	return filesToPickerItemsWithRecent(files, nil)
}

func filesToPickerItemsWithRecent(files []string, recent []string) []overlay.PickerItem {
	rank := make(map[string]int, len(recent))
	for i, path := range recent {
		rank[filepath.ToSlash(path)] = len(recent) - i
		rank[path] = len(recent) - i
	}
	items := make([]overlay.PickerItem, len(files))
	for i, f := range files {
		slash := filepath.ToSlash(f)
		items[i] = overlay.PickerItem{
			Label:       filepath.Base(f),
			Description: filepath.Dir(f),
			Value:       f,
			Search:      slash,
			Recency:     rank[slash] + rank[f],
		}
	}
	return items
}

func mergeRecentIntoQuickOpen(files, recent []string, limit int) []string {
	if len(recent) == 0 {
		return files
	}
	seen := make(map[string]struct{}, len(files)+len(recent))
	out := make([]string, 0, len(files)+len(recent))
	for _, path := range recent {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	for _, path := range files {
		if _, ok := seen[path]; ok {
			continue
		}
		if limit > 0 && len(out) >= limit {
			break
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

// preparePickerItemsCmd moves the O(N) conversion from a FileListMsg handler
// into a Bubble Tea command. The picker then performs its fuzzy projection in
// a second cancellable command, so a large quick-open result never makes the
// root Update loop build or sort thousands of items.
func preparePickerItemsCmd(instanceID uint64, zoneID string, files []string, agent bool) tea.Cmd {
	return preparePickerItemsCmdRecent(instanceID, zoneID, files, nil, agent)
}

func preparePickerItemsCmdRecent(instanceID uint64, zoneID string, files, recent []string, agent bool) tea.Cmd {
	return func() tea.Msg {
		var items []overlay.PickerItem
		if agent {
			items = filesToAgentPickerItems(files)
		} else {
			items = filesToPickerItemsWithRecent(mergeRecentIntoQuickOpen(files, recent, maxQuickOpenFiles), recent)
		}
		return overlay.PickerItemsReadyMsg{InstanceID: instanceID, ZoneID: zoneID, Items: items}
	}
}
