package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"teak/internal/toolpath"
)

const (
	agentDialTimeout    = 200 * time.Millisecond
	agentRequestTimeout = time.Second
	cliTimeout          = 5 * time.Second
	maxAgentResponse    = 1 << 20
	maxCLIOutput        = 1 << 20
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
	if val, err := r.agentGet(ctx, project, key); err == nil {
		return val, nil
	} else if ctx != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	// Fall back to CLI
	return r.cliGet(ctx, project, key)
}

// GetAll retrieves all secrets for a project as a map.
func (r *Resolver) GetAll(ctx context.Context, project string) (map[string]string, error) {
	// Try agent socket first
	if vals, err := r.agentGetAll(ctx, project); err == nil {
		return vals, nil
	} else if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
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
	_ = conn.Close()
	return true
}

func (r *Resolver) dialAgent(ctx context.Context) (net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dialCtx, cancel := context.WithTimeout(ctx, agentDialTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", r.agentSocketPath())
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(agentRequestTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (r *Resolver) agentGet(ctx context.Context, project, key string) (string, error) {
	conn, err := r.dialAgent(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()

	req := fmt.Sprintf(`{"op":"get","project":%q,"key":%q}`+"\n", project, key)
	if _, err := conn.Write([]byte(req)); err != nil {
		return "", err
	}

	var resp struct {
		Value string `json:"value"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(conn, maxAgentResponse+1)).Decode(&resp); err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", fmt.Errorf("tvault agent: %s", resp.Error)
	}
	return resp.Value, nil
}

func (r *Resolver) agentGetAll(ctx context.Context, project string) (map[string]string, error) {
	conn, err := r.dialAgent(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	req := fmt.Sprintf(`{"op":"getall","project":%q}`+"\n", project)
	if _, err := conn.Write([]byte(req)); err != nil {
		return nil, err
	}

	var resp struct {
		Secrets map[string]string `json:"secrets"`
		Error   string            `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(conn, maxAgentResponse+1)).Decode(&resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("tvault agent: %s", resp.Error)
	}
	return resp.Secrets, nil
}

func (r *Resolver) cliGet(ctx context.Context, project, key string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()

	cmd, err := toolpath.Command(ctx, "tvault", "get", project, key, "--format", "plain")
	if err != nil {
		return "", err
	}
	out, stderr, err := toolpath.RunBounded(cmd, maxCLIOutput, maxCLIOutput)
	if err != nil {
		if detail := strings.TrimSpace(string(stderr)); detail != "" {
			return "", fmt.Errorf("tvault get: %w: %s", err, detail)
		}
		return "", fmt.Errorf("tvault get: %w", err)
	}
	return string(out), nil
}

func (r *Resolver) cliGetAll(ctx context.Context, project string) (map[string]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()

	cmd, err := toolpath.Command(ctx, "tvault", "env", project, "--format", "json")
	if err != nil {
		return nil, err
	}
	out, stderr, err := toolpath.RunBounded(cmd, maxCLIOutput, maxCLIOutput)
	if err != nil {
		if detail := strings.TrimSpace(string(stderr)); detail != "" {
			return nil, fmt.Errorf("tvault env: %w: %s", err, detail)
		}
		return nil, fmt.Errorf("tvault env: %w", err)
	}
	var env map[string]string
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("parse tvault env: %w", err)
	}
	return env, nil
}
