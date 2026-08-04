package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"teak/internal/atomicfile"
)

// maxRecoveryRecordBounds bounds a single buffer's recovery snapshot. Larger
// buffers are skipped rather than risking multi-megabyte recovery writes on
// every autosave tick.
const maxRecoveryRecordBytes = 4 << 20

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

// SaveRecovery atomically persists the current recovery set. Empty records and
// records over the size bound are dropped; an empty set removes the file so a
// clean state never resurrects old work.
func SaveRecovery(rootDir string, records []RecoveryRecord) error {
	kept := make([]RecoveryRecord, 0, len(records))
	for _, record := range records {
		if len(record.Content) == 0 || len(record.Content) > maxRecoveryRecordBytes {
			continue
		}
		kept = append(kept, record)
	}
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

// LoadRecovery returns the persisted recovery records for a workspace. A
// missing or corrupt file reads as no records: recovery is a best effort and
// must never block startup.
func LoadRecovery(rootDir string) ([]RecoveryRecord, error) {
	data, err := os.ReadFile(RecoveryPath(rootDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []RecoveryRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, nil
	}
	return records, nil
}

// ClearRecovery removes the recovery file for a workspace.
func ClearRecovery(rootDir string) error {
	err := os.Remove(RecoveryPath(rootDir))
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
