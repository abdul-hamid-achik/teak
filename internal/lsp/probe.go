package lsp

import (
	"context"
	"fmt"
	"time"

	"teak/internal/toolpath"
)

const protocolProbeCleanupTimeout = time.Second

// ProtocolProbeResult is the bounded evidence collected by an explicit LSP
// handshake. Capabilities are reported as stable names so headless callers do
// not need to understand the many boolean-or-options shapes in the protocol.
type ProtocolProbeResult struct {
	Capabilities []string
}

// ProbeProtocol starts a configured language server, completes only the LSP
// initialize/initialized handshake, and then shuts it down. It is intended for
// explicit health checks such as `teak doctor` when a server has no safe,
// non-interactive version command.
//
// The caller owns the probe deadline through ctx. Shutdown is always attempted
// before returning, and the client performs bounded process-group teardown so
// a server or shim cannot remain attached to Teak's pipes after a timeout.
func ProbeProtocol(ctx context.Context, cfg ServerConfig, rootDir string) error {
	_, err := ProbeProtocolCapabilities(ctx, cfg, rootDir)
	return err
}

// ProbeProtocolCapabilities starts a configured language server, completes
// the initialize/initialized handshake, and returns the capabilities declared
// by the server. It has the same bounded cleanup guarantees as ProbeProtocol.
func ProbeProtocolCapabilities(ctx context.Context, cfg ServerConfig, rootDir string) (ProtocolProbeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// A protocol health check has no consumer for notifications. A nil channel
	// keeps advisory diagnostics from building a second queue while the probe is
	// only interested in the initialize response.
	client, err := NewClient(cfg, rootDir, nil)
	if err != nil {
		return ProtocolProbeResult{}, err
	}
	forceCleanup := false
	defer func() {
		client.Shutdown()
		if forceCleanup {
			forceTerminateProbeClient(client)
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), protocolProbeCleanupTimeout)
		defer cancel()
		_ = client.WaitForShutdown(cleanupCtx)
	}()

	initialized := make(chan error, 1)
	go func() {
		initialized <- client.Initialize()
	}()

	select {
	case err := <-initialized:
		if err != nil {
			forceCleanup = true
			return ProtocolProbeResult{}, fmt.Errorf("initialize language server: %w", err)
		}
		return ProtocolProbeResult{Capabilities: CapabilityNames(client.Capabilities())}, nil
	case <-ctx.Done():
		forceCleanup = true
		return ProtocolProbeResult{}, ctx.Err()
	}
}

// CapabilityNames converts the initialize result into a stable, bounded list
// suitable for diagnostics and machine-facing contracts.
func CapabilityNames(caps ServerCapabilities) []string {
	capabilities := make([]string, 0, 11)
	appendIf := func(name string, enabled bool) {
		if enabled {
			capabilities = append(capabilities, name)
		}
	}
	appendIf("completion", caps.CompletionProvider != nil)
	appendIf("hover", capabilityEnabled(caps.HoverProvider))
	appendIf("definition", capabilityEnabled(caps.DefinitionProvider))
	appendIf("references", capabilityEnabled(caps.ReferencesProvider))
	appendIf("rename", capabilityEnabled(caps.RenameProvider))
	appendIf("code_actions", capabilityEnabled(caps.CodeActionProvider))
	appendIf("document_symbols", capabilityEnabled(caps.DocumentSymbolProvider))
	appendIf("formatting", capabilityEnabled(caps.FormattingProvider))
	appendIf("range_formatting", capabilityEnabled(caps.RangeFormattingProvider))
	appendIf("folding_range", capabilityEnabled(caps.FoldingRangeProvider))
	appendIf("signature_help", caps.SignatureHelpProvider != nil)
	return capabilities
}

func forceTerminateProbeClient(client *Client) {
	if client == nil {
		return
	}
	client.mu.RLock()
	cmd := client.cmd
	client.mu.RUnlock()
	_ = toolpath.TerminateCommand(cmd)
}
