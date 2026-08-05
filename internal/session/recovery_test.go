package session

import (
	"os"
	"testing"
	"time"
)

func TestRecoverySaveLoadRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	records := []RecoveryRecord{
		{FilePath: "/tmp/project/main.go", CRLF: true, Modified: time.Now(), Content: []byte("package main\n")},
		{Untitled: true, Modified: time.Now(), Content: []byte("scratch notes")},
	}

	if err := SaveRecovery(root, records); err != nil {
		t.Fatalf("SaveRecovery: %v", err)
	}
	loaded, err := LoadRecovery(root)
	if err != nil {
		t.Fatalf("LoadRecovery: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d records, want 2", len(loaded))
	}
	if loaded[0].FilePath != records[0].FilePath || !loaded[0].CRLF || string(loaded[0].Content) != "package main\n" {
		t.Errorf("record 0 = %+v, want the saved file record", loaded[0])
	}
	if !loaded[1].Untitled || string(loaded[1].Content) != "scratch notes" {
		t.Errorf("record 1 = %+v, want the saved untitled record", loaded[1])
	}
}

func TestRecoverySaveEmptyClears(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	if err := SaveRecovery(root, []RecoveryRecord{{FilePath: "/tmp/a", Content: []byte("x")}}); err != nil {
		t.Fatalf("SaveRecovery: %v", err)
	}

	// All buffers saved → nothing left to recover → the file must go away so a
	// later launch does not resurrect clean work.
	if err := SaveRecovery(root, nil); err != nil {
		t.Fatalf("SaveRecovery(empty): %v", err)
	}
	loaded, err := LoadRecovery(root)
	if err != nil {
		t.Fatalf("LoadRecovery: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded %d records after clearing, want 0", len(loaded))
	}
}

func TestRecoverySkipsEmptyAndOversizedRecords(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	records := []RecoveryRecord{
		{FilePath: "/tmp/empty", Content: nil},
		{FilePath: "/tmp/huge", Content: make([]byte, MaxRecoveryRecordBytes+1)},
		{FilePath: "/tmp/kept", Content: []byte("kept")},
	}
	if err := SaveRecovery(root, records); err != nil {
		t.Fatalf("SaveRecovery: %v", err)
	}
	loaded, err := LoadRecovery(root)
	if err != nil {
		t.Fatalf("LoadRecovery: %v", err)
	}
	if len(loaded) != 1 || loaded[0].FilePath != "/tmp/kept" {
		t.Fatalf("loaded = %+v, want only the bounded record", loaded)
	}
}

func TestBoundedRecoveryRecordsEnforcesAggregateBudget(t *testing.T) {
	content := make([]byte, MaxRecoveryRecordBytes)
	count := MaxRecoveryContentBytes/MaxRecoveryRecordBytes + 1
	records := make([]RecoveryRecord, count)
	for i := range records {
		records[i] = RecoveryRecord{FilePath: "/tmp/file", Content: content}
	}

	bounded := boundedRecoveryRecords(records)
	if got, want := len(bounded), MaxRecoveryContentBytes/MaxRecoveryRecordBytes; got != want {
		t.Fatalf("bounded records = %d, want %d", got, want)
	}
}

func TestBoundedRecoveryRecordsEnforcesRecordLimit(t *testing.T) {
	records := make([]RecoveryRecord, MaxRecoveryRecords+1)
	for i := range records {
		records[i] = RecoveryRecord{FilePath: "/tmp/file", Content: []byte("x")}
	}

	if got := len(boundedRecoveryRecords(records)); got != MaxRecoveryRecords {
		t.Fatalf("bounded records = %d, want %d", got, MaxRecoveryRecords)
	}
}

func TestRecoveryLoadMissingFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	loaded, err := LoadRecovery(t.TempDir())
	if err != nil {
		t.Fatalf("LoadRecovery on a fresh workspace: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded %d records, want 0", len(loaded))
	}
}

func TestRecoveryLoadCorruptFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	if err := SaveRecovery(root, []RecoveryRecord{{FilePath: "/tmp/a", Content: []byte("x")}}); err != nil {
		t.Fatalf("SaveRecovery: %v", err)
	}
	path := RecoveryPath(root)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("corrupting recovery file: %v", err)
	}
	// A corrupt recovery file must not block startup; it reads as no records.
	loaded, err := LoadRecovery(root)
	if err != nil {
		t.Fatalf("LoadRecovery on corrupt file: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded %d records from corrupt file, want 0", len(loaded))
	}
}

func TestRecoveryLoadRejectsOversizedFileBeforeReading(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	if err := SaveRecovery(root, []RecoveryRecord{{FilePath: "/tmp/a", Content: []byte("x")}}); err != nil {
		t.Fatalf("SaveRecovery: %v", err)
	}
	if err := os.Truncate(RecoveryPath(root), maxRecoveryFileBytes+1); err != nil {
		t.Fatalf("Truncate: %v", err)
	}

	loaded, err := LoadRecovery(root)
	if err != nil {
		t.Fatalf("LoadRecovery: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded %d records from oversized file, want 0", len(loaded))
	}
}
