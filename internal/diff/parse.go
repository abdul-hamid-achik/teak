package diff

import (
	"strings"
)

// LineKind classifies a diff line.
type LineKind int

const (
	KindUnchanged LineKind = iota
	KindAdded
	KindRemoved
	KindEmpty // padding (no content on this side)
)

// DiffLine represents one row in a side-by-side diff view.
type DiffLine struct {
	Left     string
	Right    string
	LeftNum  int // 1-based line number, 0 = padding/separator
	RightNum int
	LeftKind  LineKind
	RightKind LineKind
	IsSeparator bool // true for "..." hunk separators
}

// ParseUnifiedDiff parses unified diff output into side-by-side DiffLine rows.
func ParseUnifiedDiff(raw string) []DiffLine {
	if raw == "" {
		return nil
	}

	lines := strings.Split(raw, "\n")
	// Remove trailing empty line from split
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var result []DiffLine
	var leftNum, rightNum int
	lastHunkEnd := -1 // track end of previous hunk for separator insertion

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Skip diff headers
		if strings.HasPrefix(line, "diff ") ||
			strings.HasPrefix(line, "index ") ||
			strings.HasPrefix(line, "--- ") ||
			strings.HasPrefix(line, "+++ ") ||
			strings.HasPrefix(line, "Binary ") {
			continue
		}

		// Hunk header
		if strings.HasPrefix(line, "@@") {
			oldStart, newStart := parseHunkHeader(line)
			// Insert separator between non-contiguous hunks
			if lastHunkEnd >= 0 {
				result = append(result, DiffLine{
					Left:        "...",
					Right:       "...",
					IsSeparator: true,
				})
			}
			leftNum = oldStart
			rightNum = newStart
			lastHunkEnd = len(result)
			continue
		}

		if len(line) == 0 {
			// Empty context line (blank line in source)
			result = append(result, DiffLine{
				Left:      "",
				Right:     "",
				LeftNum:   leftNum,
				RightNum:  rightNum,
				LeftKind:  KindUnchanged,
				RightKind: KindUnchanged,
			})
			leftNum++
			rightNum++
			continue
		}

		prefix := line[0]
		content := line[1:]

		switch prefix {
		case ' ':
			result = append(result, DiffLine{
				Left:      content,
				Right:     content,
				LeftNum:   leftNum,
				RightNum:  rightNum,
				LeftKind:  KindUnchanged,
				RightKind: KindUnchanged,
			})
			leftNum++
			rightNum++

		case '-':
			// Collect consecutive removed lines
			removed := []string{content}
			removedStart := leftNum
			leftNum++
			for i+1 < len(lines) && len(lines[i+1]) > 0 && lines[i+1][0] == '-' {
				i++
				removed = append(removed, lines[i][1:])
				leftNum++
			}
			// Collect consecutive added lines
			var added []string
			addedStart := rightNum
			for i+1 < len(lines) && len(lines[i+1]) > 0 && lines[i+1][0] == '+' {
				i++
				added = append(added, lines[i][1:])
				rightNum++
			}
			// Pair them up
			maxLen := max(len(removed), len(added))
			for j := 0; j < maxLen; j++ {
				dl := DiffLine{}
				if j < len(removed) {
					dl.Left = removed[j]
					dl.LeftNum = removedStart + j
					dl.LeftKind = KindRemoved
				} else {
					dl.LeftKind = KindEmpty
				}
				if j < len(added) {
					dl.Right = added[j]
					dl.RightNum = addedStart + j
					dl.RightKind = KindAdded
				} else {
					dl.RightKind = KindEmpty
				}
				result = append(result, dl)
			}

		case '+':
			// Standalone added lines (no preceding removes)
			result = append(result, DiffLine{
				Right:     content,
				RightNum:  rightNum,
				LeftKind:  KindEmpty,
				RightKind: KindAdded,
			})
			rightNum++
		}
	}

	return result
}

// AllAddedLines creates diff lines for a completely new/untracked file.
func AllAddedLines(content string) []DiffLine {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	// Remove trailing empty line from final newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	result := make([]DiffLine, len(lines))
	for i, line := range lines {
		result[i] = DiffLine{
			Right:     line,
			RightNum:  i + 1,
			LeftKind:  KindEmpty,
			RightKind: KindAdded,
		}
	}
	return result
}

// parseHunkHeader parses "@@ -old,count +new,count @@" and returns old start, new start.
func parseHunkHeader(line string) (int, int) {
	// Find the range specifications between @@ markers
	line = strings.TrimPrefix(line, "@@")
	idx := strings.Index(line, "@@")
	if idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(line)

	parts := strings.Fields(line)
	oldStart, newStart := 1, 1
	for _, p := range parts {
		if strings.HasPrefix(p, "-") {
			oldStart = parseRangeStart(p[1:])
		} else if strings.HasPrefix(p, "+") {
			newStart = parseRangeStart(p[1:])
		}
	}
	return oldStart, newStart
}

func parseRangeStart(s string) int {
	// Format: "line,count" or just "line"
	comma := strings.Index(s, ",")
	if comma >= 0 {
		s = s[:comma]
	}
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	if n == 0 {
		return 1
	}
	return n
}
