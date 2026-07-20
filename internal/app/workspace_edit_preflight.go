package app

import (
	"fmt"
	"os"
	"slices"
	"unicode/utf8"

	"teak/internal/lsp"
	"teak/internal/text"
)

const (
	maxWorkspaceEditDocuments = 1024
	maxWorkspaceTextEdits     = 16_384
	maxWorkspaceNewTextBytes  = 16 << 20
)

type workspaceEditBudget struct {
	documents int
	edits     int
	newBytes  int
}

// validateFormattingTextEdits applies the same strict range rules as
// workspace edits, with a single-document budget suitable for an untrusted
// formatting response. Validation must complete before the live buffer is
// changed because Buffer.ReplaceRange deliberately clamps user-facing cursor
// positions for interactive editing.
func validateFormattingTextEdits(buf *text.Buffer, edits []lsp.TextEdit) error {
	if len(edits) > maxWorkspaceTextEdits {
		return fmt.Errorf("formatting result exceeds %d text edits", maxWorkspaceTextEdits)
	}

	newBytes := 0
	for _, edit := range edits {
		if len(edit.NewText) > maxWorkspaceNewTextBytes-newBytes {
			return fmt.Errorf("formatting result exceeds %d bytes of replacement text", maxWorkspaceNewTextBytes)
		}
		newBytes += len(edit.NewText)
	}
	return validateTextEditRanges(buf, edits)
}

// validateTextEditRanges verifies positions against byte-oriented LSP UTF-8
// coordinates. It is shared by workspace edits and formatting so every
// server-originated edit has identical range and overlap semantics.
func validateTextEditRanges(buf *text.Buffer, edits []lsp.TextEdit) error {
	type editRange struct {
		start int
		end   int
	}
	ranges := make([]editRange, 0, len(edits))
	rope := buf.Rope()
	for _, edit := range edits {
		start, err := strictTextEditPositionOffset(rope, edit.StartLine, edit.StartCol)
		if err != nil {
			return fmt.Errorf("invalid edit start: %w", err)
		}
		end, err := strictTextEditPositionOffset(rope, edit.EndLine, edit.EndCol)
		if err != nil {
			return fmt.Errorf("invalid edit end: %w", err)
		}
		if end < start {
			return fmt.Errorf("edit range ends before it starts")
		}
		ranges = append(ranges, editRange{start: start, end: end})
	}

	slices.SortFunc(ranges, func(a, b editRange) int {
		if a.start != b.start {
			return a.start - b.start
		}
		return a.end - b.end
	})
	for i := 1; i < len(ranges); i++ {
		if ranges[i].start < ranges[i-1].end ||
			(ranges[i].start == ranges[i-1].start && ranges[i].end == ranges[i-1].end) {
			return fmt.Errorf("overlapping or ambiguous edits")
		}
	}
	return nil
}

func strictTextEditPositionOffset(rope *text.Rope, line, col int) (int, error) {
	if line < 0 || line >= rope.LineCount() {
		return 0, fmt.Errorf("line %d is outside document", line)
	}
	lineBytes := rope.Line(line)
	if col < 0 || col > len(lineBytes) {
		return 0, fmt.Errorf("column %d is outside line %d", col, line)
	}
	if !utf8.Valid(lineBytes) {
		return 0, fmt.Errorf("line %d is not valid UTF-8", line)
	}
	if col < len(lineBytes) && !utf8.RuneStart(lineBytes[col]) {
		return 0, fmt.Errorf("column %d splits a UTF-8 sequence", col)
	}
	return rope.LineStart(line) + col, nil
}

func preflightWorkspaceFileOperationAtRoot(root *os.Root, rootDir string, op lsp.WorkspaceFileOperation) error {
	switch op.Kind {
	case lsp.FileOpCreate:
		target, _, err := workspaceRelativePathForRoot(rootDir, lsp.URIToPath(op.URI))
		if err != nil {
			return err
		}
		if _, err := root.Stat(target); err == nil {
			return fmt.Errorf("create target already exists")
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect create target: %w", err)
		}
		return nil
	case lsp.FileOpDelete:
		target, _, err := workspaceRelativePathForRoot(rootDir, lsp.URIToPath(op.URI))
		if err != nil {
			return err
		}
		info, err := root.Stat(target)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("delete target is not a regular file")
		}
		return nil
	case lsp.FileOpRename:
		oldRelative, _, err := workspaceRelativePathForRoot(rootDir, lsp.URIToPath(op.OldURI))
		if err != nil {
			return err
		}
		newRelative, _, err := workspaceRelativePathForRoot(rootDir, lsp.URIToPath(op.NewURI))
		if err != nil {
			return err
		}
		info, err := root.Stat(oldRelative)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("rename source is not a regular file")
		}
		if _, err := root.Stat(newRelative); err == nil {
			return fmt.Errorf("rename target already exists")
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect rename target: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported workspace file operation %q", op.Kind)
	}
}
