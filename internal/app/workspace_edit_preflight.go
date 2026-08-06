package app

import (
	"context"
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

type preparedTextEdit struct {
	start   int
	end     int
	newText string
}

// prepareFormattingTextEdits applies the same strict range rules as workspace
// edits, with a single-document budget suitable for an untrusted response.
func prepareFormattingTextEdits(ctx context.Context, rope *text.Rope, edits []lsp.TextEdit) ([]preparedTextEdit, error) {
	if len(edits) > maxWorkspaceTextEdits {
		return nil, fmt.Errorf("formatting result exceeds %d text edits", maxWorkspaceTextEdits)
	}

	newBytes := 0
	for i, edit := range edits {
		if i&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if len(edit.NewText) > maxWorkspaceNewTextBytes-newBytes {
			return nil, fmt.Errorf("formatting result exceeds %d bytes of replacement text", maxWorkspaceNewTextBytes)
		}
		newBytes += len(edit.NewText)
	}
	return prepareTextEditRanges(ctx, rope, edits)
}

// prepareTextEditRanges verifies byte-oriented LSP UTF-8 coordinates and
// returns one sorted representation shared by workspace edits and formatting.
func prepareTextEditRanges(ctx context.Context, rope *text.Rope, edits []lsp.TextEdit) ([]preparedTextEdit, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lineCacheCapacity := min(len(edits), maxWorkspaceTextEdits)
	validator := strictTextEditPositionValidator{
		rope:      rope,
		lineCount: rope.LineCount(),
		lines:     make(map[int]strictTextEditLine, lineCacheCapacity),
	}
	ranges := make([]preparedTextEdit, 0, len(edits))
	for i, edit := range edits {
		if i&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		start, err := validator.offset(edit.StartLine, edit.StartCol)
		if err != nil {
			return nil, fmt.Errorf("invalid edit start: %w", err)
		}
		end, err := validator.offset(edit.EndLine, edit.EndCol)
		if err != nil {
			return nil, fmt.Errorf("invalid edit end: %w", err)
		}
		if end < start {
			return nil, fmt.Errorf("edit range ends before it starts")
		}
		ranges = append(ranges, preparedTextEdit{start: start, end: end, newText: edit.NewText})
	}

	slices.SortFunc(ranges, func(a, b preparedTextEdit) int {
		if a.start != b.start {
			return a.start - b.start
		}
		return a.end - b.end
	})
	for i := 1; i < len(ranges); i++ {
		if ranges[i].start < ranges[i-1].end ||
			(ranges[i].start == ranges[i-1].start && ranges[i].end == ranges[i-1].end) {
			return nil, fmt.Errorf("overlapping or ambiguous edits")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ranges, nil
}

type strictTextEditLine struct {
	start     int
	length    int
	validUTF8 bool
}

type strictTextEditPositionValidator struct {
	rope      *text.Rope
	lineCount int
	lines     map[int]strictTextEditLine
}

func (v *strictTextEditPositionValidator) offset(line, col int) (int, error) {
	if line < 0 || line >= v.lineCount {
		return 0, fmt.Errorf("line %d is outside document", line)
	}
	lineInfo, ok := v.lines[line]
	if !ok {
		lineInfo.start = v.rope.LineStart(line)
		lineEnd := v.rope.Len()
		if line < v.lineCount-1 {
			lineEnd = v.rope.LineStart(line+1) - 1
		}
		lineInfo.length = lineEnd - lineInfo.start
		lineInfo.validUTF8 = v.rope.ValidUTF8Range(lineInfo.start, lineEnd)
		v.lines[line] = lineInfo
	}
	if col < 0 || col > lineInfo.length {
		return 0, fmt.Errorf("column %d is outside line %d", col, line)
	}
	if !lineInfo.validUTF8 {
		return 0, fmt.Errorf("line %d is not valid UTF-8", line)
	}
	offset := lineInfo.start + col
	if col < lineInfo.length {
		value, _ := v.rope.ByteAtSafe(offset)
		if !utf8.RuneStart(value) {
			return 0, fmt.Errorf("column %d splits a UTF-8 sequence", col)
		}
	}
	return offset, nil
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
