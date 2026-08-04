package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyHeadlessProjectFileEnforcesLiveByteBudget(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "source.txt"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	budget := headlessProjectCopyBudget{maxNodes: 2, maxBytes: 3}
	_, err = copyHeadlessProjectFile(context.Background(), root, "source.txt", "destination.txt", 0o600, &budget)
	if err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("copy over live byte budget error = %v, want byte-limit error", err)
	}
	if budget.bytes != 0 {
		t.Fatalf("live byte budget consumed %d bytes after rejected chunk, want 0", budget.bytes)
	}
	if _, statErr := root.Lstat("destination.txt"); statErr != nil {
		t.Fatalf("copy should leave a cleanup target after rejection: %v", statErr)
	}
}

func runHeadlessProjectTestCommand(t *testing.T, args ...string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI(args, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("headless project command %v exit code = %d, stderr=%s, stdout=%s", args, code, stderr.String(), stdout.String())
	}
	return stdout.Bytes()
}

func TestRunHeadlessProjectMutationRequiresConfirmation(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(target, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"project", "remove", "--json", "--root", root, "keep.txt"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("project remove without confirmation exit code = %d, want 2; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var response headlessErrorResponse
	if err := json.Unmarshal(stderr.Bytes(), &response); err != nil {
		t.Fatalf("decode confirmation error: %v; stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	if response.Code != "invalid_argument" || !strings.Contains(response.Message, "--confirm") {
		t.Fatalf("confirmation error = %#v, want invalid_argument mentioning --confirm", response)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("target after rejected mutation: %v", err)
	}
}

func TestRunHeadlessProjectMutationsAndList(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "nested", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var mkdir headlessProjectResponse
	if err := json.Unmarshal(runHeadlessProjectTestCommand(t, "project", "mkdir", "--confirm", "--json", "--root", root, "generated"), &mkdir); err != nil {
		t.Fatal(err)
	}
	if mkdir.Operation != "mkdir" || !mkdir.Committed || mkdir.Nodes != 1 {
		t.Fatalf("mkdir response = %#v", mkdir)
	}
	var rename headlessProjectResponse
	if err := json.Unmarshal(runHeadlessProjectTestCommand(t, "project", "rename", "--confirm", "--json", "--root", root, "src", "source"), &rename); err != nil {
		t.Fatal(err)
	}
	if rename.Operation != "rename" || !rename.Committed || rename.Source != "src" || rename.Destination != "source" {
		t.Fatalf("rename response = %#v", rename)
	}
	if rename.Nodes != 3 || rename.Bytes != int64(len("package main\n")) {
		t.Fatalf("rename counts = nodes:%d bytes:%d, want nodes:3 bytes:%d", rename.Nodes, rename.Bytes, len("package main\n"))
	}
	var copyResponse headlessProjectResponse
	if err := json.Unmarshal(runHeadlessProjectTestCommand(t, "project", "copy", "--confirm", "--json", "--root", root, "source", "backup"), &copyResponse); err != nil {
		t.Fatal(err)
	}
	if copyResponse.Operation != "copy" || !copyResponse.Committed || copyResponse.Nodes < 2 || copyResponse.Bytes == 0 {
		t.Fatalf("copy response = %#v", copyResponse)
	}
	var stat headlessProjectStatResponse
	if err := json.Unmarshal(runHeadlessProjectTestCommand(t, "project", "stat", "--json", "--root", root, "backup/nested/main.go"), &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Kind != "file" || stat.RelativePath != "backup/nested/main.go" || stat.Bytes == 0 {
		t.Fatalf("stat response = %#v", stat)
	}
	var list headlessContextResponse
	if err := json.Unmarshal(runHeadlessProjectTestCommand(t, "project", "list", "--depth", "2", "--json", "--root", root), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Entries) == 0 || list.Truncated {
		t.Fatalf("list response = %#v", list)
	}
	seen := make(map[string]bool)
	for _, entry := range list.Entries {
		seen[entry.Path] = true
	}
	for _, path := range []string{"source", "source/nested", "source/nested/main.go", "backup", "backup/nested", "backup/nested/main.go", "generated"} {
		if !seen[path] {
			t.Fatalf("list missing %q: %#v", path, list.Entries)
		}
	}
	var removed headlessProjectResponse
	if err := json.Unmarshal(runHeadlessProjectTestCommand(t, "project", "remove", "--confirm", "--json", "--root", root, "backup"), &removed); err != nil {
		t.Fatal(err)
	}
	if removed.Operation != "remove" || !removed.Committed || removed.Nodes < 2 {
		t.Fatalf("remove response = %#v", removed)
	}
	if _, err := os.Stat(filepath.Join(root, "backup")); !os.IsNotExist(err) {
		t.Fatalf("backup after remove: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "source", "nested", "main.go")); err != nil {
		t.Fatalf("renamed source after copy/remove: %v", err)
	}
}

func TestRunHeadlessProjectRejectsSymlinkCopyAndTraversal(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"project", "copy", "--confirm", "--json", "--root", root, "linked", "copied"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("symlink copy exit code = %d, want 2; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "symlink") && !strings.Contains(stderr.String(), "symlink") {
		t.Fatalf("symlink copy error = stdout:%s stderr:%s", stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runHeadlessCLI([]string{"project", "mkdir", "--confirm", "--json", "--root", root, "../escape"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("traversal mkdir exit code = %d, want 2; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(outside, "escape")); !os.IsNotExist(err) {
		t.Fatalf("traversal created outside path: %v", err)
	}
}

func TestRunHeadlessProjectRejectsCopyIntoSource(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "nested", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"project", "copy", "--confirm", "--json", "--root", root, "src", "src/backup"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("copy into source exit code = %d, want 2; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "inside itself") {
		t.Fatalf("copy into source error = %s, want inside-itself diagnostic", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "src", "backup")); !os.IsNotExist(err) {
		t.Fatalf("copy into source created destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "nested", "main.go")); err != nil {
		t.Fatalf("source after rejected copy: %v", err)
	}
}
