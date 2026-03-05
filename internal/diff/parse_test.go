package diff

import (
	"testing"
)

func TestParseUnifiedDiff_Empty(t *testing.T) {
	result := ParseUnifiedDiff("")
	if result != nil {
		t.Errorf("expected nil, got %d lines", len(result))
	}
}

func TestParseUnifiedDiff_SimpleChange(t *testing.T) {
	raw := `diff --git a/file.go b/file.go
index abc..def 100644
--- a/file.go
+++ b/file.go
@@ -1,3 +1,3 @@
 line1
-old line
+new line
 line3
`
	lines := ParseUnifiedDiff(raw)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// line1 unchanged
	if lines[0].LeftKind != KindUnchanged || lines[0].RightKind != KindUnchanged {
		t.Errorf("line 0: expected unchanged, got left=%d right=%d", lines[0].LeftKind, lines[0].RightKind)
	}
	if lines[0].Left != "line1" || lines[0].Right != "line1" {
		t.Errorf("line 0: expected 'line1', got left=%q right=%q", lines[0].Left, lines[0].Right)
	}

	// old → new (paired)
	if lines[1].LeftKind != KindRemoved || lines[1].RightKind != KindAdded {
		t.Errorf("line 1: expected removed/added, got left=%d right=%d", lines[1].LeftKind, lines[1].RightKind)
	}
	if lines[1].Left != "old line" || lines[1].Right != "new line" {
		t.Errorf("line 1: expected 'old line'/'new line', got left=%q right=%q", lines[1].Left, lines[1].Right)
	}

	// line3 unchanged
	if lines[2].LeftKind != KindUnchanged || lines[2].RightKind != KindUnchanged {
		t.Errorf("line 2: expected unchanged, got left=%d right=%d", lines[2].LeftKind, lines[2].RightKind)
	}
}

func TestParseUnifiedDiff_AddedLines(t *testing.T) {
	raw := `@@ -1,2 +1,4 @@
 line1
+added1
+added2
 line2
`
	lines := ParseUnifiedDiff(raw)
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}

	if lines[0].LeftKind != KindUnchanged {
		t.Errorf("line 0: expected unchanged")
	}
	if lines[1].LeftKind != KindEmpty || lines[1].RightKind != KindAdded {
		t.Errorf("line 1: expected empty/added, got left=%d right=%d", lines[1].LeftKind, lines[1].RightKind)
	}
	if lines[2].LeftKind != KindEmpty || lines[2].RightKind != KindAdded {
		t.Errorf("line 2: expected empty/added")
	}
	if lines[3].LeftKind != KindUnchanged {
		t.Errorf("line 3: expected unchanged")
	}
}

func TestParseUnifiedDiff_RemovedLines(t *testing.T) {
	raw := `@@ -1,4 +1,2 @@
 line1
-removed1
-removed2
 line2
`
	lines := ParseUnifiedDiff(raw)
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}

	if lines[1].LeftKind != KindRemoved || lines[1].RightKind != KindEmpty {
		t.Errorf("line 1: expected removed/empty, got left=%d right=%d", lines[1].LeftKind, lines[1].RightKind)
	}
	if lines[1].Left != "removed1" {
		t.Errorf("line 1: expected 'removed1', got %q", lines[1].Left)
	}
}

func TestParseUnifiedDiff_MultipleHunks(t *testing.T) {
	raw := `@@ -1,2 +1,2 @@
-old1
+new1
 same
@@ -10,2 +10,2 @@
 same2
-old2
+new2
`
	lines := ParseUnifiedDiff(raw)
	// Should have separator between hunks
	hasSep := false
	for _, l := range lines {
		if l.IsSeparator {
			hasSep = true
			break
		}
	}
	if !hasSep {
		t.Error("expected separator between hunks")
	}
}

func TestParseUnifiedDiff_UnevenPairing(t *testing.T) {
	raw := `@@ -1,3 +1,1 @@
-a
-b
-c
+x
`
	lines := ParseUnifiedDiff(raw)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// First pair: a → x
	if lines[0].LeftKind != KindRemoved || lines[0].RightKind != KindAdded {
		t.Errorf("line 0: expected removed/added")
	}
	// Extra removes get empty right
	if lines[1].LeftKind != KindRemoved || lines[1].RightKind != KindEmpty {
		t.Errorf("line 1: expected removed/empty, got left=%d right=%d", lines[1].LeftKind, lines[1].RightKind)
	}
	if lines[2].LeftKind != KindRemoved || lines[2].RightKind != KindEmpty {
		t.Errorf("line 2: expected removed/empty")
	}
}

func TestAllAddedLines(t *testing.T) {
	content := "line1\nline2\nline3\n"
	lines := AllAddedLines(content)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	for i, l := range lines {
		if l.LeftKind != KindEmpty {
			t.Errorf("line %d: expected left empty", i)
		}
		if l.RightKind != KindAdded {
			t.Errorf("line %d: expected right added", i)
		}
		if l.RightNum != i+1 {
			t.Errorf("line %d: expected RightNum=%d, got %d", i, i+1, l.RightNum)
		}
	}
}

func TestParseHunkHeader(t *testing.T) {
	tests := []struct {
		input     string
		wantOld   int
		wantNew   int
	}{
		{"@@ -1,3 +1,3 @@", 1, 1},
		{"@@ -10,5 +20,7 @@ func foo()", 10, 20},
		{"@@ -1 +1 @@", 1, 1},
		{"@@ -100,0 +101,3 @@", 100, 101},
	}
	for _, tt := range tests {
		old, new := parseHunkHeader(tt.input)
		if old != tt.wantOld || new != tt.wantNew {
			t.Errorf("parseHunkHeader(%q) = (%d, %d), want (%d, %d)", tt.input, old, new, tt.wantOld, tt.wantNew)
		}
	}
}

func TestParseUnifiedDiff_LineNumbers(t *testing.T) {
	raw := `@@ -5,3 +5,3 @@
 unchanged
-removed
+added
 unchanged2
`
	lines := ParseUnifiedDiff(raw)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	if lines[0].LeftNum != 5 || lines[0].RightNum != 5 {
		t.Errorf("line 0: expected nums 5/5, got %d/%d", lines[0].LeftNum, lines[0].RightNum)
	}
	if lines[1].LeftNum != 6 || lines[1].RightNum != 6 {
		t.Errorf("line 1: expected nums 6/6, got %d/%d", lines[1].LeftNum, lines[1].RightNum)
	}
	if lines[2].LeftNum != 7 || lines[2].RightNum != 7 {
		t.Errorf("line 2: expected nums 7/7, got %d/%d", lines[2].LeftNum, lines[2].RightNum)
	}
}
