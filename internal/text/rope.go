package text

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
)

const maxLeaf = 512

// A line index makes small and medium documents very fast to navigate, but
// costs one machine word per line. Keep it bounded so opening a generated
// file with tens of millions of empty lines cannot reserve gigabytes just to
// paint a viewport.
const maxCachedLineIndexEntries = 250_000

// Rope is a persistent (immutable) rope data structure for efficient text manipulation.
// Each mutation returns a new Rope; the original is unchanged.
type Rope struct {
	left      *Rope
	right     *Rope
	value     []byte // only set for leaf nodes
	len       int
	newlines  int
	depth     int
	lineIndex []int     // lazy cache of byte offsets for each line start
	initOnce  sync.Once // ensures lineIndex is built only once
}

// fibonacci numbers for rebalancing threshold
var fibs = func() []int {
	f := make([]int, 64)
	f[0], f[1] = 1, 2
	for i := 2; i < len(f); i++ {
		f[i] = f[i-1] + f[i-2]
	}
	return f
}()

// New creates a Rope from a byte slice.
//
// New defensively copies data. Callers may retain or mutate their slice after
// this function returns without affecting the immutable rope snapshot.
func New(data []byte) *Rope {
	if len(data) == 0 {
		return newLeaf(nil)
	}
	if len(data) <= maxLeaf {
		return newLeaf(data)
	}
	mid := len(data) / 2
	// avoid splitting in the middle of a multi-byte UTF-8 sequence
	for mid < len(data) && mid > 0 && data[mid]&0xC0 == 0x80 {
		mid++
	}
	if mid == 0 || mid >= len(data) {
		return newLeaf(data)
	}
	return join(New(data[:mid]), New(data[mid:]))
}

// NewOwned creates a Rope by taking exclusive ownership of data's backing
// storage. It does not copy the bytes into its leaves, so callers MUST NOT
// retain, mutate, or append to data after this call. Use New for ordinary
// caller-provided input. NewOwned is intended for a freshly-read file or a
// freshly-created contiguous snapshot whose ownership is being transferred
// directly into the immutable rope.
//
// As with New, this constructor preserves bytes exactly; it does not validate
// or normalize text. When the input is valid UTF-8, leaves are split only at
// rune boundaries.
func NewOwned(data []byte) *Rope {
	if len(data) == 0 {
		return newLeafOwned(nil)
	}
	// Keep leaf capacities within their logical extent. Besides making the
	// ownership boundary explicit, this prevents any future internal append
	// from writing through one leaf into its neighbour's storage.
	data = data[:len(data):len(data)]
	if len(data) <= maxLeaf {
		return newLeafOwned(data)
	}
	mid := len(data) / 2
	// Avoid splitting in the middle of a multi-byte UTF-8 sequence. This is
	// deliberately the same rule as New so both constructors have identical
	// text semantics.
	for mid < len(data) && mid > 0 && data[mid]&0xC0 == 0x80 {
		mid++
	}
	if mid == 0 || mid >= len(data) {
		return newLeafOwned(data)
	}
	return join(NewOwned(data[:mid:mid]), NewOwned(data[mid:len(data):len(data)]))
}

// NewFromString creates a Rope from a string.
func NewFromString(s string) *Rope {
	return New([]byte(s))
}

func newLeaf(data []byte) *Rope {
	b := make([]byte, len(data))
	copy(b, data)
	return newLeafOwned(b)
}

// newLeafOwned creates a leaf from storage exclusively owned by the rope.
// Callers must not retain or mutate data afterwards.
func newLeafOwned(data []byte) *Rope {
	return &Rope{
		value:    data,
		len:      len(data),
		newlines: bytes.Count(data, []byte{'\n'}),
		depth:    0,
	}
}

func join(left, right *Rope) *Rope {
	if left.len == 0 {
		return right
	}
	if right.len == 0 {
		return left
	}
	d := max(left.depth, right.depth)
	r := &Rope{
		left:     left,
		right:    right,
		len:      left.len + right.len,
		newlines: left.newlines + right.newlines,
		depth:    d + 1,
	}
	return maybeRebalance(r)
}

func maybeRebalance(r *Rope) *Rope {
	if r.depth < len(fibs) && r.len >= fibs[r.depth] {
		return r
	}
	return rebalance(r)
}

func rebalance(r *Rope) *Rope {
	leaves := collectLeaves(r, nil)
	return buildBalanced(leaves)
}

func collectLeaves(r *Rope, acc []*Rope) []*Rope {
	if r.isLeaf() {
		if r.len > 0 {
			return append(acc, r)
		}
		return acc
	}
	acc = collectLeaves(r.left, acc)
	acc = collectLeaves(r.right, acc)
	return acc
}

func buildBalanced(leaves []*Rope) *Rope {
	if len(leaves) == 0 {
		return newLeaf(nil)
	}
	if len(leaves) == 1 {
		return leaves[0]
	}
	mid := len(leaves) / 2
	left := buildBalanced(leaves[:mid])
	right := buildBalanced(leaves[mid:])
	d := max(left.depth, right.depth)
	return &Rope{
		left:     left,
		right:    right,
		len:      left.len + right.len,
		newlines: left.newlines + right.newlines,
		depth:    d + 1,
	}
}

func (r *Rope) isLeaf() bool {
	return r.left == nil && r.right == nil
}

// Len returns the total byte length.
func (r *Rope) Len() int {
	if r == nil {
		return 0
	}
	return r.len
}

// LineCount returns the number of lines (newline count + 1).
func (r *Rope) LineCount() int {
	if r == nil {
		return 1
	}
	return r.newlines + 1
}

// String returns the full text as a string.
func (r *Rope) String() string {
	return string(r.Bytes())
}

// Bytes returns the full text as a byte slice.
func (r *Rope) Bytes() []byte {
	if r == nil {
		return nil
	}
	buf := make([]byte, 0, r.len)
	r.appendTo(&buf)
	return buf
}

// BytesContext returns a copy of the rope content, stopping between leaves if
// ctx is canceled. It is intended for background work that needs a contiguous
// snapshot without making the UI goroutine copy a whole document.
func (r *Rope) BytesContext(ctx context.Context) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buf := make([]byte, 0, r.len)
	if err := r.appendToContext(ctx, &buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// EqualBytes compares a contiguous byte slice with the rope leaf-by-leaf.
// Unlike Bytes, it performs no document-sized allocation, which makes it
// suitable for background file-watcher attribution and validation.
func (r *Rope) EqualBytes(data []byte) bool {
	if r == nil {
		return len(data) == 0
	}
	if len(data) != r.len {
		return false
	}
	offset := 0
	var equal func(*Rope) bool
	equal = func(node *Rope) bool {
		if node.isLeaf() {
			end := offset + len(node.value)
			if !bytes.Equal(node.value, data[offset:end]) {
				return false
			}
			offset = end
			return true
		}
		return equal(node.left) && equal(node.right)
	}
	return equal(r) && offset == len(data)
}

// EqualReader compares a stream with the rope leaf-by-leaf without allocating
// a contiguous document. The reader must reach EOF exactly after the rope;
// callers should buffer filesystem readers because leaves are intentionally
// small.
func (r *Rope) EqualReader(reader io.Reader) (bool, error) {
	if reader == nil {
		return false, io.ErrUnexpectedEOF
	}
	equal := true
	buf := make([]byte, maxLeaf)
	var compareNode func(*Rope) error
	compareNode = func(node *Rope) error {
		if node == nil {
			return nil
		}
		if node.isLeaf() {
			leafBuf := buf[:len(node.value)]
			if _, err := io.ReadFull(reader, leafBuf); err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					equal = false
					return nil
				}
				return err
			}
			if !bytes.Equal(node.value, leafBuf) {
				equal = false
			}
			return nil
		}
		if err := compareNode(node.left); err != nil {
			return err
		}
		return compareNode(node.right)
	}
	if err := compareNode(r); err != nil {
		return false, err
	}
	var extra [1]byte
	n, err := reader.Read(extra[:])
	switch {
	case err == nil && n > 0:
		return false, nil
	case errors.Is(err, io.EOF):
		return equal, nil
	case err != nil:
		return false, err
	default:
		// Readers are permitted to return (0, nil). ReadFull avoids treating
		// that transient result as EOF while still requiring exactly one byte.
		n, err = io.ReadFull(reader, extra[:])
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return equal, nil
		}
		if err != nil {
			return false, err
		}
		return n == 0 && equal, nil
	}
}

// WriteTo streams the rope in document order without first allocating a
// contiguous copy. Callers that target files should normally wrap the writer
// in a modest buffer because rope leaves are intentionally small.
func (r *Rope) WriteTo(w io.Writer) (int64, error) {
	if w == nil {
		return 0, io.ErrClosedPipe
	}
	if r == nil {
		return 0, nil
	}
	var written int64
	var writeNode func(*Rope) error
	writeNode = func(node *Rope) error {
		if node.isLeaf() {
			n, err := w.Write(node.value)
			written += int64(n)
			if err != nil {
				return err
			}
			if n != len(node.value) {
				return io.ErrShortWrite
			}
			return nil
		}
		if err := writeNode(node.left); err != nil {
			return err
		}
		return writeNode(node.right)
	}
	if err := writeNode(r); err != nil {
		return written, err
	}
	return written, nil
}

func (r *Rope) appendTo(buf *[]byte) {
	if r.isLeaf() {
		*buf = append(*buf, r.value...)
		return
	}
	r.left.appendTo(buf)
	r.right.appendTo(buf)
}

func (r *Rope) appendToContext(ctx context.Context, buf *[]byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.isLeaf() {
		*buf = append(*buf, r.value...)
		return nil
	}
	if err := r.left.appendToContext(ctx, buf); err != nil {
		return err
	}
	return r.right.appendToContext(ctx, buf)
}

// PrefixLines copies at most maxLines logical lines and maxBytes bytes without
// constructing the rope's whole-file line index. It is used for the initial
// syntax-highlighted frame, where bounded work is more important than seeing a
// very long first line in full.
func (r *Rope) PrefixLines(maxLines, maxBytes int) []byte {
	if r == nil || maxLines <= 0 || maxBytes <= 0 {
		return nil
	}
	maxBytes = min(maxBytes, r.len)
	buf := make([]byte, 0, maxBytes)
	lines := 0
	r.appendPrefixLines(&buf, maxLines, maxBytes, &lines)
	return buf
}

// appendPrefixLines returns true once either bound has been reached.
func (r *Rope) appendPrefixLines(buf *[]byte, maxLines, maxBytes int, lines *int) bool {
	if len(*buf) >= maxBytes || *lines >= maxLines {
		return true
	}
	if !r.isLeaf() {
		if r.left.appendPrefixLines(buf, maxLines, maxBytes, lines) {
			return true
		}
		return r.right.appendPrefixLines(buf, maxLines, maxBytes, lines)
	}

	remaining := maxBytes - len(*buf)
	take := min(len(r.value), remaining)
	for i := 0; i < take; i++ {
		if r.value[i] == '\n' {
			*lines++
			if *lines >= maxLines {
				take = i + 1
				break
			}
		}
	}
	*buf = append(*buf, r.value[:take]...)
	return len(*buf) >= maxBytes || *lines >= maxLines
}

// Slice returns a new Rope containing bytes [start, end).
func (r *Rope) Slice(start, end int) *Rope {
	if r == nil || start >= end || start >= r.len {
		return newLeaf(nil)
	}
	if end > r.len {
		end = r.len
	}
	if start <= 0 && end >= r.len {
		return r
	}
	if r.isLeaf() {
		if start < 0 {
			start = 0
		}
		return newLeaf(r.value[start:end])
	}
	ll := r.left.len
	if end <= ll {
		return r.left.Slice(start, end)
	}
	if start >= ll {
		return r.right.Slice(start-ll, end-ll)
	}
	return join(r.left.Slice(start, ll), r.right.Slice(0, end-ll))
}

// Insert returns a new Rope with data inserted at offset.
func (r *Rope) Insert(offset int, data []byte) *Rope {
	if len(data) == 0 {
		return r
	}
	if r == nil || r.len == 0 {
		return New(data)
	}
	// Keep ordinary typing in a small leaf as a single immutable copy. Without
	// this fast path each keystroke creates a new branch, even though the leaf
	// has ample capacity, which needlessly grows the persistent tree.
	if r.isLeaf() && r.len+len(data) <= maxLeaf {
		offset = max(0, min(offset, r.len))
		combined := make([]byte, r.len+len(data))
		copy(combined, r.value[:offset])
		copy(combined[offset:], data)
		copy(combined[offset+len(data):], r.value[offset:])
		return newLeafOwned(combined)
	}
	if offset <= 0 {
		return join(New(data), r)
	}
	if offset >= r.len {
		return join(r, New(data))
	}
	if r.isLeaf() {
		combined := make([]byte, 0, r.len+len(data))
		combined = append(combined, r.value[:offset]...)
		combined = append(combined, data...)
		combined = append(combined, r.value[offset:]...)
		return New(combined)
	}
	ll := r.left.len
	if offset <= ll {
		return join(r.left.Insert(offset, data), r.right)
	}
	return join(r.left, r.right.Insert(offset-ll, data))
}

// Delete returns a new Rope with n bytes removed starting at offset.
func (r *Rope) Delete(offset, n int) *Rope {
	if n <= 0 || r == nil || r.len == 0 {
		return r
	}
	if offset <= 0 && n >= r.len {
		return newLeaf(nil)
	}
	if offset < 0 {
		n += offset
		offset = 0
	}
	if offset+n > r.len {
		n = r.len - offset
	}
	left := r.Slice(0, offset)
	right := r.Slice(offset+n, r.len)
	return join(left, right)
}

// buildLineIndex walks the full rope content and populates the lineIndex cache.
// This is safe for concurrent access via sync.Once.
func (r *Rope) buildLineIndex() {
	idx := make([]int, 0, r.newlines+1)
	idx = append(idx, 0)
	data := r.Bytes()
	for i, b := range data {
		if b == '\n' {
			idx = append(idx, i+1)
		}
	}
	r.lineIndex = idx
}

// ensureLineIndex ensures the lineIndex cache is initialized.
// Safe for concurrent access.
func (r *Rope) ensureLineIndex() {
	r.initOnce.Do(r.buildLineIndex)
}

// LineStart returns the byte offset of the start of the given line (0-based).
func (r *Rope) LineStart(line int) ByteOffset {
	if line <= 0 {
		return 0
	}
	if r.newlines+1 <= maxCachedLineIndexEntries {
		r.ensureLineIndex()
		if line < len(r.lineIndex) {
			return r.lineIndex[line]
		}
		return r.len
	}
	return r.lineStartWithoutIndex(line)
}

func (r *Rope) lineStartWithoutIndex(line int) ByteOffset {
	if line <= 0 || r == nil {
		return 0
	}
	if r.isLeaf() {
		seen := 0
		for i, b := range r.value {
			if b == '\n' {
				seen++
				if seen == line {
					return i + 1
				}
			}
		}
		return r.len
	}
	if line <= r.left.newlines {
		return r.left.lineStartWithoutIndex(line)
	}
	return r.left.len + r.right.lineStartWithoutIndex(line-r.left.newlines)
}

// Line returns the content of the given line (0-based), without trailing newline.
func (r *Rope) Line(line int) []byte {
	start := r.LineStart(line)
	end := r.LineStart(line + 1)
	if end > start && end <= r.len {
		// remove the trailing newline
		b := r.Slice(start, end).Bytes()
		if len(b) > 0 && b[len(b)-1] == '\n' {
			b = b[:len(b)-1]
		}
		return b
	}
	// last line (no trailing newline)
	return r.Slice(start, r.len).Bytes()
}

// LineLen returns the length in bytes of the given line, excluding the newline.
func (r *Rope) LineLen(line int) int {
	start := r.LineStart(line)
	if line >= 0 && line < r.newlines {
		end := r.LineStart(line + 1)
		return end - start - 1
	}
	return r.len - start
}

// PositionToOffset converts a Position to a byte offset.
func (r *Rope) PositionToOffset(pos Position) ByteOffset {
	lineStart := r.LineStart(pos.Line)
	col := min(pos.Col, r.LineLen(pos.Line))
	return lineStart + col
}

// PositionToOffsetUncached returns the offset for a byte position without
// initializing the full line-index cache. Rendering code uses it for bounded
// probes (such as bracket matching), where copying a large document merely to
// find one character would violate the frame budget.
func (r *Rope) PositionToOffsetUncached(pos Position) (ByteOffset, bool) {
	if r == nil || pos.Line < 0 || pos.Line >= r.LineCount() || pos.Col < 0 {
		return 0, false
	}
	start := r.lineStartWithoutIndex(pos.Line)
	end := ByteOffset(r.len)
	if pos.Line < r.newlines {
		end = r.lineStartWithoutIndex(pos.Line+1) - 1 // omit the newline
	}
	// A position at the end of a line is valid (and is how selections include
	// the final byte), even though it is not a valid character probe.
	if pos.Col > end-start {
		return 0, false
	}
	return start + pos.Col, true
}

// OffsetToPosition converts a byte offset to a Position.
func (r *Rope) OffsetToPosition(offset ByteOffset) Position {
	if offset <= 0 {
		return Position{0, 0}
	}
	if offset >= r.len {
		lastLine := r.LineCount() - 1
		return Position{lastLine, r.LineLen(lastLine)}
	}
	line := r.offsetToLine(offset)
	lineStart := r.LineStart(line)
	return Position{line, offset - lineStart}
}

func (r *Rope) offsetToLine(offset int) int {
	if r == nil {
		return 0
	}
	if r.isLeaf() {
		n := 0
		for i := 0; i < len(r.value) && i < offset; i++ {
			if r.value[i] == '\n' {
				n++
			}
		}
		return n
	}
	if offset <= r.left.len {
		return r.left.offsetToLine(offset)
	}
	return r.left.newlines + r.right.offsetToLine(offset-r.left.len)
}

// ByteAt returns the byte at the given offset.
func (r *Rope) ByteAt(offset int) byte {
	if r.isLeaf() {
		return r.value[offset]
	}
	if offset < r.left.len {
		return r.left.ByteAt(offset)
	}
	return r.right.ByteAt(offset - r.left.len)
}

// ByteAtSafe returns the byte at the given offset with bounds checking.
// Returns (byte, true) if the offset is valid, (0, false) otherwise.
func (r *Rope) ByteAtSafe(offset int) (byte, bool) {
	if r == nil || offset < 0 || offset >= r.len {
		return 0, false
	}
	return r.ByteAt(offset), true
}
