package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/session"
	"teak/internal/text"
)

func TestSessionRestoreLateResultCannotReplaceUserOpenedFile(t *testing.T) {
	root := t.TempDir()
	userPath := filepath.Join(root, "user.go")
	stalePath := filepath.Join(root, "stale.go")
	for _, path := range []string{userPath, stalePath} {
		if err := os.WriteFile(path, []byte("package p"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()
	generation := m.sessionRestoreGeneration
	opened, _ := m.openFilePinned(userPath)
	m = opened.(Model)
	updated, _ := m.Update(sessionRestoreResultMsg{
		Generation: generation,
		State:      session.State{RootDir: root, ActiveTab: 0, Tabs: []session.TabState{{FilePath: stalePath}}},
	})
	m = updated.(Model)
	if len(m.editors) != 1 || m.activeEditor().Buffer.FilePath != userPath {
		t.Fatalf("late restore replaced user tab: %+v", m.tabBar.Tabs)
	}
}

func TestSessionRestoreCommandFiltersTabsAndRemapsState(t *testing.T) {
	root := t.TempDir()
	kept := filepath.Join(root, "kept.go")
	if err := os.WriteFile(kept, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldLoad := loadSessionForRestore
	loadSessionForRestore = func(context.Context, string) (session.State, error) {
		return session.State{RootDir: root, ActiveTab: 1, Tabs: []session.TabState{
			{FilePath: filepath.Join(root, "missing.go")},
			{FilePath: kept, CursorLine: 2, CursorCol: 1, ScrollY: 3, WrapScrollY: 4, Pinned: true},
		}}, nil
	}
	defer func() { loadSessionForRestore = oldLoad }()

	cfg := config.DefaultConfig()
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()
	result := sessionRestoreCmd(m.sessionRestoreCtx, m.sessionRestoreGeneration, root)()
	updated, command := m.Update(result)
	m = updated.(Model)
	if len(m.editors) != 1 || m.activeTab != 0 || m.tabBar.Tabs[0].Preview {
		t.Fatalf("restored tabs/active/pin = %d/%d/%+v", len(m.editors), m.activeTab, m.tabBar.Tabs)
	}
	if m.welcome != nil && m.welcome.Active {
		t.Fatal("welcome screen remained active after session restore")
	}
	if command == nil {
		t.Fatal("session restore did not schedule async file loads")
	}
	loaded := command()
	updated, _ = m.Update(loaded)
	m = updated.(Model)
	ed := m.activeEditor()
	if ed.Buffer.Cursor.Line != 2 || ed.Buffer.Cursor.Col != 1 || ed.Viewport.ScrollY != 3 || ed.Viewport.WrapScrollY != 4 {
		t.Fatalf("restored state cursor=%+v scroll=%d/%d", ed.Buffer.Cursor, ed.Viewport.ScrollY, ed.Viewport.WrapScrollY)
	}
}

func TestSessionRestoreReportsAndRetainsSkippedTabs(t *testing.T) {
	root := t.TempDir()
	kept := filepath.Join(root, "kept.go")
	if err := os.WriteFile(kept, []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := session.TabState{FilePath: filepath.Join(root, "missing.go"), CursorLine: 3, Pinned: true}

	oldLoad := loadSessionForRestore
	loadSessionForRestore = func(context.Context, string) (session.State, error) {
		return session.State{RootDir: root, ActiveTab: 0, Tabs: []session.TabState{
			missing,
			{FilePath: kept},
		}}, nil
	}
	defer func() { loadSessionForRestore = oldLoad }()

	cfg := config.DefaultConfig()
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()
	result := sessionRestoreCmd(m.sessionRestoreCtx, m.sessionRestoreGeneration, root)()
	msg, ok := result.(sessionRestoreResultMsg)
	if !ok {
		t.Fatalf("session restore returned %T", result)
	}
	if len(msg.Skipped) != 1 || msg.Skipped[0].FilePath != missing.FilePath {
		t.Fatalf("skipped tabs = %+v, want the missing tab reported", msg.Skipped)
	}

	updated, _ := m.Update(result)
	m = updated.(Model)
	if len(m.sessionUnrestoredTabs) != 1 {
		t.Fatalf("sessionUnrestoredTabs = %+v, want the skipped tab retained", m.sessionUnrestoredTabs)
	}

	// The next session snapshot must keep the unrestorable tab instead of
	// silently rewriting it out of the session file.
	state, okSnap := m.sessionSnapshot()
	if !okSnap {
		t.Fatal("session snapshot disabled")
	}
	found := false
	for _, tab := range state.Tabs {
		if tab.FilePath == missing.FilePath && tab.CursorLine == 3 && tab.Pinned {
			found = true
		}
	}
	if !found {
		t.Fatalf("snapshot tabs = %+v, want the skipped tab preserved", state.Tabs)
	}
}

func TestSessionRestoreSnapshotDropsSkippedTabOnceReopened(t *testing.T) {
	root := t.TempDir()
	reopened := filepath.Join(root, "back.go")
	if err := os.WriteFile(reopened, []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()
	m.sessionUnrestoredTabs = []session.TabState{{FilePath: reopened}}
	m.editors = nil
	m.tabBar.Tabs = nil

	opened, _ := m.openFilePinned(reopened)
	m = opened.(Model)

	state, okSnap := m.sessionSnapshot()
	if !okSnap {
		t.Fatal("session snapshot disabled")
	}
	count := 0
	for _, tab := range state.Tabs {
		if tab.FilePath == reopened {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("snapshot contains the reopened path %d times, want exactly 1", count)
	}
}

func TestApplySessionRestoreSurfacesLoadFailure(t *testing.T) {
	cfg := config.DefaultConfig()
	m, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	m.applySessionRestore(sessionRestoreResultMsg{Generation: m.sessionRestoreGeneration, Err: errors.New("session.json: corrupt")})
	if m.status == "" {
		t.Fatal("failed session restore did not surface a status message")
	}
}

func TestSessionRestoreMapsSavedWorkspaceAliasToCurrentRootSpelling(t *testing.T) {
	physicalRoot := t.TempDir()
	currentRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(physicalRoot, currentRoot); err != nil {
		t.Fatal(err)
	}
	physicalPath := filepath.Join(physicalRoot, "kept.go")
	if err := os.WriteFile(physicalPath, []byte("package p"), 0o600); err != nil {
		t.Fatal(err)
	}

	oldLoad := loadSessionForRestore
	loadSessionForRestore = func(context.Context, string) (session.State, error) {
		return session.State{RootDir: physicalRoot, ActiveTab: 0, Tabs: []session.TabState{{FilePath: physicalPath}}}, nil
	}
	defer func() { loadSessionForRestore = oldLoad }()

	result, ok := sessionRestoreCmd(context.Background(), 1, currentRoot)().(sessionRestoreResultMsg)
	if !ok {
		t.Fatal("session restore returned unexpected message")
	}
	defer releaseRestoredSessionFiles(result.Files)
	if result.Err != nil || len(result.State.Tabs) != 1 || len(result.Files) != 1 {
		t.Fatalf("restore result = %+v / %+v", result.State, result.Files)
	}
	wantPath := filepath.Join(currentRoot, "kept.go")
	if got := result.State.Tabs[0].FilePath; got != wantPath {
		t.Fatalf("display path = %q, want current root spelling %q", got, wantPath)
	}
	if got := result.Files[0].Snapshot; got == nil || got.String() != "package p" {
		t.Fatalf("loaded snapshot = %v, want package p", got)
	}
}

func TestApplySessionRestoreUsesPinnedBytesAndReleasesStalePayload(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "kept.go")
	if err := os.WriteFile(path, []byte("disk content"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()
	pinnedSnapshot := text.NewFromString("pinned content")
	payload := []restoredSessionFile{{Tab: session.TabState{FilePath: path}, Snapshot: pinnedSnapshot}}
	cmd := m.applySessionRestore(sessionRestoreResultMsg{
		Generation: m.sessionRestoreGeneration,
		State:      session.State{RootDir: root, ActiveTab: 0, Tabs: []session.TabState{{FilePath: path}}},
		Files:      payload,
	})
	if payload[0].Snapshot != nil {
		t.Fatal("accepted restore retained duplicate snapshot ownership")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	msg := cmd()
	loaded, ok := msg.(FileLoadedMsg)
	if !ok {
		t.Fatalf("restore command returned %T", msg)
	}
	if loaded.Snapshot == nil || loaded.Snapshot.String() != "pinned content" {
		t.Fatalf("restore reopened path or prepared wrong snapshot: %v", loaded.Snapshot)
	}
	if loaded.Snapshot != pinnedSnapshot {
		t.Fatal("restore rebuilt a rope that was already prepared off the UI goroutine")
	}
	if loaded.Data != nil {
		t.Fatalf("restore retained %d raw bytes after preparing snapshot", len(loaded.Data))
	}

	stale := []restoredSessionFile{{Tab: session.TabState{FilePath: path}, Snapshot: text.NewFromString("discard me")}}
	m.applySessionRestore(sessionRestoreResultMsg{Generation: 0, Files: stale})
	if stale[0].Snapshot != nil {
		t.Fatal("stale restore retained snapshot")
	}
}

func TestNewModelDoesNotSynchronouslyLoadSession(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	oldLoad := loadSessionForRestore
	loadSessionForRestore = func(ctx context.Context, _ string) (session.State, error) {
		close(entered)
		select {
		case <-release:
			return session.State{}, nil
		case <-ctx.Done():
			return session.State{}, ctx.Err()
		}
	}
	defer func() { loadSessionForRestore = oldLoad }()

	cfg := config.DefaultConfig()
	start := time.Now()
	m, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("NewModel blocked on session load for %s", elapsed)
	}
	if len(entered) != 0 {
		t.Fatal("NewModel called the session loader synchronously")
	}
	go func() { _ = sessionRestoreCmd(m.sessionRestoreCtx, m.sessionRestoreGeneration, m.rootDir)() }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("session restore command never started")
	}
	m.cancelSessionRestore()
	close(release)
}

func TestSessionRestoreCommandStopsAfterSlowPinnedReadIsCanceled(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "slow.go")
	if err := os.WriteFile(path, []byte("package p"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldLoad, oldRead := loadSessionForRestore, readSessionRestoreFile
	loadSessionForRestore = func(context.Context, string) (session.State, error) {
		return session.State{RootDir: root, ActiveTab: 0, Tabs: []session.TabState{{FilePath: path}}}, nil
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	readSessionRestoreFile = func(ctx context.Context, root *os.Root, name string, limit int64) ([]byte, os.FileInfo, error) {
		close(entered)
		<-release
		return readSessionRestoreFileFromRoot(ctx, root, name, limit)
	}
	defer func() {
		loadSessionForRestore = oldLoad
		readSessionRestoreFile = oldRead
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultCh := make(chan tea.Msg, 1)
	go func() { resultCh <- sessionRestoreCmd(ctx, 7, root)() }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("slow pinned read did not start")
	}
	cancel()
	close(release)
	result := (<-resultCh).(sessionRestoreResultMsg)
	if result.Err == nil || !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("restore result error = %v, want context.Canceled", result.Err)
	}
}

func TestReadSessionRestoreFilesLimitsAggregateBytesAndTabCount(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"first.go":  "one",
		"second.go": "tw",
		"third.go":  "z",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pinned, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pinned.Close() }()
	state := session.State{RootDir: root, ActiveTab: 1, Tabs: []session.TabState{
		{FilePath: filepath.Join(root, "first.go")},
		{FilePath: filepath.Join(root, "second.go")},
		{FilePath: filepath.Join(root, "third.go")},
	}}
	filtered, files, _, err := readSessionRestoreFiles(context.Background(), pinned, root, state, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseRestoredSessionFiles(files)
	if len(filtered.Tabs) != 2 || len(files) != 2 {
		t.Fatalf("restored tabs/files = %d/%d, want 2/2", len(filtered.Tabs), len(files))
	}
	if filtered.ActiveTab != 1 {
		t.Fatalf("active tab = %d, want 1", filtered.ActiveTab)
	}
	var total int
	for _, file := range files {
		if file.Snapshot == nil {
			t.Fatal("restore retained an unprepared file payload")
		}
		total += file.Snapshot.Len()
	}
	if total != 5 {
		t.Fatalf("restored bytes = %d, want aggregate budget 5", total)
	}

	var tabs []session.TabState
	for index := 0; index <= maxStartupSessionTabs; index++ {
		name := filepath.Join(root, fmt.Sprintf("empty-%03d.go", index))
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		tabs = append(tabs, session.TabState{FilePath: name})
	}
	filtered, files, _, err = readSessionRestoreFiles(context.Background(), pinned, root, session.State{RootDir: root, Tabs: tabs}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseRestoredSessionFiles(files)
	if len(filtered.Tabs) != maxStartupSessionTabs || len(files) != maxStartupSessionTabs {
		t.Fatalf("restored tabs/files = %d/%d, want %d", len(filtered.Tabs), len(files), maxStartupSessionTabs)
	}
}

func TestPinnedSessionRestoreRejectsSymlinkEscapingWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.go")
	if err := os.WriteFile(outsideFile, []byte("package outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.go")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Fatal(err)
	}

	filtered, files := readPinnedSessionRestoreForTest(t, root, session.State{RootDir: root,
		ActiveTab: 0,
		Tabs:      []session.TabState{{FilePath: link}},
	})
	defer releaseRestoredSessionFiles(files)
	if len(filtered.Tabs) != 0 {
		t.Fatalf("escaped symlink was restored: %+v", filtered.Tabs)
	}
}

func TestPinnedSessionRestoreRejectsEscapingParentSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.go")
	if err := os.WriteFile(outsideFile, []byte("package outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "generated")); err != nil {
		t.Fatal(err)
	}

	filtered, files := readPinnedSessionRestoreForTest(t, root, session.State{RootDir: root,
		ActiveTab: 0,
		Tabs:      []session.TabState{{FilePath: filepath.Join(root, "generated", "outside.go")}},
	})
	defer releaseRestoredSessionFiles(files)
	if len(filtered.Tabs) != 0 {
		t.Fatalf("escaped parent symlink was restored: %+v", filtered.Tabs)
	}
}

func TestPinnedSessionRestoreReadsInternalSymlinkWithinWorkspace(t *testing.T) {
	root := t.TempDir()
	realPath := filepath.Join(root, "real.go")
	if err := os.WriteFile(realPath, []byte("package p"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.go")
	if err := os.Symlink(filepath.Base(realPath), link); err != nil {
		t.Fatal(err)
	}

	filtered, files := readPinnedSessionRestoreForTest(t, root, session.State{RootDir: root,
		ActiveTab: 0,
		Tabs:      []session.TabState{{FilePath: link}},
	})
	defer releaseRestoredSessionFiles(files)
	if len(filtered.Tabs) != 1 {
		t.Fatalf("internal symlink was not restored: %+v", filtered.Tabs)
	}
	if got := files[0].Snapshot; got == nil || got.String() != "package p" {
		t.Fatalf("symlink snapshot = %v", got)
	}
}

func TestPinnedSessionRestoreDeduplicatesInternalSymlinkIdentity(t *testing.T) {
	root := t.TempDir()
	realPath := filepath.Join(root, "real.go")
	if err := os.WriteFile(realPath, []byte("package p"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.go")
	if err := os.Symlink(filepath.Base(realPath), link); err != nil {
		t.Fatal(err)
	}

	filtered, files := readPinnedSessionRestoreForTest(t, root, session.State{RootDir: root,
		ActiveTab: 1,
		Tabs: []session.TabState{
			{FilePath: link},
			{FilePath: realPath},
		},
	})
	defer releaseRestoredSessionFiles(files)
	if len(filtered.Tabs) != 1 || len(files) != 1 {
		t.Fatalf("identity duplicate was restored: %+v / %+v", filtered.Tabs, files)
	}
	if filtered.ActiveTab != 0 {
		t.Fatalf("active canonical duplicate mapped to %d, want 0", filtered.ActiveTab)
	}
}

func TestSameSessionWorkspaceUsesPhysicalRootIdentity(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	pinned, err := os.OpenRoot(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pinned.Close() }()
	info, err := pinned.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	if !sessionWorkspaceMatchesRoot(root, info) {
		t.Fatalf("same physical workspace roots were treated as different: %q, %q", root, alias)
	}
}

func readPinnedSessionRestoreForTest(t *testing.T, root string, state session.State) (session.State, []restoredSessionFile) {
	t.Helper()
	pinned, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pinned.Close() }()
	filtered, files, _, err := readSessionRestoreFiles(context.Background(), pinned, root, state, maxStartupSessionBytes)
	if err != nil {
		t.Fatal(err)
	}
	return filtered, files
}
