package lsp

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// positionEncoding is the LSP character-unit encoding negotiated during
// initialize. Teak keeps its own columns as UTF-8 byte offsets, so every
// protocol boundary must convert through the relevant document snapshot.
type positionEncoding string

const (
	positionEncodingUTF8  positionEncoding = "utf-8"
	positionEncodingUTF16 positionEncoding = "utf-16"
	positionEncodingUTF32 positionEncoding = "utf-32"
)

// validateServerPositionEncoding normalizes a server choice. LSP 3.17 defines
// UTF-16 as the default when the server omits positionEncoding.
func validateServerPositionEncoding(encoding string) (positionEncoding, error) {
	switch positionEncoding(strings.ToLower(strings.TrimSpace(encoding))) {
	case "":
		return positionEncodingUTF16, nil
	case positionEncodingUTF8, positionEncodingUTF16, positionEncodingUTF32:
		return positionEncoding(strings.ToLower(strings.TrimSpace(encoding))), nil
	default:
		return "", fmt.Errorf("language server selected unsupported position encoding %q", encoding)
	}
}

// PositionFromProtocol converts a server position after its target document has
// been loaded by the application. It is used for deferred navigation to an
// unopened UTF-16/UTF-32 location, never on the JSON-RPC reader.
func PositionFromProtocol(content, encoding string, position Position) (Position, error) {
	negotiated, err := validateServerPositionEncoding(encoding)
	if err != nil {
		return Position{}, err
	}
	return positionFromProtocol(content, negotiated, position)
}

// PositionFromProtocolLine converts one protocol character offset without
// indexing or materializing the rest of a document. The input excludes LF in
// the same way text.Rope.Line does; a trailing CR from CRLF is treated as a
// terminator rather than editable content.
func PositionFromProtocolLine(line []byte, encoding string, character int) (int, error) {
	negotiated, err := validateServerPositionEncoding(encoding)
	if err != nil {
		return 0, err
	}
	if !utf8.Valid(line) {
		return 0, fmt.Errorf("document line is not valid UTF-8")
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return decodedColumnBytes(line, negotiated, character)
}

// documentSnapshot is an immutable copy of the text against which a language
// server calculated positions. It is stored by value behind Client.mu; strings
// are immutable, so callers can use the returned copy without holding a lock.
type documentSnapshot struct {
	version         int
	content         string
	lineCount       int
	lineCheckpoints []int
}

// retainedBytes is a conservative, architecture-independent accounting of
// memory retained by a snapshot. The eight-byte checkpoint charge keeps the
// global budget deterministic on every supported target (and is exact on the
// 64-bit platforms Teak ships today).
func (snapshot documentSnapshot) retainedBytes() int {
	return len(snapshot.content) + len(snapshot.lineCheckpoints)*8
}

// A dense []int line index can consume hundreds of MiB for generated files
// made of very short lines. Checkpoint every 256 lines instead: lookups scan
// at most 255 line terminators plus the requested line itself.
const positionLineCheckpointStride = 256

// newDocumentSnapshot validates one immutable LSP document image and indexes
// its lines once, at DidOpen/DidChange time. Position conversion is a hot path
// (completion, hover, cursor-triggered requests), so it must never scan or
// allocate for the entire document.
func newDocumentSnapshot(version int, content string) (documentSnapshot, error) {
	if !utf8.ValidString(content) {
		return documentSnapshot{}, fmt.Errorf("document is not valid UTF-8")
	}
	lineCount := 1
	checkpoints := make([]int, 1, 1+strings.Count(content, "\n")/positionLineCheckpointStride)
	checkpoints[0] = 0
	for offset := 0; offset < len(content); offset++ {
		if content[offset] == '\n' {
			lineCount++
			if (lineCount-1)%positionLineCheckpointStride == 0 {
				checkpoints = append(checkpoints, offset+1)
			}
		}
	}
	return documentSnapshot{
		version:         version,
		content:         content,
		lineCount:       lineCount,
		lineCheckpoints: checkpoints,
	}, nil
}

// positionToProtocol converts a 0-based Teak byte column to an LSP character
// offset. A final CR in a CRLF line is a line terminator, not a document
// character: the editor's position immediately after it maps to the LSP end
// of line.
func positionToProtocol(content string, encoding positionEncoding, pos Position) (Position, error) {
	snapshot, err := newDocumentSnapshot(0, content)
	if err != nil {
		return Position{}, err
	}
	return positionToProtocolSnapshot(snapshot, encoding, pos)
}

func positionToProtocolSnapshot(snapshot documentSnapshot, encoding positionEncoding, pos Position) (Position, error) {
	line, err := snapshot.line(pos.Line)
	if err != nil {
		return Position{}, fmt.Errorf("line %d is outside document", pos.Line)
	}
	if pos.Character < 0 || pos.Character > len(line.raw) {
		return Position{}, fmt.Errorf("column %d is outside line %d", pos.Character, pos.Line)
	}

	byteColumn := pos.Character
	if byteColumn > len(line.content) {
		// The only extra byte allowed here is the CR from a CRLF terminator.
		byteColumn = len(line.content)
	}
	if byteColumn < len(line.content) && !utf8.RuneStart(line.content[byteColumn]) {
		return Position{}, fmt.Errorf("column %d splits a UTF-8 sequence on line %d", pos.Character, pos.Line)
	}

	character, err := encodedColumn(line.content[:byteColumn], encoding)
	if err != nil {
		return Position{}, err
	}
	return Position{Line: pos.Line, Character: character}, nil
}

// positionFromProtocol converts a 0-based LSP character offset to Teak's
// byte-oriented column. It rejects positions in the middle of a UTF-8 rune or
// UTF-16 surrogate pair instead of silently rounding them into an unrelated
// edit location.
func positionFromProtocol(content string, encoding positionEncoding, pos Position) (Position, error) {
	snapshot, err := newDocumentSnapshot(0, content)
	if err != nil {
		return Position{}, err
	}
	return positionFromProtocolSnapshot(snapshot, encoding, pos)
}

func positionFromProtocolSnapshot(snapshot documentSnapshot, encoding positionEncoding, pos Position) (Position, error) {
	line, err := snapshot.line(pos.Line)
	if err != nil {
		return Position{}, fmt.Errorf("line %d is outside document", pos.Line)
	}
	if pos.Character < 0 {
		return Position{}, fmt.Errorf("column %d is outside line %d", pos.Character, pos.Line)
	}

	byteColumn, err := decodedColumn(line.content, encoding, pos.Character)
	if err != nil {
		return Position{}, fmt.Errorf("line %d: %w", pos.Line, err)
	}
	return Position{Line: pos.Line, Character: byteColumn}, nil
}

type documentLine struct {
	start   int
	raw     string
	content string
}

func (snapshot documentSnapshot) line(line int) (documentLine, error) {
	if line < 0 || line >= snapshot.lineCount {
		return documentLine{}, fmt.Errorf("line %d is outside document", line)
	}
	checkpoint := line / positionLineCheckpointStride
	if checkpoint < 0 || checkpoint >= len(snapshot.lineCheckpoints) {
		return documentLine{}, fmt.Errorf("line index is inconsistent")
	}
	start := snapshot.lineCheckpoints[checkpoint]
	if start < 0 || start > len(snapshot.content) {
		return documentLine{}, fmt.Errorf("line index is inconsistent")
	}
	checkpointLine := checkpoint * positionLineCheckpointStride
	for current := checkpointLine; current < line; current++ {
		relative := strings.IndexByte(snapshot.content[start:], '\n')
		if relative < 0 {
			return documentLine{}, fmt.Errorf("line index is inconsistent")
		}
		start += relative + 1
	}
	end := len(snapshot.content)
	if relative := strings.IndexByte(snapshot.content[start:], '\n'); relative >= 0 {
		end = start + relative // exclude LF
	}
	raw := snapshot.content[start:end]
	value := documentLine{start: start, raw: raw, content: raw}
	if len(raw) > 0 && raw[len(raw)-1] == '\r' {
		value.content = raw[:len(raw)-1]
	}
	return value, nil
}

func encodedColumn(prefix string, encoding positionEncoding) (int, error) {
	switch encoding {
	case positionEncodingUTF8:
		return len(prefix), nil
	case positionEncodingUTF16, positionEncodingUTF32:
		units := 0
		for len(prefix) > 0 {
			r, size := utf8.DecodeRuneInString(prefix)
			if r == utf8.RuneError && size == 1 {
				return 0, fmt.Errorf("document is not valid UTF-8")
			}
			if encoding == positionEncodingUTF16 && r > 0xFFFF {
				units += 2
			} else {
				units++
			}
			prefix = prefix[size:]
		}
		return units, nil
	default:
		return 0, fmt.Errorf("unsupported position encoding %q", encoding)
	}
}

func decodedColumn(line string, encoding positionEncoding, character int) (int, error) {
	if encoding != positionEncodingUTF8 && encoding != positionEncodingUTF16 && encoding != positionEncodingUTF32 {
		return 0, fmt.Errorf("unsupported position encoding %q", encoding)
	}
	if character == 0 {
		return 0, nil
	}

	units := 0
	for byteColumn := 0; byteColumn < len(line); {
		r, size := utf8.DecodeRuneInString(line[byteColumn:])
		if r == utf8.RuneError && size == 1 {
			return 0, fmt.Errorf("document is not valid UTF-8")
		}
		width := 1
		if encoding == positionEncodingUTF8 {
			width = size
		} else if encoding == positionEncodingUTF16 && r > 0xFFFF {
			width = 2
		}
		if character == units {
			return byteColumn, nil
		}
		if character > units && character < units+width {
			return 0, fmt.Errorf("column %d splits a character encoding unit", character)
		}
		units += width
		byteColumn += size
	}
	if character == units {
		return len(line), nil
	}
	return 0, fmt.Errorf("column %d is outside line", character)
}

func decodedColumnBytes(line []byte, encoding positionEncoding, character int) (int, error) {
	if encoding != positionEncodingUTF8 && encoding != positionEncodingUTF16 && encoding != positionEncodingUTF32 {
		return 0, fmt.Errorf("unsupported position encoding %q", encoding)
	}
	if character < 0 {
		return 0, fmt.Errorf("column %d is outside line", character)
	}
	if character == 0 {
		return 0, nil
	}

	units := 0
	for byteColumn := 0; byteColumn < len(line); {
		r, size := utf8.DecodeRune(line[byteColumn:])
		if r == utf8.RuneError && size == 1 {
			return 0, fmt.Errorf("document is not valid UTF-8")
		}
		width := 1
		if encoding == positionEncodingUTF8 {
			width = size
		} else if encoding == positionEncodingUTF16 && r > 0xFFFF {
			width = 2
		}
		if character == units {
			return byteColumn, nil
		}
		if character > units && character < units+width {
			return 0, fmt.Errorf("column %d splits a character encoding unit", character)
		}
		units += width
		byteColumn += size
	}
	if character == units {
		return len(line), nil
	}
	return 0, fmt.Errorf("column %d is outside line", character)
}

func replaceInternalRangeSnapshot(snapshot documentSnapshot, start, end Position, replacement string) (string, error) {
	startOffset, err := internalPositionOffsetSnapshot(snapshot, start)
	if err != nil {
		return "", fmt.Errorf("invalid start: %w", err)
	}
	endOffset, err := internalPositionOffsetSnapshot(snapshot, end)
	if err != nil {
		return "", fmt.Errorf("invalid end: %w", err)
	}
	if endOffset < startOffset {
		return "", fmt.Errorf("range ends before it starts")
	}
	return snapshot.content[:startOffset] + replacement + snapshot.content[endOffset:], nil
}

func internalPositionOffsetSnapshot(snapshot documentSnapshot, pos Position) (int, error) {
	line, err := snapshot.line(pos.Line)
	if err != nil {
		return 0, fmt.Errorf("line %d is outside document", pos.Line)
	}
	if pos.Character < 0 || pos.Character > len(line.raw) {
		return 0, fmt.Errorf("column %d is outside line %d", pos.Character, pos.Line)
	}
	if pos.Character < len(line.raw) && !utf8.RuneStart(line.raw[pos.Character]) {
		return 0, fmt.Errorf("column %d splits a UTF-8 sequence on line %d", pos.Character, pos.Line)
	}
	return line.start + pos.Character, nil
}
