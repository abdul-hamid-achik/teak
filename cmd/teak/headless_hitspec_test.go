package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"teak/internal/toolpath"
)

func TestHeadlessHitspecValidateReportsValidFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "api.http")
	if err := os.WriteFile(path, []byte("GET https://example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "hitspec")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nprintf '%s\\n' '[{\"file\":\"api.http\",\"ok\":true}]'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"hitspec": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"hitspec", "validate", "--json", "--root", root, "api.http"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("headless hitspec exit code = %d, stderr = %s", code, stderr.String())
	}
	var response headlessHitspecValidationResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode hitspec response: %v; output=%s", err, stdout.String())
	}
	if response.State != "ready" || !response.Valid || response.Files != 1 || len(response.Results) != 1 {
		t.Fatalf("hitspec response = %#v, want one valid result", response)
	}
}

func TestHeadlessHitspecValidateReportsInvalidFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "api.http")
	if err := os.WriteFile(path, []byte("not a request\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "hitspec")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nprintf '%s\\n' '[{\"file\":\"api.http\",\"ok\":false,\"errors\":[\"no requests found\"]}]'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"hitspec": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"hitspec", "validate", "--json", "--root", root, "api.http"}, nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("headless hitspec exit code = %d, want 1; stderr=%s", code, stderr.String())
	}
	var response headlessHitspecValidationResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode hitspec response: %v; output=%s", err, stdout.String())
	}
	if response.State != "invalid" || response.Valid || len(response.Results) != 1 || len(response.Results[0].Errors) != 1 {
		t.Fatalf("hitspec response = %#v, want one invalid result", response)
	}
}

func TestHeadlessHitspecValidateRejectsPathOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "api.http")
	if err := os.WriteFile(outside, []byte("GET https://example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"hitspec", "validate", "--json", "--root", root, outside}, nil, &stdout, &stderr)
	if code == 0 || !bytes.Contains(stderr.Bytes(), []byte("outside workspace")) {
		t.Fatalf("headless hitspec outside path code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestHeadlessHitspecValidateRejectsMalformedResultFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "api.http")
	if err := os.WriteFile(path, []byte("GET https://example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "hitspec")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nprintf '%s\\n' '[{\"file\":\"\",\"ok\":true}]'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"hitspec": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"hitspec", "validate", "--json", "--root", root, "api.http"}, nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("headless hitspec malformed result code = %d, want 1; stderr=%s", code, stderr.String())
	}
	var response headlessHitspecValidationResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode malformed hitspec response: %v; output=%s", err, stdout.String())
	}
	if response.Valid || response.State != "failed" || !bytes.Contains([]byte(response.Detail), []byte("output contract")) {
		t.Fatalf("malformed hitspec response = %#v, want failed contract response", response)
	}
}

func TestHeadlessHitspecValidateRejectsResultOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "api.http")
	if err := os.WriteFile(path, []byte("GET https://example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside.http")
	if err := os.WriteFile(outside, []byte("GET https://outside.example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	fixture := filepath.Join(t.TempDir(), "hitspec")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nprintf '%s\\n' '[{\"file\":\"../outside.http\",\"ok\":true}]'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"hitspec": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"hitspec", "validate", "--json", "--root", root, "api.http"}, nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("headless hitspec outside result code = %d, want 1; stderr=%s", code, stderr.String())
	}
	var response headlessHitspecValidationResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode outside hitspec response: %v; output=%s", err, stdout.String())
	}
	if response.Valid || response.State != "failed" || !bytes.Contains([]byte(response.Detail), []byte("outside workspace")) {
		t.Fatalf("outside hitspec response = %#v, want failed workspace-bound response", response)
	}
}

func TestHeadlessHitspecValidateRejectsMissingResultFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "api.http")
	if err := os.WriteFile(path, []byte("GET https://example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "hitspec")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nprintf '%s\\n' '[{\"file\":\"missing.http\",\"ok\":true}]'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"hitspec": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"hitspec", "validate", "--json", "--root", root, "."}, nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("headless hitspec missing result code = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var response headlessHitspecValidationResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode missing-result hitspec response: %v; output=%s", err, stdout.String())
	}
	if response.Valid || response.State != "failed" || !bytes.Contains([]byte(response.Detail), []byte("does not exist")) {
		t.Fatalf("missing-result hitspec response = %#v, want failed missing-file contract response", response)
	}
}

func TestHeadlessHitspecValidatePropagatesParentCancellationAsStructuredResponse(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "api.http")
	if err := os.WriteFile(path, []byte("GET https://example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "hitspec")
	script := "#!/bin/sh\nwhile :; do sleep 1; done\n"
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"hitspec": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLIContext(ctx, []string{"hitspec", "validate", "--json", "--root", root, "api.http"}, nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("cancelled hitspec exit code = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("cancelled hitspec wrote unstructured stderr: %s", stderr.String())
	}
	var response headlessHitspecValidationResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode cancelled hitspec response: %v; output=%s", err, stdout.String())
	}
	if response.State != "cancelled" || response.Valid {
		t.Fatalf("cancelled hitspec response = %#v, want structured cancelled state", response)
	}
}
