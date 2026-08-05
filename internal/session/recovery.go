package session

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"teak/internal/atomicfile"
)

// Recovery persistence is bounded per record, in aggregate decoded content,
// by record count, and by encoded file size. Oversized inputs are skipped or
// rejected rather than creating unbounded autosave or startup work.
const (
	// MaxRecoveryRecordBytes bounds one recovered buffer. Larger buffers are
	// skipped rather than copied and encoded on every autosave tick.
	MaxRecoveryRecordBytes = 4 << 20
	// MaxRecoveryContentBytes bounds the aggregate decoded buffer content in a
	// workspace recovery set.
	MaxRecoveryContentBytes = 32 << 20
	// MaxRecoveryRecords bounds metadata growth for workspaces with very many
	// dirty or untitled tabs.
	MaxRecoveryRecords = 256
	// Base64 expands []byte JSON fields by roughly one third. This ceiling
	// leaves room for record metadata while bounding the initial file read.
	maxRecoveryFileBytes = 48 << 20
)

// RecoveryRecord holds one buffer's content at the last autosave, so a crash,
// SIGKILL, or power loss does not silently discard unsaved edits. Untitled
// buffers have no FilePath; they exist only here until saved.
type RecoveryRecord struct {
	FilePath string    `json:"file_path,omitempty"`
	Untitled bool      `json:"untitled,omitempty"`
	CRLF     bool      `json:"crlf,omitempty"`
	Modified time.Time `json:"modified"`
	Content  []byte    `json:"content"`
}

// RecoveryPath returns the per-workspace recovery file path, next to the
// workspace's session.json.
func RecoveryPath(rootDir string) string {
	return filepath.Join(StateHome(), "sessions", rootKey(rootDir), "recovery.json")
}

// SaveRecovery atomically persists the bounded current recovery set. Empty or
// over-budget records are dropped; an empty set removes the file so a clean
// state never resurrects old work.
func SaveRecovery(rootDir string, records []RecoveryRecord) error {
	kept := boundedRecoveryRecords(records)
	if len(kept) == 0 {
		return ClearRecovery(rootDir)
	}
	path := RecoveryPath(rootDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(path, func(file *os.File) error {
		return json.NewEncoder(file).Encode(kept)
	})
}

func boundedRecoveryRecords(records []RecoveryRecord) []RecoveryRecord {
	kept := make([]RecoveryRecord, 0, len(records))
	total := 0
	for _, record := range records {
		if len(kept) >= MaxRecoveryRecords {
			break
		}
		size := len(record.Content)
		if size == 0 || size > MaxRecoveryRecordBytes || size > MaxRecoveryContentBytes-total {
			continue
		}
		kept = append(kept, record)
		total += size
	}
	return kept
}

// LoadRecovery returns the bounded persisted recovery records for a workspace.
// A missing, corrupt, or oversized file reads as no records: recovery is best
// effort and must never block or exhaust memory during startup.
func LoadRecovery(rootDir string) ([]RecoveryRecord, error) {
	file, err := os.Open(RecoveryPath(rootDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	if info, statErr := file.Stat(); statErr != nil {
		return nil, statErr
	} else if info.Size() > maxRecoveryFileBytes {
		return nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(file, maxRecoveryFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRecoveryFileBytes {
		return nil, nil
	}
	var records []RecoveryRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, nil
	}
	return boundedRecoveryRecords(records), nil
}

// ClearRecovery removes the recovery file for a workspace.
func ClearRecovery(rootDir string) error {
	err := os.Remove(RecoveryPath(rootDir))
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
