package app

import (
	"context"
	"sync"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/lsp"
	"teak/internal/problems"
)

// diagnosticProjectionStore owns immutable per-file problem slices. Commands
// may read and aggregate them concurrently; Update only reserves generations
// and swaps a single file reference.
type diagnosticProjectionStore struct {
	mu                 sync.RWMutex
	nextGeneration     uint64
	latestByPath       map[string]uint64
	cancelByPath       map[string]context.CancelFunc
	files              map[string][]problems.Problem
	snapshotGeneration uint64
}

type diagnosticsPreparedMsg struct {
	Path        string
	Version     int
	HasVersion  bool
	Generation  uint64
	Diagnostics []lsp.Diagnostic
	EditorSet   *editor.DiagnosticSet
	Problems    []problems.Problem
	Severity    int
	HasSeverity bool
	Canceled    bool
}

type diagnosticsSnapshotPreparedMsg struct {
	Generation uint64
	Snapshot   problems.Snapshot
}

func newDiagnosticProjectionStore() *diagnosticProjectionStore {
	return &diagnosticProjectionStore{
		latestByPath: make(map[string]uint64),
		cancelByPath: make(map[string]context.CancelFunc),
		files:        make(map[string][]problems.Problem),
	}
}

func (s *diagnosticProjectionStore) begin(path string) (uint64, context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel := s.cancelByPath[path]; cancel != nil {
		cancel()
	}
	s.nextGeneration++
	s.latestByPath[path] = s.nextGeneration
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelByPath[path] = cancel
	return s.nextGeneration, ctx
}

func (s *diagnosticProjectionStore) isLatest(path string, generation uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestByPath[path] == generation
}

func (s *diagnosticProjectionStore) accept(path string, generation uint64, fileProblems []problems.Problem) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latestByPath[path] != generation {
		return 0, false
	}
	if cancel := s.cancelByPath[path]; cancel != nil {
		cancel()
		delete(s.cancelByPath, path)
	}
	delete(s.latestByPath, path)
	if len(fileProblems) == 0 {
		delete(s.files, path)
	} else {
		s.files[path] = fileProblems
	}
	s.snapshotGeneration++
	return s.snapshotGeneration, true
}

func (s *diagnosticProjectionStore) relocate(oldPath, newPath string) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fileProblems, ok := s.files[oldPath]
	if !ok || oldPath == "" || newPath == "" || oldPath == newPath {
		return 0, false
	}
	// Retire preparation commands targeting either identity.
	if cancel := s.cancelByPath[oldPath]; cancel != nil {
		cancel()
	}
	if cancel := s.cancelByPath[newPath]; cancel != nil {
		cancel()
	}
	delete(s.cancelByPath, oldPath)
	delete(s.cancelByPath, newPath)
	delete(s.latestByPath, oldPath)
	delete(s.latestByPath, newPath)
	s.files[newPath] = fileProblems
	delete(s.files, oldPath)
	s.snapshotGeneration++
	return s.snapshotGeneration, true
}

func (s *diagnosticProjectionStore) snapshotCmd(generation uint64) tea.Cmd {
	return func() tea.Msg {
		s.mu.RLock()
		if generation != s.snapshotGeneration {
			s.mu.RUnlock()
			return diagnosticsSnapshotPreparedMsg{Generation: generation}
		}
		parts := make(map[string][]problems.Problem, len(s.files))
		count := 0
		for path, fileProblems := range s.files {
			parts[path] = fileProblems
			count += len(fileProblems)
		}
		s.mu.RUnlock()

		flat := make([]problems.Problem, 0, count)
		for path, fileProblems := range parts {
			for _, problem := range fileProblems {
				problem.FilePath = path
				flat = append(flat, problem)
			}
		}
		return diagnosticsSnapshotPreparedMsg{
			Generation: generation,
			Snapshot:   problems.PrepareSnapshot(flat),
		}
	}
}

func (s *diagnosticProjectionStore) isCurrentSnapshot(generation uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotGeneration == generation
}

func (s *diagnosticProjectionStore) currentSnapshotGeneration() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotGeneration
}

func (s *diagnosticProjectionStore) finish(path string, generation uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latestByPath[path] != generation {
		return
	}
	if cancel := s.cancelByPath[path]; cancel != nil {
		cancel()
		delete(s.cancelByPath, path)
	}
	delete(s.latestByPath, path)
}

func prepareDiagnostics(ctx context.Context, msg lsp.DiagnosticsMsg, path string, generation uint64) diagnosticsPreparedMsg {
	prepared := diagnosticsPreparedMsg{
		Path:       path,
		Version:    msg.Version,
		HasVersion: msg.HasVersion,
		Generation: generation,
	}
	if err := ctx.Err(); err != nil {
		prepared.Canceled = true
		return prepared
	}
	prepared.Diagnostics = make([]lsp.Diagnostic, len(msg.Diagnostics))
	editorDiagnostics := make([]editor.Diagnostic, len(msg.Diagnostics))
	prepared.Problems = make([]problems.Problem, len(msg.Diagnostics))
	for index, diagnostic := range msg.Diagnostics {
		if index%256 == 0 {
			if err := ctx.Err(); err != nil {
				prepared.Canceled = true
				return prepared
			}
		}
		prepared.Diagnostics[index] = diagnostic
		editorDiagnostics[index] = editor.Diagnostic{
			StartLine: diagnostic.Range.Start.Line,
			StartCol:  diagnostic.Range.Start.Character,
			EndLine:   diagnostic.Range.End.Line,
			EndCol:    diagnostic.Range.End.Character,
			Severity:  int(diagnostic.Severity),
			Message:   diagnostic.Message,
		}
		prepared.Problems[index] = problems.Problem{
			Line:     diagnostic.Range.Start.Line,
			Col:      diagnostic.Range.Start.Character,
			EndLine:  diagnostic.Range.End.Line,
			EndCol:   diagnostic.Range.End.Character,
			Severity: int(diagnostic.Severity),
			Message:  diagnostic.Message,
			Source:   diagnostic.Source,
		}
		severity := int(diagnostic.Severity)
		if !prepared.HasSeverity || severity < prepared.Severity {
			prepared.Severity = severity
			prepared.HasSeverity = true
		}
	}
	set, err := editor.PrepareDiagnosticSet(ctx, editorDiagnostics)
	if err != nil {
		prepared.Canceled = true
		return prepared
	}
	prepared.EditorSet = set
	return prepared
}
