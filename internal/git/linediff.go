package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"teak/internal/toolpath"
)

// LineKind is a per-line git gutter mark relative to HEAD.
type LineKind uint8

const (
	LineUnchanged LineKind = iota
	LineAdded
	LineModified
	LineDeleted
)

// DiffLinesAgainstHEAD returns 0-based line marks for path. Deleted hunks
// are recorded on the following surviving line (or line 0).
func DiffLinesAgainstHEAD(ctx context.Context, rootDir, path string) (map[int]LineKind, error) {
	if rootDir == "" || path == "" {
		return nil, nil
	}
	cmd, err := toolpath.Command(ctx, "git", "-C", rootDir, "diff", "-U0", "--no-color", "HEAD", "--", path)
	if err != nil {
		return nil, err
	}
	stdout, stderr, err := toolpath.RunBounded(cmd, 2<<20, 16<<10)
	if err != nil {
		if bytes.Contains(stderr, []byte("bad revision 'HEAD'")) || bytes.Contains(stderr, []byte("does not have any commits")) {
			return nil, nil
		}
		if bytes.Contains(stderr, []byte("unknown revision")) {
			return nil, nil
		}
		return nil, fmt.Errorf("git diff: %w (%s)", err, bytes.TrimSpace(stderr))
	}
	return parseUnifiedZeroDiff(stdout), nil
}

func parseUnifiedZeroDiff(data []byte) map[int]LineKind {
	marks := make(map[int]LineKind)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "@@") {
			continue
		}
		newStart, newCount, ok := parseHunkHeader(line)
		if !ok {
			continue
		}
		switch {
		case newCount == 0:
			target := newStart - 1
			if target < 0 {
				target = 0
			}
			if marks[target] == LineUnchanged {
				marks[target] = LineDeleted
			}
		case newCount > 0:
			kind := LineAdded
			if strings.Contains(line, "-") && !strings.HasPrefix(strings.TrimSpace(line), "@@ -0,0") {
				oldStart, oldCount, oldOK := parseOldHunk(line)
				if oldOK && oldCount > 0 && oldStart > 0 {
					kind = LineModified
				}
			}
			for i := 0; i < newCount; i++ {
				lineIdx := newStart - 1 + i
				if lineIdx < 0 {
					continue
				}
				marks[lineIdx] = kind
			}
		}
	}
	return marks
}

func parseHunkHeader(header string) (start, count int, ok bool) {
	// @@ -oldStart,oldCount +newStart,newCount @@
	plus := strings.Index(header, "+")
	if plus < 0 {
		return 0, 0, false
	}
	rest := header[plus+1:]
	end := strings.IndexAny(rest, " @")
	if end >= 0 {
		rest = rest[:end]
	}
	start, count, ok = parseRange(rest)
	return start, count, ok
}

func parseOldHunk(header string) (start, count int, ok bool) {
	minus := strings.Index(header, "-")
	if minus < 0 {
		return 0, 0, false
	}
	rest := header[minus+1:]
	end := strings.IndexAny(rest, " +")
	if end >= 0 {
		rest = rest[:end]
	}
	return parseRange(rest)
}

func parseRange(spec string) (start, count int, ok bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, 0, false
	}
	startStr, countStr, hasCount := strings.Cut(spec, ",")
	start, err := strconv.Atoi(startStr)
	if err != nil {
		return 0, 0, false
	}
	count = 1
	if hasCount {
		count, err = strconv.Atoi(countStr)
		if err != nil {
			return 0, 0, false
		}
	}
	return start, count, true
}
