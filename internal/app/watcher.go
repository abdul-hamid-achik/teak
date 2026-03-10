package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/log"
	"github.com/fsnotify/fsnotify"
	"teak/internal/filetree"
)

const (
	debounceWindow    = 100 * time.Millisecond
	debounceRetention = 2 * time.Minute
	watchFDReserve    = 128
	minWatchLimit     = 32
	defaultWatchLimit = 512
)

// FileChangedMsg is sent when an open file is modified externally.
type FileChangedMsg struct {
	Path string
	Data []byte
}

// TreeChangedMsg is sent when a directory in the tree changes (file created/deleted/renamed).
type TreeChangedMsg struct {
	Dir string
}

// fileWatcher watches open files and the project directory for external changes.
type fileWatcher struct {
	watcher           *fsnotify.Watcher
	rootDir           string
	msgChan           chan tea.Msg
	debounce          map[string]time.Time
	gitignorePatterns []string
	maxWatches        int

	mu           sync.RWMutex
	watched      map[string]struct{}
	limitReached bool
}

func newFileWatcher(rootDir string) (*fileWatcher, error) {
	return newFileWatcherWithMaxWatches(rootDir, defaultMaxWatches())
}

func newFileWatcherWithMaxWatches(rootDir string, maxWatches int) (*fileWatcher, error) {
	if maxWatches <= 0 {
		maxWatches = defaultMaxWatches()
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	cleanRoot := filepath.Clean(rootDir)
	if rootDir == "" {
		cleanRoot = ""
	}
	fw := &fileWatcher{
		watcher:           w,
		rootDir:           cleanRoot,
		msgChan:           make(chan tea.Msg, 32),
		debounce:          make(map[string]time.Time),
		gitignorePatterns: filetree.LoadGitignorePatterns(cleanRoot),
		maxWatches:        maxWatches,
		watched:           make(map[string]struct{}),
	}

	// Watch root directory for tree changes
	if cleanRoot != "" {
		fw.addWatch(cleanRoot)
		// Watch .git directory for commit/push/branch changes
		gitDir := filepath.Join(cleanRoot, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			fw.addWatch(gitDir)
			refsDir := filepath.Join(gitDir, "refs")
			if info, err := os.Stat(refsDir); err == nil && info.IsDir() {
				fw.addWatch(refsDir)
				headsDir := filepath.Join(refsDir, "heads")
				if info, err := os.Stat(headsDir); err == nil && info.IsDir() {
					fw.addWatch(headsDir)
				}
			}
		}
		fw.watchDirChildrenRecursive(cleanRoot)
	}
	go fw.listen()
	return fw, nil
}

// watchDirRecursive adds a directory and all visible subdirectories to the watcher.
func (fw *fileWatcher) watchDirRecursive(dir string) {
	if !fw.shouldWatchDir(dir) {
		return
	}
	if !fw.addWatch(dir) {
		return
	}
	fw.watchDirChildrenRecursive(dir)
}

func (fw *fileWatcher) watchDirChildrenRecursive(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			fw.watchDirRecursive(filepath.Join(dir, e.Name()))
		}
	}
}

// WatchFile adds a file path to the watcher.
func (fw *fileWatcher) WatchFile(path string) {
	if path == "" {
		return
	}
	clean := filepath.Clean(path)
	if fw.isWatched(filepath.Dir(clean)) {
		return
	}
	fw.addWatch(clean)
}

// UnwatchFile removes a file path from the watcher.
func (fw *fileWatcher) UnwatchFile(path string) {
	fw.removeWatch(path)
}

// WatchDir adds a directory to the watcher.
func (fw *fileWatcher) WatchDir(dir string) {
	fw.watchDirRecursive(dir)
}

func (fw *fileWatcher) pruneDebounceEntries(now time.Time) {
	for path, last := range fw.debounce {
		if now.Sub(last) > debounceRetention {
			delete(fw.debounce, path)
		}
	}
}

func (fw *fileWatcher) listen() {
	for {
		select {
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			// Debounce: skip if we saw this path in the last 100ms
			now := time.Now()
			if last, ok := fw.debounce[event.Name]; ok && now.Sub(last) < debounceWindow {
				continue
			}
			fw.pruneDebounceEntries(now)
			fw.debounce[event.Name] = now

			// Detect .git directory changes (commit, push, branch switch)
			if isGitInternalPath(event.Name, fw.rootDir) {
				fw.msgChan <- TreeChangedMsg{Dir: fw.rootDir}
				continue
			}

			if event.Has(fsnotify.Write) {
				// File modified externally — read new content
				data, err := os.ReadFile(event.Name)
				if err == nil {
					fw.msgChan <- FileChangedMsg{Path: event.Name, Data: data}
				}
			}
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				// Directory structure changed — notify tree
				dir := filepath.Dir(event.Name)
				fw.msgChan <- TreeChangedMsg{Dir: dir}
				// If a new directory was created, watch it too
				if event.Has(fsnotify.Create) {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						fw.watchDirRecursive(event.Name)
					}
				}
				if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
					fw.removeWatch(event.Name)
				}
			}
		case _, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// listenCmd returns a tea.Cmd that waits for the next file system event.
func (fw *fileWatcher) listenCmd() tea.Cmd {
	ch := fw.msgChan
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// Close shuts down the watcher.
func (fw *fileWatcher) Close() {
	fw.watcher.Close()
}

// isGitInternalPath returns true if the path is inside the .git directory.
func isGitInternalPath(path, rootDir string) bool {
	gitDir := filepath.Join(rootDir, ".git")
	return path == gitDir || strings.HasPrefix(path, gitDir+string(filepath.Separator))
}

func (fw *fileWatcher) shouldWatchDir(dir string) bool {
	if dir == "" {
		return true
	}
	clean := filepath.Clean(dir)
	if clean == fw.rootDir {
		return true
	}
	name := filepath.Base(clean)
	if strings.HasPrefix(name, ".") {
		return false
	}
	if fw.rootDir == "" {
		return true
	}
	rel, err := filepath.Rel(fw.rootDir, clean)
	if err != nil || rel == "." {
		return true
	}
	return !filetree.MatchesGitignore(rel, fw.gitignorePatterns, true)
}

func (fw *fileWatcher) addWatch(path string) bool {
	if path == "" {
		return false
	}
	clean := filepath.Clean(path)

	fw.mu.Lock()
	defer fw.mu.Unlock()

	if _, ok := fw.watched[clean]; ok {
		return true
	}
	if fw.maxWatches > 0 && len(fw.watched) >= fw.maxWatches {
		fw.markLimitReachedLocked()
		return false
	}
	if err := fw.watcher.Add(clean); err != nil {
		if os.IsNotExist(err) {
			return false
		}
		if isWatchLimitError(err) {
			fw.markLimitReachedLocked()
			return false
		}
		log.Error("file watcher add failed", "path", clean, "err", err)
		return false
	}

	fw.watched[clean] = struct{}{}
	return true
}

func (fw *fileWatcher) removeWatch(path string) {
	if path == "" {
		return
	}
	clean := filepath.Clean(path)

	fw.mu.Lock()
	defer fw.mu.Unlock()

	prefix := clean + string(filepath.Separator)
	var toRemove []string
	for watched := range fw.watched {
		if watched == clean || strings.HasPrefix(watched, prefix) {
			toRemove = append(toRemove, watched)
		}
	}
	if len(toRemove) == 0 {
		return
	}
	for _, watched := range toRemove {
		if err := fw.watcher.Remove(watched); err != nil && !os.IsNotExist(err) {
			log.Error("file watcher remove failed", "path", watched, "err", err)
		}
		delete(fw.watched, watched)
	}
}

func (fw *fileWatcher) markLimitReachedLocked() {
	if fw.limitReached {
		return
	}
	fw.limitReached = true
	log.Warn("file watcher limit reached", "root", fw.rootDir, "limit", fw.maxWatches)
}

func (fw *fileWatcher) isWatched(path string) bool {
	if path == "" {
		return false
	}
	clean := filepath.Clean(path)
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	_, ok := fw.watched[clean]
	return ok
}

func (fw *fileWatcher) watchedCount() int {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	return len(fw.watched)
}

func (fw *fileWatcher) watchLimitReached() bool {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	return fw.limitReached
}
