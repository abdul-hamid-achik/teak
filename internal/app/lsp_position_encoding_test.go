package app

import (
	"path/filepath"
	"strings"
	"testing"

	"teak/internal/text"
)

func TestResolvePendingLSPPositionUTF16AfterAsyncLoad(t *testing.T) {
	got, err := resolvePendingLSPPosition("a😀b", text.Position{Line: 0, Col: 3}, "utf-16")
	if err != nil {
		t.Fatalf("resolvePendingLSPPosition() error = %v", err)
	}
	if want := (text.Position{Line: 0, Col: 5}); got != want {
		t.Fatalf("resolvePendingLSPPosition() = %#v, want %#v", got, want)
	}
}

func TestResolvePendingLSPPositionRejectsSplitSurrogate(t *testing.T) {
	_, err := resolvePendingLSPPosition("a😀b", text.Position{Line: 0, Col: 2}, "utf-16")
	if err == nil {
		t.Fatal("resolvePendingLSPPosition() succeeded for a split UTF-16 surrogate pair")
	}
}

func TestResolvePendingLSPPositionFromRopeReadsOnlyTargetLine(t *testing.T) {
	rope := text.NewFromString(strings.Repeat("short\n", 100_000) + "a😀b\n")
	got, err := resolvePendingLSPPositionRope(
		rope,
		text.Position{Line: 100_000, Col: 3},
		"utf-16",
	)
	if err != nil {
		t.Fatalf("resolvePendingLSPPositionRope() error = %v", err)
	}
	if want := (text.Position{Line: 100_000, Col: 5}); got != want {
		t.Fatalf("resolvePendingLSPPositionRope() = %#v, want %#v", got, want)
	}
}

func TestFileLoadConvertsDeferredUTF16NavigationForUnopenedFile(t *testing.T) {
	model := newInputRoutingTestModel(t)
	path := filepath.Join(model.rootDir, "target.go")
	// The location came from a UTF-16 server before target.go was opened:
	// character 3 is immediately after the astral rune in a😀b.
	model.setPendingLSPCursor(path, text.Position{Line: 0, Col: 3}, "utf-16")

	opened, _ := model.openFilePinned(path)
	model = opened.(Model)
	request := model.latestFileLoadRequest()

	updated, _ := model.handleFileLoaded(FileLoadedMsg{
		Path:      path,
		Snapshot:  text.NewFromString("a😀b"),
		EditorID:  request.EditorID,
		RequestID: request.ID,
	})
	model = updated.(Model)
	if got := model.activeEditor().Buffer.Cursor; got != (text.Position{Line: 0, Col: 5}) {
		t.Fatalf("deferred UTF-16 cursor = %#v, want byte column 5", got)
	}
}

func TestFileLoadRejectsInvalidDeferredUTF16NavigationSafely(t *testing.T) {
	model := newInputRoutingTestModel(t)
	path := filepath.Join(model.rootDir, "invalid-target.go")
	// Character 2 splits the UTF-16 surrogate pair for 😀.
	model.setPendingLSPCursor(path, text.Position{Line: 0, Col: 2}, "utf-16")

	opened, _ := model.openFilePinned(path)
	model = opened.(Model)
	request := model.latestFileLoadRequest()
	updated, _ := model.handleFileLoaded(FileLoadedMsg{
		Path:      path,
		Snapshot:  text.NewFromString("a😀b"),
		EditorID:  request.EditorID,
		RequestID: request.ID,
	})
	model = updated.(Model)
	if got := model.activeEditor().Buffer.Cursor; got != (text.Position{}) {
		t.Fatalf("invalid deferred cursor mutated buffer to %#v", got)
	}
	if !strings.Contains(model.status, "LSP navigation position rejected") {
		t.Fatalf("status = %q, want safe navigation rejection", model.status)
	}
}
