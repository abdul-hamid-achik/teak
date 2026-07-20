package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/log"
	"github.com/fsnotify/fsnotify"
	"teak/internal/filetree"
	"teak/internal/text"
)

const (
	debounceWindow          = 100 * time.Millisecond
	debounceRetention       = 2 * time.Minute
	watchFDReserve          = 128
	minWatchLimit           = 32
	defaultWatchLimit       = 512
	watchScanBatchSize      = 256
	maxWatchScanDirs        = 2_048
	maxWatchScanEntries     = 100_000
	maxWatchControlOps      = 64
	maxPendingChangeBytes   = 128 << 20
	maxOwnWriteExpectations = 1_024
	ownWriteExpectationTTL  = 5 * time.Second
	maxPendingFileReads     = 64
)

type watchControlAction uint8

const (
	watchControlAdd watchControlAction = iota
	watchControlRemove
	// watchControlDrop removes an invalidated fsnotify watch on the worker.
	// It is used after rename/remove notifications; doing fsnotify.Remove from
	// the event reader can otherwise stall delivery of every subsequent event.
	watchControlDrop
)

// FileChangedMsg is sent when an open file is modified externally.
type FileChangedMsg struct {
	Path        string
	Data        []byte     // legacy input; converted by a background command
	Snapshot    *text.Rope // prepared off Update by the production watcher
	Observation uint64     // watcher order; zero is reserved for direct/test messages
	NeedsRead   bool       // aggregate-budget fallback: re-read asynchronously
	// RequiresConflict marks an unattributed watcher observation that began no
	// later than a completed save. A watermark is ordering evidence, not proof
	// that the event belongs to Teak's own bytes, so this observation must not
	// be discarded or auto-reloaded into a now-clean buffer.
	RequiresConflict bool
	// Missing reports that the watched path was removed or renamed. It is kept
	// separate from NeedsRead so the UI can distinguish a failed re-read from a
	// confirmed disappearance and choose the appropriate conflict workflow.
	Missing bool
}

// TreeChangedMsg is sent when a directory in the tree changes (file created/deleted/renamed).
type TreeChangedMsg struct {
	Dir string
}

type watcherFileChangeWakeMsg struct{}

type ownWriteExpectation struct {
	snapshot *text.Rope
	expires  time.Time
}

// fileWatcher watches open files and the project directory for external changes.
type fileWatcher struct {
	watcher           *fsnotify.Watcher
	rootDir           string
	msgChan           chan tea.Msg
	debounce          map[string]time.Time
	gitignorePatterns []string
	gitignoreLoaded   bool
	maxWatches        int
	readDirBatches    directoryBatchReader
	now               func() time.Time // deterministic debounce seam for tests

	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
	workers   sync.WaitGroup
	scanQueue chan string
	scanMu    sync.Mutex
	queued    map[string]struct{}
	scanAgain map[string]struct{}
	// WatchFile and UnwatchFile are called from Update. They change the
	// logical open-file set immediately, then hand fsnotify Add/Remove to this
	// bounded, latest-wins worker queue.
	watchControlMu      sync.Mutex
	watchControlPending map[string]watchControlAction
	watchControlOrder   []string
	watchControlWake    chan struct{}
	watchControlRecheck bool
	watchAdd            func(string) error
	watchRemove         func(string) error

	pendingMu          sync.Mutex
	pendingFileChanges map[string]FileChangedMsg
	pendingChangeBytes int

	// File contents are read on this dedicated worker, never on the fsnotify
	// event reader.  A path has at most one queued read; a newer observation
	// replaces its metadata while the older read is in flight.
	fileReadMu      sync.Mutex
	fileReadPending map[string]uint64
	fileReadQueued  map[string]struct{}
	fileReadQueue   chan string

	ownWriteMu  sync.Mutex
	ownWrites   map[string]ownWriteExpectation
	observation atomic.Uint64

	mu           sync.RWMutex
	watched      map[string]struct{}
	watching     map[string]struct{}
	removing     map[string]struct{}
	fileWatches  map[string]struct{} // direct file watches, never tree/root directory watches
	openFiles    map[string]struct{}
	limitReached bool
}

func newFileWatcher(rootDir string) (*fileWatcher, error) {
	return newFileWatcherWithBatchReader(rootDir, defaultMaxWatches(), readDirBatches)
}

func newFileWatcherWithMaxWatches(rootDir string, maxWatches int) (*fileWatcher, error) {
	return newFileWatcherWithBatchReader(rootDir, maxWatches, readDirBatches)
}

// newFileWatcherWithReadDir is retained as a narrow compatibility seam for
// tests. Production always uses batched directory reads.
func newFileWatcherWithReadDir(rootDir string, maxWatches int, readDir func(string) ([]os.DirEntry, error)) (*fileWatcher, error) {
	return newFileWatcherWithBatchReader(rootDir, maxWatches, func(ctx context.Context, path string, _ int, visit func([]os.DirEntry) bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := readDir(path)
		if len(entries) > 0 {
			visit(entries)
		}
		return err
	})
}

func newFileWatcherWithBatchReader(rootDir string, maxWatches int, readBatches directoryBatchReader) (*fileWatcher, error) {
	if maxWatches <= 0 {
		maxWatches = defaultMaxWatches()
	}
	if readBatches == nil {
		readBatches = readDirBatches
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	cleanRoot := filepath.Clean(rootDir)
	if rootDir == "" {
		cleanRoot = ""
	}
	ctx, cancel := context.WithCancel(context.Background())
	fw := &fileWatcher{
		watcher:             w,
		rootDir:             cleanRoot,
		msgChan:             make(chan tea.Msg, 32),
		debounce:            make(map[string]time.Time),
		maxWatches:          maxWatches,
		watched:             make(map[string]struct{}),
		watching:            make(map[string]struct{}),
		removing:            make(map[string]struct{}),
		fileWatches:         make(map[string]struct{}),
		openFiles:           make(map[string]struct{}),
		readDirBatches:      readBatches,
		now:                 time.Now,
		ctx:                 ctx,
		cancel:              cancel,
		done:                make(chan struct{}),
		scanQueue:           make(chan string, 64),
		queued:              make(map[string]struct{}),
		scanAgain:           make(map[string]struct{}),
		watchControlPending: make(map[string]watchControlAction),
		watchControlWake:    make(chan struct{}, 1),
		watchAdd:            w.Add,
		watchRemove:         w.Remove,
		pendingFileChanges:  make(map[string]FileChangedMsg),
		fileReadPending:     make(map[string]uint64),
		fileReadQueued:      make(map[string]struct{}),
		fileReadQueue:       make(chan string, maxPendingFileReads),
		ownWrites:           make(map[string]ownWriteExpectation),
	}

	fw.workers.Add(4)
	go fw.listen()
	go fw.scanLoop()
	go fw.watchControlLoop()
	go fw.fileReadLoop()
	if cleanRoot != "" {
		fw.queueDirectoryScan(cleanRoot)
	}
	go func() {
		fw.workers.Wait()
		close(fw.done)
	}()
	return fw, nil
}

// queueDirectoryScan schedules recursive discovery on a background worker.
// It is deliberately non-blocking: filesystem traversal must never delay the
// Bubble Tea update loop or editor startup.
func (fw *fileWatcher) queueDirectoryScan(dir string) {
	if dir == "" || fw.ctx.Err() != nil {
		return
	}
	clean := filepath.Clean(dir)
	fw.scanMu.Lock()
	if _, exists := fw.queued[clean]; exists {
		// A scan already in progress is not enough after an event overflow: it
		// may have observed the tree before the lost events. Keep exactly one
		// follow-up scan instead of silently dropping the reconciliation.
		if fw.scanAgain == nil {
			fw.scanAgain = make(map[string]struct{})
		}
		fw.scanAgain[clean] = struct{}{}
		fw.scanMu.Unlock()
		return
	}
	fw.queued[clean] = struct{}{}
	fw.scanMu.Unlock()

	select {
	case fw.scanQueue <- clean:
	case <-fw.ctx.Done():
		fw.scanMu.Lock()
		delete(fw.queued, clean)
		fw.scanMu.Unlock()
	default:
		// A full queue should not stall fsnotify's reader. The root and newly
		// created directories are retried by subsequent tree events.
		fw.scanMu.Lock()
		delete(fw.queued, clean)
		fw.scanMu.Unlock()
		log.Warn("file watcher scan queue full", "root", fw.rootDir, "dir", clean)
	}
}

func (fw *fileWatcher) scanLoop() {
	defer fw.workers.Done()
	for {
		select {
		case <-fw.ctx.Done():
			return
		case dir := <-fw.scanQueue:
			fw.scanDirectoryTree(dir)
			fw.scanMu.Lock()
			_, again := fw.scanAgain[dir]
			delete(fw.scanAgain, dir)
			if !again {
				delete(fw.queued, dir)
			}
			fw.scanMu.Unlock()
			if again {
				select {
				case fw.scanQueue <- dir:
				case <-fw.ctx.Done():
					return
				default:
					fw.scanMu.Lock()
					delete(fw.queued, dir)
					fw.scanMu.Unlock()
					log.Warn("file watcher follow-up scan queue full", "root", fw.rootDir, "dir", dir)
				}
			}
		}
	}
}

func (fw *fileWatcher) scanDirectoryTree(root string) {
	if err := fw.ctx.Err(); err != nil {
		return
	}
	// Create events are intentionally probed here rather than by listen: stat
	// can block on slow filesystems and an event reader must remain drainable.
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return
	}
	if filepath.Clean(root) == fw.rootDir {
		// Even the initial fsnotify Add and .git metadata probes may block on a
		// network filesystem. They belong to this worker, never NewModel.
		fw.addWatch(root)
		fw.addGitWatches()
	}
	// Configuration I/O belongs to the background scan, not NewModel. This
	// keeps the first TUI frame independent of a slow network filesystem.
	if !fw.gitignoreLoaded && fw.rootDir != "" {
		patterns, err := filetree.LoadGitignorePatternsContext(fw.ctx, fw.rootDir)
		if err != nil {
			return
		}
		fw.gitignorePatterns = patterns
		fw.gitignoreLoaded = true
	}

	stack := []string{root}
	directories, entriesSeen := 0, 0
	for len(stack) > 0 {
		if fw.ctx.Err() != nil {
			return
		}
		if directories >= maxWatchScanDirs || entriesSeen >= maxWatchScanEntries {
			log.Warn("file watcher scan budget reached", "root", root, "directories", directories, "entries", entriesSeen)
			return
		}
		dir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !fw.shouldWatchDir(dir) || !fw.addWatch(dir) {
			continue
		}
		directories++
		err := fw.readDirBatches(fw.ctx, dir, watchScanBatchSize, func(batch []os.DirEntry) bool {
			for _, entry := range batch {
				if fw.ctx.Err() != nil || entriesSeen >= maxWatchScanEntries {
					return false
				}
				entriesSeen++
				if entry.IsDir() {
					stack = append(stack, filepath.Join(dir, entry.Name()))
				}
			}
			return true
		})
		if err != nil && fw.ctx.Err() != nil {
			return
		}
	}
}

func (fw *fileWatcher) addGitWatches() {
	if fw.rootDir == "" || fw.ctx.Err() != nil {
		return
	}
	gitDir := filepath.Join(fw.rootDir, ".git")
	refsDir := filepath.Join(gitDir, "refs")
	headsDir := filepath.Join(refsDir, "heads")
	for _, dir := range []string{gitDir, refsDir, headsDir} {
		if fw.ctx.Err() != nil {
			return
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			fw.addWatch(dir)
		}
	}
}

// WatchFile records an open path immediately and queues fsnotify work for the
// dedicated worker. It must remain safe to call from Bubble Tea Update.
func (fw *fileWatcher) WatchFile(path string) {
	if path == "" {
		return
	}
	clean := filepath.Clean(path)
	fw.mu.Lock()
	fw.openFiles[clean] = struct{}{}
	fw.mu.Unlock()
	fw.queueWatchControl(clean, watchControlAdd)
}

// UnwatchFile removes the logical open path immediately and queues the
// potentially blocking fsnotify Remove operation.
func (fw *fileWatcher) UnwatchFile(path string) {
	if path == "" {
		return
	}
	clean := filepath.Clean(path)
	fw.mu.Lock()
	delete(fw.openFiles, clean)
	fw.mu.Unlock()
	fw.queueWatchControl(clean, watchControlRemove)
}

// WatchDir adds a directory to the watcher.
func (fw *fileWatcher) WatchDir(dir string) {
	fw.queueDirectoryScan(dir)
}

// queueWatchControl coalesces requests by path while retaining FIFO ordering
// between distinct paths. The queue is deliberately bounded so an event storm
// cannot grow memory without limit; openFiles remains authoritative even when
// the worker queue is saturated.
func (fw *fileWatcher) queueWatchControl(path string, action watchControlAction) {
	if path == "" || fw.ctx.Err() != nil {
		return
	}
	fw.watchControlMu.Lock()
	if _, exists := fw.watchControlPending[path]; exists {
		fw.watchControlPending[path] = action
		fw.watchControlMu.Unlock()
		return
	}
	if len(fw.watchControlOrder) >= maxWatchControlOps {
		// The logical state is already in openFiles. Coalesce one background
		// reconciliation so a saturated queue cannot permanently lose an
		// Add/Remove transition.
		fw.watchControlRecheck = true
		fw.watchControlMu.Unlock()
		return
	}
	fw.watchControlPending[path] = action
	fw.watchControlOrder = append(fw.watchControlOrder, path)
	fw.watchControlMu.Unlock()

	select {
	case fw.watchControlWake <- struct{}{}:
	case <-fw.ctx.Done():
	default:
	}
}

func (fw *fileWatcher) nextWatchControl() (string, watchControlAction, bool) {
	fw.watchControlMu.Lock()
	defer fw.watchControlMu.Unlock()
	if len(fw.watchControlOrder) == 0 {
		return "", 0, false
	}
	path := fw.watchControlOrder[0]
	fw.watchControlOrder = fw.watchControlOrder[1:]
	action := fw.watchControlPending[path]
	delete(fw.watchControlPending, path)
	return path, action, true
}

func (fw *fileWatcher) takeWatchControlRecheck() bool {
	fw.watchControlMu.Lock()
	defer fw.watchControlMu.Unlock()
	if !fw.watchControlRecheck {
		return false
	}
	fw.watchControlRecheck = false
	return true
}

func (fw *fileWatcher) watchControlLoop() {
	defer fw.workers.Done()
	for {
		select {
		case <-fw.ctx.Done():
			return
		case <-fw.watchControlWake:
		}
		for {
			if fw.ctx.Err() != nil {
				return
			}
			path, action, ok := fw.nextWatchControl()
			if !ok {
				if fw.takeWatchControlRecheck() {
					fw.reconcileFileWatches()
					continue
				}
				break
			}
			switch action {
			case watchControlAdd:
				fw.applyWatchFile(path)
			case watchControlRemove:
				fw.applyUnwatchFile(path)
			case watchControlDrop:
				fw.removeWatch(path)
			}
		}
	}
}

func (fw *fileWatcher) applyWatchFile(path string) {
	if !fw.isOpenFile(path) || fw.isWatched(filepath.Dir(path)) {
		return
	}
	if !fw.addWatch(path) {
		return
	}
	fw.mu.Lock()
	_, stillOpen := fw.openFiles[path]
	if stillOpen {
		fw.fileWatches[path] = struct{}{}
	}
	fw.mu.Unlock()
	if !stillOpen {
		fw.removeWatch(path)
	}
}

func (fw *fileWatcher) applyUnwatchFile(path string) {
	if fw.isOpenFile(path) {
		return
	}
	fw.removeWatch(path)
	fw.mu.Lock()
	delete(fw.fileWatches, path)
	fw.mu.Unlock()
}

// reconcileFileWatches converges the actual direct watches with openFiles
// after the bounded control queue overflowed. It runs only on the watcher
// worker and is coalesced to one request, so Update never pays this cost.
func (fw *fileWatcher) reconcileFileWatches() {
	fw.mu.RLock()
	toRemove := make([]string, 0)
	for path := range fw.fileWatches {
		if _, open := fw.openFiles[path]; !open {
			toRemove = append(toRemove, path)
		}
	}
	toAdd := make([]string, 0)
	for path := range fw.openFiles {
		if _, direct := fw.fileWatches[path]; direct {
			continue
		}
		if _, parentWatched := fw.watched[filepath.Dir(path)]; !parentWatched {
			toAdd = append(toAdd, path)
		}
	}
	fw.mu.RUnlock()

	for _, path := range toRemove {
		fw.applyUnwatchFile(path)
	}
	for _, path := range toAdd {
		fw.applyWatchFile(path)
	}
}

func (fw *fileWatcher) pruneDebounceEntries(now time.Time) {
	for path, last := range fw.debounce {
		if now.Sub(last) > debounceRetention {
			delete(fw.debounce, path)
		}
	}
}

func (fw *fileWatcher) clockNow() time.Time {
	if fw.now != nil {
		return fw.now()
	}
	return time.Now()
}

// processEvent contains all event classification so it can be exercised with
// a deterministic clock. It never reads from disk or calls fsnotify Add/Remove.
func (fw *fileWatcher) processEvent(event fsnotify.Event, now time.Time) {
	if event.Name == "" || fw.ctx.Err() != nil {
		return
	}
	path := filepath.Clean(event.Name)
	open := fw.isOpenFile(path)
	mutatesOpen := open && (event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename))

	// Open buffers are correctness-sensitive. Their events intentionally bypass
	// tree debounce; the bounded per-path reader/pending map does the safe
	// coalescing without ever losing the newest observation.
	if mutatesOpen {
		observation := fw.observation.Add(1)
		if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
			fw.publish(FileChangedMsg{
				Path:        path,
				Observation: observation,
				NeedsRead:   true,
				Missing:     true,
			})
			// A direct file watch vanishes on atomic replacement. Drop it on the
			// control worker and rescan the parent so a parent watch is installed
			// before the replacement Create arrives (also works outside rootDir).
			fw.queueWatchControl(path, watchControlDrop)
			fw.queueDirectoryScan(filepath.Dir(path))
		} else {
			fw.queueFileRead(path, observation)
		}
	}

	if isGitInternalPath(path, fw.rootDir) {
		fw.publishDebouncedTreeChange(fw.rootDir, path, now)
		return
	}
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		dir := filepath.Dir(path)
		fw.publishDebouncedTreeChange(dir, path, now)
		if event.Has(fsnotify.Create) {
			// scanDirectoryTree performs the directory probe off this listener.
			fw.queueDirectoryScan(path)
		}
		if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
			fw.queueWatchControl(path, watchControlDrop)
		}
	}
}

func (fw *fileWatcher) publishDebouncedTreeChange(dir, key string, now time.Time) {
	if fw.debounce == nil {
		fw.debounce = make(map[string]time.Time)
	}
	if last, ok := fw.debounce[key]; ok && now.Sub(last) < debounceWindow {
		return
	}
	fw.pruneDebounceEntries(now)
	fw.debounce[key] = now
	fw.publish(TreeChangedMsg{Dir: dir})
}

// processError recovers from both ordinary fsnotify errors and queue overflow.
// The event stream is no longer complete, so every open buffer is marked for a
// bounded asynchronous re-read and the tree/watch set is reconciled.
func (fw *fileWatcher) processError(err error) {
	if err == nil || fw.ctx.Err() != nil {
		return
	}
	if errors.Is(err, fsnotify.ErrEventOverflow) {
		log.Error("file watcher event overflow; reconciling open files and tree", "root", fw.rootDir, "err", err)
	} else {
		log.Error("file watcher error; reconciling open files and tree", "root", fw.rootDir, "err", err)
	}

	fw.mu.RLock()
	open := make([]string, 0, len(fw.openFiles))
	for path := range fw.openFiles {
		open = append(open, path)
	}
	root := fw.rootDir
	fw.mu.RUnlock()
	for _, path := range open {
		fw.publish(FileChangedMsg{
			Path:        path,
			Observation: fw.observation.Add(1),
			NeedsRead:   true,
		})
	}
	if root != "" {
		// Do not debounce recovery: a dropped tree signal would leave the UI
		// stale after the one condition where we know events were lost.
		fw.publish(TreeChangedMsg{Dir: root})
		fw.queueDirectoryScan(root)
	}
	fw.requestWatchReconcile()
}

func (fw *fileWatcher) requestWatchReconcile() {
	fw.watchControlMu.Lock()
	fw.watchControlRecheck = true
	fw.watchControlMu.Unlock()
	select {
	case fw.watchControlWake <- struct{}{}:
	case <-fw.ctx.Done():
	default:
	}
}

func (fw *fileWatcher) queueFileRead(path string, observation uint64) {
	if path == "" || fw.ctx.Err() != nil {
		return
	}
	path = filepath.Clean(path)
	fw.fileReadMu.Lock()
	if fw.fileReadPending == nil {
		fw.fileReadPending = make(map[string]uint64)
	}
	if fw.fileReadQueued == nil {
		fw.fileReadQueued = make(map[string]struct{})
	}
	fw.fileReadPending[path] = observation
	if _, queued := fw.fileReadQueued[path]; queued {
		fw.fileReadMu.Unlock()
		return
	}
	fw.fileReadQueued[path] = struct{}{}
	queue := fw.fileReadQueue
	fw.fileReadMu.Unlock()
	if queue == nil {
		fw.publish(FileChangedMsg{Path: path, Observation: observation, NeedsRead: true})
		return
	}
	select {
	case queue <- path:
	case <-fw.ctx.Done():
	default:
		// A saturated reader must degrade to the existing asynchronous model
		// re-read, never block or silently discard the external-change signal.
		fw.fileReadMu.Lock()
		latest := fw.fileReadPending[path]
		delete(fw.fileReadPending, path)
		delete(fw.fileReadQueued, path)
		fw.fileReadMu.Unlock()
		fw.publish(FileChangedMsg{Path: path, Observation: latest, NeedsRead: true})
	}
}

func (fw *fileWatcher) fileReadLoop() {
	defer fw.workers.Done()
	for {
		select {
		case <-fw.ctx.Done():
			return
		case path := <-fw.fileReadQueue:
			fw.fileReadMu.Lock()
			observation := fw.fileReadPending[path]
			delete(fw.fileReadPending, path)
			delete(fw.fileReadQueued, path)
			fw.fileReadMu.Unlock()
			if observation == 0 || !fw.isOpenFile(path) {
				continue
			}
			data, err := readEditorFile(fw.ctx, path)
			if err != nil {
				fw.publish(FileChangedMsg{
					Path:        path,
					Observation: observation,
					NeedsRead:   true,
					Missing:     errors.Is(err, os.ErrNotExist),
				})
				continue
			}
			if fw.matchesExpectedOwnWrite(path, data, fw.clockNow()) {
				continue
			}
			// The worker owns data from readEditorFile until this message is
			// published, so the immutable snapshot can take it directly.
			fw.publish(FileChangedMsg{Path: path, Snapshot: text.NewOwned(data), Observation: observation})
		}
	}
}

func (fw *fileWatcher) listen() {
	defer fw.workers.Done()
	for {
		select {
		case <-fw.ctx.Done():
			return
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			fw.processEvent(event, fw.clockNow())
		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			fw.processError(err)
		}
	}
}

// publish delivers watcher events without ever blocking the fsnotify reader.
// If the UI is busy, keeping the newest event is preferable to leaking a
// goroutine or stalling all filesystem notifications.
func (fw *fileWatcher) publish(msg tea.Msg) {
	if fw.ctx.Err() != nil {
		return
	}
	if changed, ok := msg.(FileChangedMsg); ok {
		// Open-file changes are correctness-sensitive: dropping one can let a
		// later save overwrite external data without warning. Coalesce only by
		// path and use the channel as a wake signal; listenCmd drains this map
		// before ordinary tree notifications.
		fw.pendingMu.Lock()
		path := filepath.Clean(changed.Path)
		if previous, exists := fw.pendingFileChanges[path]; exists {
			// Reads can finish out of order. Keep the newest filesystem
			// observation rather than allowing an older snapshot to overwrite a
			// newer Remove/Create result while the UI is busy.
			if previous.Observation != 0 && changed.Observation != 0 && previous.Observation > changed.Observation {
				fw.pendingMu.Unlock()
				return
			}
			if previous.Snapshot != nil {
				fw.pendingChangeBytes -= previous.Snapshot.Len()
			}
		}
		if changed.Snapshot != nil {
			if size := changed.Snapshot.Len(); size > maxPendingChangeBytes-fw.pendingChangeBytes {
				// Preserve the correctness signal and observation order while
				// releasing the large snapshot. Update will schedule a bounded
				// re-read when it drains this entry.
				changed.Snapshot = nil
				changed.NeedsRead = true
			} else {
				fw.pendingChangeBytes += size
			}
		}
		fw.pendingFileChanges[path] = changed
		fw.pendingMu.Unlock()
		select {
		case fw.msgChan <- watcherFileChangeWakeMsg{}:
		default:
		}
		return
	}
	select {
	case fw.msgChan <- msg:
		return
	default:
	}
	select {
	case <-fw.msgChan:
	default:
	}
	select {
	case fw.msgChan <- msg:
	case <-fw.ctx.Done():
	default:
	}
}

// listenCmd returns a tea.Cmd that waits for the next file system event.
func (fw *fileWatcher) listenCmd() tea.Cmd {
	ch := fw.msgChan
	return func() tea.Msg {
		for {
			if fw.ctx.Err() != nil {
				return nil
			}
			if msg, ok := fw.popPendingFileChange(); ok {
				return msg
			}
			select {
			case <-fw.ctx.Done():
				return nil
			case msg := <-ch:
				if _, wake := msg.(watcherFileChangeWakeMsg); wake {
					continue
				}
				return msg
			}
		}
	}
}

func (fw *fileWatcher) popPendingFileChange() (FileChangedMsg, bool) {
	fw.pendingMu.Lock()
	defer fw.pendingMu.Unlock()
	for path, msg := range fw.pendingFileChanges {
		delete(fw.pendingFileChanges, path)
		if msg.Snapshot != nil {
			fw.pendingChangeBytes -= msg.Snapshot.Len()
		}
		return msg, true
	}
	return FileChangedMsg{}, false
}

// expectOwnWrite records an immutable snapshot before an asynchronous save
// starts. The fsnotify worker compares observed bytes against it off the UI
// goroutine, preventing Teak's own atomic rename from looking like an external
// edit. Matching expectations remain briefly because one save can emit more
// than one platform event.
func (fw *fileWatcher) expectOwnWrite(path string, snapshot *text.Rope) {
	if fw == nil || path == "" || snapshot == nil {
		return
	}
	now := time.Now()
	fw.ownWriteMu.Lock()
	defer fw.ownWriteMu.Unlock()
	if fw.ownWrites == nil {
		fw.ownWrites = make(map[string]ownWriteExpectation)
	}
	fw.pruneOwnWritesLocked(now)
	if len(fw.ownWrites) >= maxOwnWriteExpectations {
		for existing := range fw.ownWrites {
			delete(fw.ownWrites, existing)
			break
		}
	}
	fw.ownWrites[filepath.Clean(path)] = ownWriteExpectation{
		snapshot: snapshot,
		expires:  now.Add(ownWriteExpectationTTL),
	}
}

func (fw *fileWatcher) matchesExpectedOwnWrite(path string, data []byte, now time.Time) bool {
	if fw == nil {
		return false
	}
	fw.ownWriteMu.Lock()
	defer fw.ownWriteMu.Unlock()
	fw.pruneOwnWritesLocked(now)
	expected, ok := fw.ownWrites[filepath.Clean(path)]
	return ok && expected.snapshot != nil && expected.snapshot.EqualBytes(data)
}

func (fw *fileWatcher) pruneOwnWritesLocked(now time.Time) {
	for path, expected := range fw.ownWrites {
		if !expected.expires.After(now) {
			delete(fw.ownWrites, path)
		}
	}
}

func (fw *fileWatcher) cancelOwnWrite(path string, snapshot *text.Rope) {
	if fw == nil {
		return
	}
	fw.ownWriteMu.Lock()
	defer fw.ownWriteMu.Unlock()
	clean := filepath.Clean(path)
	if expected, ok := fw.ownWrites[clean]; ok && expected.snapshot == snapshot {
		delete(fw.ownWrites, clean)
	}
}

// completeOwnWrite returns a watermark covering every file observation that
// began before the atomic save finished. A late-delivered older snapshot can
// then be discarded by the model instead of reloading stale bytes.
func (fw *fileWatcher) completeOwnWrite() uint64 {
	if fw == nil {
		return 0
	}
	return fw.observation.Load()
}

// Close shuts down the watcher.
func (fw *fileWatcher) Close() {
	fw.closeOnce.Do(func() {
		fw.cancel()
		if err := fw.watcher.Close(); err != nil {
			log.Error("file watcher close failed", "root", fw.rootDir, "err", err)
		}
	})
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
	if _, ok := fw.watched[clean]; ok {
		fw.mu.Unlock()
		return true
	}
	if _, ok := fw.watching[clean]; ok {
		fw.mu.Unlock()
		return true
	}
	if _, removing := fw.removing[clean]; removing {
		fw.mu.Unlock()
		return false
	}
	if fw.maxWatches > 0 && len(fw.watched)+len(fw.watching) >= fw.maxWatches {
		fw.markLimitReachedLocked()
		fw.mu.Unlock()
		return false
	}
	fw.watching[clean] = struct{}{}
	add := fw.watchAdd
	if add == nil {
		add = fw.watcher.Add
	}
	fw.mu.Unlock()

	// fsnotify can block on a slow or overloaded filesystem. Never hold the
	// state lock while making that call: Update must still be able to record an
	// open/closed file while a worker is waiting here.
	err := add(clean)

	fw.mu.Lock()
	delete(fw.watching, clean)
	if err != nil {
		if os.IsNotExist(err) {
			fw.mu.Unlock()
			return false
		}
		if isWatchLimitError(err) {
			fw.markLimitReachedLocked()
			fw.mu.Unlock()
			return false
		}
		fw.mu.Unlock()
		log.Error("file watcher add failed", "path", clean, "err", err)
		return false
	}

	fw.watched[clean] = struct{}{}
	fw.mu.Unlock()
	return true
}

func (fw *fileWatcher) removeWatch(path string) {
	if path == "" {
		return
	}
	clean := filepath.Clean(path)

	fw.mu.Lock()
	prefix := clean + string(filepath.Separator)
	var toRemove []string
	for watched := range fw.watched {
		if watched == clean || strings.HasPrefix(watched, prefix) {
			toRemove = append(toRemove, watched)
			delete(fw.watched, watched)
			fw.removing[watched] = struct{}{}
			delete(fw.fileWatches, watched)
		}
	}
	remove := fw.watchRemove
	if remove == nil {
		remove = fw.watcher.Remove
	}
	fw.mu.Unlock()
	if len(toRemove) == 0 {
		return
	}
	for _, watched := range toRemove {
		if err := remove(watched); err != nil && !os.IsNotExist(err) && !errors.Is(err, fsnotify.ErrNonExistentWatch) {
			log.Error("file watcher remove failed", "path", watched, "err", err)
		}
		fw.mu.Lock()
		delete(fw.removing, watched)
		fw.mu.Unlock()
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

func (fw *fileWatcher) isOpenFile(path string) bool {
	if path == "" {
		return false
	}
	clean := filepath.Clean(path)
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	_, ok := fw.openFiles[clean]
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
