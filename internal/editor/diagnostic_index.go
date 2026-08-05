package editor

import (
	"context"
	"sort"
)

// DiagnosticSet is an immutable collection with a start-line ordering and a
// range-maximum tree. It lets View select diagnostics intersecting the viewport
// without scanning every diagnostic published for a generated or noisy file.
// Build it in a tea.Cmd and install it as one state transition.
type DiagnosticSet struct {
	diagnostics []Diagnostic
	order       []int
	maxEnd      []int
}

// PrepareDiagnosticSet takes ownership of diagnostics, builds a stable
// start-line ordering without changing publication order, and indexes interval
// ends. The preparation is cancellable and is intended to run alongside the
// other LSP diagnostic projections outside
// Bubble Tea's Update loop.
func PrepareDiagnosticSet(ctx context.Context, diagnostics []Diagnostic) (*DiagnosticSet, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(diagnostics) == 0 {
		return &DiagnosticSet{}, nil
	}
	order := make([]int, len(diagnostics))
	for i := range order {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		order[i] = i
	}
	if err := stableSortDiagnosticOrderContext(ctx, diagnostics, order); err != nil {
		return nil, err
	}

	set := &DiagnosticSet{
		diagnostics: diagnostics,
		order:       order,
		maxEnd:      make([]int, len(diagnostics)),
	}
	visited := 0
	if _, err := set.buildMaxEnd(ctx, 0, len(diagnostics)-1, &visited); err != nil {
		return nil, err
	}
	return set, nil
}

// Intersecting returns diagnostics whose line intervals overlap [start, end].
// Results retain the server's original publication order. The returned slice
// is owned by the caller; Diagnostic values are immutable after preparation.
func (s *DiagnosticSet) Intersecting(start, end int) []Diagnostic {
	if s == nil || len(s.diagnostics) == 0 || start > end {
		return nil
	}
	hi := sort.Search(len(s.diagnostics), func(i int) bool {
		return s.diagnostics[s.order[i]].StartLine > end
	})
	root := (len(s.diagnostics) - 1) / 2
	if hi == 0 || s.maxEnd[root] < start {
		return nil
	}
	capacity := min(hi, max(8, min(end-start+1, 32)))
	indices := make([]int, 0, capacity)
	s.visitIntersectingPrefix(0, len(s.diagnostics)-1, hi, start, func(index int, _ Diagnostic) bool {
		indices = append(indices, index)
		return true
	})
	sort.Ints(indices)
	matches := make([]Diagnostic, len(indices))
	for i, index := range indices {
		matches[i] = s.diagnostics[index]
	}
	return matches
}

// IntersectingLines selects diagnostics covering any line in a sparse visible
// projection, such as a viewport with a large collapsed fold. Contiguous rows
// are queried together and publication indices deduplicate ranges spanning
// more than one visible run.
func (s *DiagnosticSet) IntersectingLines(lines []int) []Diagnostic {
	if s == nil || len(s.diagnostics) == 0 || len(lines) == 0 {
		return nil
	}
	if len(lines) == 1 {
		return s.Intersecting(lines[0], lines[0])
	}
	indices := make([]int, 0, min(max(len(lines), 8), 32))
	seen := make(map[int]struct{}, min(len(lines), 32))
	visit := func(index int, _ Diagnostic) bool {
		if _, exists := seen[index]; exists {
			return true
		}
		seen[index] = struct{}{}
		indices = append(indices, index)
		return true
	}
	start := lines[0]
	previous := start
	for _, line := range lines[1:] {
		if line <= previous+1 {
			previous = max(previous, line)
			continue
		}
		s.visitIntersecting(start, previous, visit)
		start, previous = line, line
	}
	s.visitIntersecting(start, previous, visit)
	sort.Ints(indices)
	matches := make([]Diagnostic, len(indices))
	for i, index := range indices {
		matches[i] = s.diagnostics[index]
	}
	return matches
}

func (s *DiagnosticSet) buildMaxEnd(ctx context.Context, left, right int, visited *int) (int, error) {
	(*visited)++
	if *visited%256 == 0 {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
	}
	middle := left + (right-left)/2
	diagnostic := s.diagnostics[s.order[middle]]
	maximum := diagnostic.EndLine
	if maximum < diagnostic.StartLine {
		maximum = -1
	}
	if left < middle {
		leftMax, err := s.buildMaxEnd(ctx, left, middle-1, visited)
		if err != nil {
			return 0, err
		}
		maximum = max(maximum, leftMax)
	}
	if middle < right {
		rightMax, err := s.buildMaxEnd(ctx, middle+1, right, visited)
		if err != nil {
			return 0, err
		}
		maximum = max(maximum, rightMax)
	}
	s.maxEnd[middle] = maximum
	return maximum, nil
}

func (s *DiagnosticSet) visitIntersecting(start, end int, visit func(int, Diagnostic) bool) {
	if s == nil || len(s.diagnostics) == 0 || start > end || visit == nil {
		return
	}
	hi := sort.Search(len(s.diagnostics), func(i int) bool {
		return s.diagnostics[s.order[i]].StartLine > end
	})
	if hi == 0 {
		return
	}
	s.visitIntersectingPrefix(0, len(s.diagnostics)-1, hi, start, visit)
}

func (s *DiagnosticSet) visitIntersectingPrefix(left, right, hi, start int, visit func(int, Diagnostic) bool) bool {
	if left >= hi {
		return true
	}
	middle := left + (right-left)/2
	if s.maxEnd[middle] < start {
		return true
	}
	if left < middle && !s.visitIntersectingPrefix(left, middle-1, hi, start, visit) {
		return false
	}
	if middle < hi {
		index := s.order[middle]
		diagnostic := s.diagnostics[index]
		if diagnostic.EndLine >= diagnostic.StartLine && diagnostic.EndLine >= start && !visit(index, diagnostic) {
			return false
		}
	}
	if middle < right && middle+1 < hi {
		return s.visitIntersectingPrefix(middle+1, right, hi, start, visit)
	}
	return true
}

func stableSortDiagnosticOrderContext(ctx context.Context, diagnostics []Diagnostic, order []int) error {
	if len(order) < 2 {
		return ctx.Err()
	}
	scratch := make([]int, len(order))
	source := order
	destination := scratch
	writes := 0
	for width := 1; width < len(order); width *= 2 {
		for start := 0; start < len(order); start += 2 * width {
			middle := min(start+width, len(order))
			end := min(start+2*width, len(order))
			left, right := start, middle
			for output := start; output < end; output++ {
				writes++
				if writes%256 == 0 {
					if err := ctx.Err(); err != nil {
						return err
					}
				}
				if right >= end || (left < middle && diagnostics[source[left]].StartLine <= diagnostics[source[right]].StartLine) {
					destination[output] = source[left]
					left++
				} else {
					destination[output] = source[right]
					right++
				}
			}
		}
		source, destination = destination, source
		if width > len(order)/2 {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(source) > 0 && &source[0] != &order[0] {
		copy(order, source)
	}
	return nil
}
