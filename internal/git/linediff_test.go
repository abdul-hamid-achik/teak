package git

import "testing"

func TestParseUnifiedZeroDiffAddedAndModified(t *testing.T) {
	diff := []byte("@@ -1,0 +1,2 @@\n+one\n+two\n@@ -10,2 +12,2 @@\n-old\n+new\n")
	marks := parseUnifiedZeroDiff(diff)
	if marks[0] != LineAdded || marks[1] != LineAdded {
		t.Fatalf("added marks = %+v", marks)
	}
	if marks[11] != LineModified || marks[12] != LineModified {
		t.Fatalf("modified marks = %+v", marks)
	}
}

func TestParseUnifiedZeroDiffDeleted(t *testing.T) {
	diff := []byte("@@ -4,1 +4,0 @@\n-gone\n")
	marks := parseUnifiedZeroDiff(diff)
	if marks[3] != LineDeleted {
		t.Fatalf("deleted mark = %+v, want line 3", marks)
	}
}

func TestParseHunkHeaderDefaultCount(t *testing.T) {
	start, count, ok := parseHunkHeader("@@ -1 +2 @@")
	if !ok || start != 2 || count != 1 {
		t.Fatalf("parseHunkHeader = %d,%d,%v", start, count, ok)
	}
}
