package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"teak/internal/toolpath"
)

const (
	agentDialTimeout = 200 * time.Millisecond
	cliTimeout       = 5 * time.Second
)

// Resolver resolves secrets from TinyVault, trying the agent socket first
// and falling back to the CLI.
type Resolver struct {
	agentDir string
}

// NewResolver creates a secret resolver. The agent socket is expected at
// ~/.tvault/agent.sock (or the dir containing it).
func NewResolver() *Resolver {
	home, _ := os.UserHomeDir()
	return &Resolver{
		agentDir: filepath.Join(home, ".tvault"),
	}
}

// Available returns true if tvault is accessible (agent socket or CLI).
func (r *Resolver) Available() bool {
	if r.agentSocketAvailable() {
		return true
	}
	return toolpath.Available("tvault")
}

// Get retrieves a single secret value.
func (r *Resolver) Get(ctx context.Context, project, key string) (string, error) {
	// Try agent socket first (prompt-free, fast)
	if val, err := r.agentGet(project, key); err == nil {
		return val, nil
	}
	// Fall back to CLI
	return r.cliGet(ctx, project, key)
}

// GetAll retrieves all secrets for a project as a map.
func (r *Resolver) GetAll(ctx context.Context, project string) (map[string]string, error) {
	// Try agent socket first
	if vals, err := r.agentGetAll(project); err == nil {
		return vals, nil
	}
	// Fall back to CLI
	return r.cliGetAll(ctx, project)
}

// ResolveEnv resolves a list of key names from a vault project into env vars.
func (r *Resolver) ResolveEnv(ctx context.Context, project string, keys []string) (map[string]string, error) {
	if project == "" || len(keys) == 0 {
		return nil, nil
	}
	all, err := r.GetAll(ctx, project)
	if err != nil {
		return nil, err
	}
	env := make(map[string]string, len(keys))
	for _, key := range keys {
		if val, ok := all[key]; ok {
			env[key] = val
		}
	}
	return env, nil
}

func (r *Resolver) agentSocketPath() string {
	return filepath.Join(r.agentDir, "agent.sock")
}

func (r *Resolver) agentSocketAvailable() bool {
	conn, err := net.DialTimeout("unix", r.agentSocketPath(), agentDialTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (r *Resolver) agentGet(project, key string) (string, error) {
	conn, err := net.DialTimeout("unix", r.agentSocketPath(), agentDialTimeout)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	req := fmt.Sprintf(`{"op":"get","project":%q,"key":%q}`+"\n", project, key)
	if _, err := conn.Write([]byte(req)); err != nil {
		return "", err
	}

	var resp struct {
		Value string `json:"value"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", fmt.Errorf("tvault agent: %s", resp.Error)
	}
	return resp.Value, nil
}

func (r *Resolver) agentGetAll(project string) (map[string]string, error) {
	conn, err := net.DialTimeout("unix", r.agentSocketPath(), agentDialTimeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req := fmt.Sprintf(`{"op":"getall","project":%q}`+"\n", project)
	if _, err := conn.Write([]byte(req)); err != nil {
		return nil, err
	}

	var resp struct {
		Secrets map[string]string `json:"secrets"`
		Error   string            `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("tvault agent: %s", resp.Error)
	}
	return resp.Secrets, nil
}

func (r *Resolver) cliGet(ctx context.Context, project, key string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()

	cmd, err := toolpath.Command(ctx, "tvault", "get", project, key, "--format", "plain")
	if err != nil {
		return "", err
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tvault get: %w", err)
	}
	return string(out), nil
}

func (r *Resolver) cliGetAll(ctx context.Context, project string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()

	cmd, err := toolpath.Command(ctx, "tvault", "env", project, "--format", "json")
	if err != nil {
		return nil, err
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tvault env: %w", err)
	}
	var env map[string]string
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("parse tvault env: %w", err)
	}
	return env, nil
}
