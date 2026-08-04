package text

import (
	"bytes"
	"io"
)

// LineEnding is the newline convention a document was loaded with. Buffers
// always hold normalized LF content; the ending is remembered so a save
// restores the file's original convention instead of silently converting it.
type LineEnding int

const (
	// LF is the default Unix newline convention.
	LF LineEnding = iota
	// CRLF is the Windows newline convention.
	CRLF
)

var crlfPair = []byte("\r\n")

// DetectLineEnding reports CRLF when data contains at least one CRLF pair.
// Lone CR bytes (legacy Mac files, embedded control characters) do not count.
func DetectLineEnding(data []byte) LineEnding {
	if bytes.Contains(data, crlfPair) {
		return CRLF
	}
	return LF
}

// NormalizeLineEndings strips the CR from every CRLF pair so buffer content is
// uniformly LF-terminated, and reports the detected ending. Lone CR bytes are
// preserved: only CR immediately followed by LF is a line ending here. The
// input slice must not be mutated by callers afterwards when the returned
// slice aliases it.
func NormalizeLineEndings(data []byte) ([]byte, LineEnding) {
	if !bytes.Contains(data, crlfPair) {
		return data, LF
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		if data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n' {
			i++ // drop the CR, keep the LF on the next iteration
		}
		out = append(out, data[i])
	}
	return out, CRLF
}

// crlfWriter adapts an io.Writer so every LF is emitted as CRLF. It lets a
// save restore Windows line endings while streaming an immutable LF rope,
// without materializing a second document-sized copy.
type crlfWriter struct {
	w   io.Writer
	tmp []byte
}

func newCRLFWriter(w io.Writer) *crlfWriter {
	return &crlfWriter{w: w}
}

func (c *crlfWriter) Write(p []byte) (int, error) {
	consumed := len(p)
	for len(p) > 0 {
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			if _, err := c.w.Write(p); err != nil {
				return consumed, err
			}
			return consumed, nil
		}
		if idx > 0 {
			if _, err := c.w.Write(p[:idx]); err != nil {
				return consumed, err
			}
		}
		c.tmp = append(c.tmp[:0], '\r', '\n')
		if _, err := c.w.Write(c.tmp); err != nil {
			return consumed, err
		}
		p = p[idx+1:]
	}
	return consumed, nil
}
