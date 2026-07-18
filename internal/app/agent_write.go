package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"teak/internal/agent"
)

var errAgentWriteAtomicUnsupported = errors.New("atomic agent writes are not supported on this platform")

type agentWriteState struct {
	inFlight bool
	queued   []agent.WriteDecisionMsg
}

type agentWriteResultMsg struct {
	responseCh chan error
	path       string
	accepted   bool
	err        error
}

func (m Model) handleAgentWriteDecision(msg agent.WriteDecisionMsg) (tea.Model, tea.Cmd) {
	if m.agentWrites.inFlight {
		m.agentWrites.queued = append(m.agentWrites.queued, msg)
		return m, nil
	}
	return m.startAgentWriteDecision(msg)
}

func (m Model) startAgentWriteDecision(msg agent.WriteDecisionMsg) (tea.Model, tea.Cmd) {
	m.agentWrites.inFlight = true
	proposal := msg.Proposal
	if !msg.Accepted {
		return m, func() tea.Msg {
			return agentWriteResultMsg{
				responseCh: proposal.ResponseCh,
				path:       proposal.Path,
				err:        fmt.Errorf("agent write rejected by user"),
			}
		}
	}

	rootDir := m.rootDir
	pinnedRoot := m.agentWriteRoot
	ctx := proposal.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return m, func() tea.Msg {
		err := ctx.Err()
		if err == nil {
			root := pinnedRoot
			closeRoot := false
			if root == nil {
				root, err = os.OpenRoot(rootDir)
				closeRoot = err == nil
			}
			if closeRoot {
				defer func() {
					_ = root.Close()
				}()
			}
			var relativePath string
			if err == nil {
				relativePath, err = validateAgentWritePath(root, proposal.Path)
			}
			if err == nil {
				err = writeAgentFileAtomicRoot(ctx, root, relativePath, []byte(proposal.Content))
			}
		}
		return agentWriteResultMsg{
			responseCh: proposal.ResponseCh,
			path:       proposal.Path,
			accepted:   true,
			err:        err,
		}
	}
}

func (m Model) handleAgentWriteResult(msg agentWriteResultMsg) (tea.Model, tea.Cmd) {
	select {
	case msg.responseCh <- msg.err:
	default:
	}

	switch {
	case msg.err == nil:
		m.status = fmt.Sprintf("Agent wrote %s", msg.path)
	case !msg.accepted:
		m.status = "Agent write rejected"
	default:
		m.status = fmt.Sprintf("Agent write failed: %v", msg.err)
	}

	if len(m.agentWrites.queued) == 0 {
		m.agentWrites.inFlight = false
		return m, nil
	}

	next := m.agentWrites.queued[0]
	m.agentWrites.queued[0] = agent.WriteDecisionMsg{}
	m.agentWrites.queued = m.agentWrites.queued[1:]
	m.agentWrites.inFlight = false
	return m.startAgentWriteDecision(next)
}

func writeAgentFileAtomic(rootDir, relativePath string, data []byte) error {
	return writeAgentFileAtomicContext(context.Background(), rootDir, relativePath, data)
}

func writeAgentFileAtomicContext(ctx context.Context, rootDir, relativePath string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return fmt.Errorf("open root directory: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()
	return writeAgentFileAtomicRoot(ctx, root, relativePath, data)
}

func writeAgentFileAtomicRoot(ctx context.Context, root *os.Root, relativePath string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if root == nil {
		return fmt.Errorf("workspace root is unavailable")
	}
	if !agentWriteAtomicSupported {
		return errAgentWriteAtomicUnsupported
	}

	parent := filepath.Dir(relativePath)
	if err := mkdirAllAgentWrite(root, parent, 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	permissions := os.FileMode(0o666)
	existing := false
	if info, statErr := root.Stat(relativePath); statErr == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("target path is not a regular file")
		}
		permissions = info.Mode().Perm()
		existing = true
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect target file: %w", statErr)
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	tempFile, tempPath, err := createAgentWriteTemp(root, parent, permissions, existing)
	if err != nil {
		return err
	}
	defer func() {
		_ = tempFile.Close()
		_ = root.Remove(tempPath)
	}()

	if _, err := tempFile.Write(data); err != nil {
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := replaceAgentWrite(root, parent, tempPath, relativePath); err != nil {
		return fmt.Errorf("replace target file: %w", err)
	}
	return nil
}

func mkdirAllAgentWrite(root *os.Root, relativeDir string, permissions os.FileMode) error {
	if relativeDir == "." {
		return nil
	}

	current := ""
	for _, component := range strings.Split(relativeDir, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		if err := root.Mkdir(current, permissions); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := root.Stat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%q is not a directory", current)
		}
	}
	return nil
}

func createAgentWriteTemp(root *os.Root, parent string, permissions os.FileMode, preservePermissions bool) (*os.File, string, error) {
	for range 100 {
		var randomBytes [16]byte
		if _, err := rand.Read(randomBytes[:]); err != nil {
			return nil, "", fmt.Errorf("generate temporary file name: %w", err)
		}
		tempPath := filepath.Join(parent, ".teak-agent-write-"+hex.EncodeToString(randomBytes[:]))
		tempFile, err := root.OpenFile(tempPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, permissions)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("create temporary file: %w", err)
		}
		if preservePermissions {
			if err := tempFile.Chmod(permissions); err != nil {
				_ = tempFile.Close()
				_ = root.Remove(tempPath)
				return nil, "", fmt.Errorf("set temporary file permissions: %w", err)
			}
		}
		return tempFile, tempPath, nil
	}

	return nil, "", fmt.Errorf("create temporary file: exhausted unique names")
}
