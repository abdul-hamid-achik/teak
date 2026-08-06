package app

import (
	"context"
	"strings"
	"testing"

	"teak/internal/config"
	"teak/internal/lsp"
	"teak/internal/text"
)

func TestPrepareTextEditRangesRejectsInvalidUTF8Lines(t *testing.T) {
	rope := text.New([]byte{'a', 0xff, 'b'})
	tests := []struct {
		name string
		col  int
		want string
	}{
		{name: "invalid line", col: 0, want: "line 0 is not valid UTF-8"},
		{name: "bounds checked first", col: 4, want: "column 4 is outside line 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := prepareTextEditRanges(context.Background(), rope, []lsp.TextEdit{{
				StartLine: 0,
				StartCol:  tt.col,
				EndLine:   0,
				EndCol:    tt.col,
			}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("prepareTextEditRanges() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestFormatResultRejectsInvalidEditsWithoutMutatingBuffer(t *testing.T) {
	tooMany := make([]lsp.TextEdit, maxWorkspaceTextEdits+1)
	tooMuchText := strings.Repeat("x", maxWorkspaceNewTextBytes+1)
	tests := []struct {
		name  string
		edits []lsp.TextEdit
	}{
		{
			name:  "column splits utf8 rune",
			edits: []lsp.TextEdit{{StartLine: 0, StartCol: 2, EndLine: 0, EndCol: 2, NewText: "x"}},
		},
		{
			name:  "line outside document",
			edits: []lsp.TextEdit{{StartLine: 2, EndLine: 2, NewText: "x"}},
		},
		{
			name:  "column outside line",
			edits: []lsp.TextEdit{{StartLine: 0, StartCol: 4, EndLine: 0, EndCol: 4, NewText: "x"}},
		},
		{
			name:  "inverted range",
			edits: []lsp.TextEdit{{StartLine: 0, StartCol: 3, EndLine: 0, EndCol: 1, NewText: "x"}},
		},
		{
			name: "valid edit before invalid edit is still atomic",
			edits: []lsp.TextEdit{
				{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 1, NewText: "z"},
				{StartLine: 2, EndLine: 2, NewText: "x"},
			},
		},
		{
			name: "overlapping ranges",
			edits: []lsp.TextEdit{
				{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 3, NewText: "x"},
				{StartLine: 0, StartCol: 1, EndLine: 0, EndCol: 3, NewText: "y"},
			},
		},
		{
			name: "duplicate ranges",
			edits: []lsp.TextEdit{
				{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 1, NewText: "x"},
				{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 1, NewText: "y"},
			},
		},
		{name: "too many edits", edits: tooMany},
		{
			name:  "too many replacement bytes",
			edits: []lsp.TextEdit{{StartLine: 0, EndLine: 0, NewText: tooMuchText}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
			idx := addDirtyEditor(t, &model, "format.go", "aé\n", "aé\n")
			path := model.editors[idx].Buffer.FilePath
			beforeContent := model.editors[idx].Buffer.Content()
			beforeVersion := model.editors[idx].Buffer.Version()
			beforeDirty := model.editors[idx].Buffer.Dirty()

			updatedAny, cmd := model.Update(lsp.FormatResultMsg{
				FilePath:       path,
				BaseVersion:    beforeVersion,
				HasBaseVersion: true,
				Status:         lsp.FormatApplied,
				Edits:          tt.edits,
			})
			updated := updatedAny.(Model)
			if got := updated.editors[idx].Buffer.Content(); got != beforeContent {
				t.Fatalf("invalid format mutated content before preparation: got %q, want %q", got, beforeContent)
			}
			updated, cmd = completeFormatPreparation(t, updated, cmd)
			if cmd != nil {
				t.Fatal("invalid manual format must not schedule post-edit work after rejection")
			}
			if got := updated.editors[idx].Buffer.Version(); got != beforeVersion {
				t.Fatalf("invalid format changed version: got %d, want %d", got, beforeVersion)
			}
			if got := updated.editors[idx].Buffer.Dirty(); got != beforeDirty {
				t.Fatalf("invalid format changed dirty state: got %v, want %v", got, beforeDirty)
			}
			if !strings.Contains(updated.status, "invalid formatting edits") {
				t.Fatalf("status = %q, want rejection detail", updated.status)
			}
		})
	}
}

func TestFormatOnSaveInvalidEditsSavesOriginalSnapshot(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor.FormatOnSave = true
	model := newSaveFlowModel(t, cfg, t.TempDir())
	idx := addDirtyEditor(t, &model, "main.go", "before\n", "after\n")
	path := model.editors[idx].Buffer.FilePath
	beforeVersion := model.editors[idx].Buffer.Version()
	requestID := model.nextSaveID()
	model.pendingSaves[requestID] = pendingSaveRequest{TabIndex: idx, Path: path}

	updatedAny, cmd := model.Update(lsp.FormatResultMsg{
		RequestID: requestID,
		FilePath:  path,
		Status:    lsp.FormatApplied,
		Edits: []lsp.TextEdit{
			{StartLine: 0, StartCol: 99, EndLine: 0, EndCol: 99, NewText: "corrupt"},
		},
	})
	updated := updatedAny.(Model)
	updated, cmd = completeFormatPreparation(t, updated, cmd)

	if got := updated.editors[idx].Buffer.Content(); got != "after\n" {
		t.Fatalf("invalid format mutated buffered content: got %q", got)
	}
	if got := updated.editors[idx].Buffer.Version(); got != beforeVersion {
		t.Fatalf("invalid format changed buffer version: got %d, want %d", got, beforeVersion)
	}
	saved := requireFileSavedMsg(t, cmd)
	finalAny, _ := updated.Update(saved)
	final := finalAny.(Model)
	if got := final.editors[idx].Buffer.Content(); got != "after\n" {
		t.Fatalf("saved buffer = %q, want original snapshot", got)
	}
	if !strings.Contains(final.status, "formatting failed") {
		t.Fatalf("status = %q, want formatting failure note", final.status)
	}
}
