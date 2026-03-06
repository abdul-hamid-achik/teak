package app

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
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
	watcher   *fsnotify.Watcher
	rootDir   string
	msgChan   chan tea.Msg
	debounce  map[string]time.Time
}

func newFileWatcher(rootDir string) (*fileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	fw := &fileWatcher{
		watcher:  w,
		rootDir:  rootDir,
		msgChan:  make(chan tea.Msg, 32),
		debounce: make(map[string]time.Time),
	}
	// Watch root directory for tree changes
	if rootDir != "" {
		fw.watchDirRecursive(rootDir, 0)
		// Watch .git directory for commit/push/branch changes
		gitDir := filepath.Join(rootDir, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			_ = fw.watcher.Add(gitDir)
			refsDir := filepath.Join(gitDir, "refs")
			if info, err := os.Stat(refsDir); err == nil && info.IsDir() {
				_ = fw.watcher.Add(refsDir)
				headsDir := filepath.Join(refsDir, "heads")
				if info, err := os.Stat(headsDir); err == nil && info.IsDir() {
					_ = fw.watcher.Add(headsDir)
				}
			}
		}
	}
	go fw.listen()
	return fw, nil
}

// watchDirRecursive adds a directory and its immediate subdirectories to the watcher.
// maxDepth limits recursion to avoid watching too deep.
func (fw *fileWatcher) watchDirRecursive(dir string, depth int) {
	if depth > 3 {
		return
	}
	_ = fw.watcher.Add(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			fw.watchDirRecursive(filepath.Join(dir, e.Name()), depth+1)
		}
	}
}

// WatchFile adds a file path to the watcher.
func (fw *fileWatcher) WatchFile(path string) {
	_ = fw.watcher.Add(path)
}

// UnwatchFile removes a file path from the watcher.
func (fw *fileWatcher) UnwatchFile(path string) {
	_ = fw.watcher.Remove(path)
}

// WatchDir adds a directory to the watcher.
func (fw *fileWatcher) WatchDir(dir string) {
	_ = fw.watcher.Add(dir)
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
			if last, ok := fw.debounce[event.Name]; ok && now.Sub(last) < 100*time.Millisecond {
				continue
			}
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
						fw.WatchDir(event.Name)
					}
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
