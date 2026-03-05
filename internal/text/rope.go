package text

import "bytes"

const maxLeaf = 512

// Rope is a persistent (immutable) rope data structure for efficient text manipulation.
// Each mutation returns a new Rope; the original is unchanged.
type Rope struct {
	left      *Rope
	right     *Rope
	value     []byte // only set for leaf nodes
	len       int
	newlines  int
	depth     int
	lineIndex []int // lazy cache of byte offsets for each line start
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

// NewFromString creates a Rope from a string.
func NewFromString(s string) *Rope {
	return New([]byte(s))
}

func newLeaf(data []byte) *Rope {
	b := make([]byte, len(data))
	copy(b, data)
	return &Rope{
		value:    b,
		len:      len(b),
		newlines: bytes.Count(b, []byte{'\n'}),
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

func (r *Rope) appendTo(buf *[]byte) {
	if r.isLeaf() {
		*buf = append(*buf, r.value...)
		return
	}
	r.left.appendTo(buf)
	r.right.appendTo(buf)
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

// LineStart returns the byte offset of the start of the given line (0-based).
func (r *Rope) LineStart(line int) ByteOffset {
	if line <= 0 {
		return 0
	}
	if r.lineIndex == nil {
		r.buildLineIndex()
	}
	if line < len(r.lineIndex) {
		return r.lineIndex[line]
	}
	return r.len
}

func (r *Rope) lineStartHelper(line int, base int) int {
	if r == nil {
		return base
	}
	if r.isLeaf() {
		off := 0
		remaining := line
		for remaining > 0 {
			idx := bytes.IndexByte(r.value[off:], '\n')
			if idx < 0 {
				return base + r.len
			}
			off += idx + 1
			remaining--
		}
		return base + off
	}
	if line <= r.left.newlines {
		return r.left.lineStartHelper(line, base)
	}
	return r.right.lineStartHelper(line-r.left.newlines, base+r.left.len)
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
	if r.lineIndex == nil {
		r.buildLineIndex()
	}
	start := r.LineStart(line)
	// Check if there is a next line in the index (meaning this line ends with \n)
	if line+1 < len(r.lineIndex) {
		return r.lineIndex[line+1] - start - 1
	}
	// last line: no trailing newline
	return r.len - start
}

// PositionToOffset converts a Position to a byte offset.
func (r *Rope) PositionToOffset(pos Position) ByteOffset {
	lineStart := r.LineStart(pos.Line)
	lineContent := r.Line(pos.Line)
	col := min(pos.Col, len(lineContent))
	return lineStart + col
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
