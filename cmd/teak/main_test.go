package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleCLI(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantFilePath string
		wantHandled  bool
		wantOutput   string
		wantErr      bool
	}{
		{
			name: "no arguments starts editor",
		},
		{
			name:         "file argument starts editor",
			args:         []string{"notes.md"},
			wantFilePath: "notes.md",
		},
		{
			name:         "additional arguments preserve first file behavior",
			args:         []string{"notes.md", "ignored.md"},
			wantFilePath: "notes.md",
		},
		{
			name:        "long version flag exits early",
			args:        []string{"--version"},
			wantHandled: true,
			wantOutput:  "teak 1.2.3-test\n",
		},
		{
			name:        "short version flag exits early",
			args:        []string{"-v"},
			wantHandled: true,
			wantOutput:  "teak 1.2.3-test\n",
		},
		{
			name:    "unknown long flag is rejected",
			args:    []string{"--verbose"},
			wantErr: true,
		},
		{
			name:    "unknown short flag is rejected",
			args:    []string{"-x"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer

			filePath, handled, err := handleCLI(tt.args, &stdout, "1.2.3-test")

			if (err != nil) != tt.wantErr {
				t.Fatalf("handleCLI() error = %v, wantErr %v", err, tt.wantErr)
			}
			if filePath != tt.wantFilePath {
				t.Errorf("handleCLI() filePath = %q, want %q", filePath, tt.wantFilePath)
			}
			if handled != tt.wantHandled {
				t.Errorf("handleCLI() handled = %v, want %v", handled, tt.wantHandled)
			}
			if got := stdout.String(); got != tt.wantOutput {
				t.Errorf("handleCLI() output = %q, want %q", got, tt.wantOutput)
			}
		})
	}
}

func TestDevelopmentVersionFallback(t *testing.T) {
	if version == "" {
		t.Fatal("version fallback must not be empty")
	}
}

func TestResolveWorkspacePathsUsesOneAbsoluteFileIdentity(t *testing.T) {
	// Use a marker-free temp tree so project-root walking does not pick up the
	// Teak repo itself when the relative path resolves under the working tree.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "relative", "main.go")
	resolved, root := resolveWorkspacePaths(filePath)

	if !filepath.IsAbs(resolved) {
		t.Fatalf("resolved path = %q, want absolute", resolved)
	}
	if resolved != filePath {
		t.Fatalf("resolved = %q, want %q", resolved, filePath)
	}
	// No project markers in the temp tree → root is the file's parent.
	if got, want := root, filepath.Dir(resolved); got != want {
		t.Fatalf("root = %q, want %q", got, want)
	}
}

func TestResolveWorkspacePathsOpensDirectoryAsWorkspace(t *testing.T) {
	dir := t.TempDir()
	// Nested folder so a wrong filepath.Dir() would point at the parent, not dir.
	workspace := filepath.Join(dir, "myproject")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	resolved, root := resolveWorkspacePaths(workspace)

	if resolved != "" {
		t.Fatalf("resolved file path = %q, want empty (directory open)", resolved)
	}
	if root != workspace {
		t.Fatalf("root = %q, want workspace directory %q", root, workspace)
	}
}

func TestResolveWorkspacePathsRelativeDirectory(t *testing.T) {
	// Create a temp dir, chdir into its parent, open the child by relative name.
	parent := t.TempDir()
	workspace := filepath.Join(parent, "target-folder")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(parent); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	resolved, root := resolveWorkspacePaths("target-folder")

	if resolved != "" {
		t.Fatalf("resolved file path = %q, want empty", resolved)
	}
	// Compare via Abs of the relative name so macOS /var vs /private/var matches.
	absWant, err := filepath.Abs("target-folder")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if root != absWant {
		t.Fatalf("root = %q, want %q (not parent cwd)", root, absWant)
	}
	// Guard against the old bug: root must be the folder itself, not its parent.
	if root == parent || filepath.Base(root) != "target-folder" {
		t.Fatalf("root = %q should be the target folder, not its parent %q", root, parent)
	}
}

func TestResolveWorkspacePathsFileUsesParentAsRoot(t *testing.T) {
	// No project markers → fall back to the file's parent directory.
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	resolved, root := resolveWorkspacePaths(file)

	if resolved != file {
		t.Fatalf("resolved = %q, want %q", resolved, file)
	}
	if root != dir {
		t.Fatalf("root = %q, want %q", root, dir)
	}
}

func TestResolveWorkspacePathsFileUsesProjectRoot(t *testing.T) {
	// repo/
	//   go.mod
	//   src/cmd/app/main.go  ← open this; workspace should be repo/, not src/cmd/app
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	nested := filepath.Join(repo, "src", "cmd", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(nested, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	resolved, root := resolveWorkspacePaths(file)

	if resolved != file {
		t.Fatalf("resolved = %q, want %q", resolved, file)
	}
	if root != repo {
		t.Fatalf("root = %q, want project root %q (not parent %q)", root, repo, nested)
	}
}

func TestResolveWorkspacePathsFileUsesNearestProjectRoot(t *testing.T) {
	// monorepo/
	//   .git/
	//   packages/web/
	//     package.json
	//     src/index.ts  ← nearest root is packages/web, not monorepo
	monorepo := t.TempDir()
	if err := os.Mkdir(filepath.Join(monorepo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	pkg := filepath.Join(monorepo, "packages", "web")
	if err := os.MkdirAll(filepath.Join(pkg, "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"web"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	file := filepath.Join(pkg, "src", "index.ts")
	if err := os.WriteFile(file, []byte("export {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, root := resolveWorkspacePaths(file)

	if root != pkg {
		t.Fatalf("root = %q, want nearest package root %q", root, pkg)
	}
}

func TestResolveWorkspacePathsFileUsesGitRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	nested := filepath.Join(repo, "internal", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(nested, "app.go")
	if err := os.WriteFile(file, []byte("package app\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, root := resolveWorkspacePaths(file)

	if root != repo {
		t.Fatalf("root = %q, want git root %q", root, repo)
	}
}

func TestResolveWorkspacePathsNewFileUsesProjectRoot(t *testing.T) {
	// Not-yet-created path still walks from its parent for project markers.
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "Cargo.toml"), []byte("[package]\nname = \"x\"\n"), 0o644); err != nil {
		t.Fatalf("write Cargo.toml: %v", err)
	}
	nested := filepath.Join(repo, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	newFile := filepath.Join(nested, "lib.rs") // does not exist

	resolved, root := resolveWorkspacePaths(newFile)

	if resolved != newFile {
		t.Fatalf("resolved = %q, want %q", resolved, newFile)
	}
	if root != repo {
		t.Fatalf("root = %q, want project root %q", root, repo)
	}
}

func TestFindProjectRootStopsAtFilesystemRoot(t *testing.T) {
	// A plain temp dir with no markers should not invent a root.
	dir := t.TempDir()
	if got := findProjectRoot(dir); got != "" {
		t.Fatalf("findProjectRoot(%q) = %q, want empty", dir, got)
	}
}

func TestResolveWorkspacePathsEmptyUsesCWD(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	resolved, root := resolveWorkspacePaths("")

	if resolved != "" {
		t.Fatalf("resolved = %q, want empty", resolved)
	}
	if root != cwd {
		t.Fatalf("root = %q, want cwd %q", root, cwd)
	}
}

func TestTerminalStartupError(t *testing.T) {
	tests := []struct {
		name          string
		stdinIsTTY    bool
		stdoutIsTTY   bool
		term          string
		wantErrorPart string
	}{
		{name: "interactive terminal", stdinIsTTY: true, stdoutIsTTY: true, term: "xterm-256color"},
		{name: "stdin is a pipe", stdoutIsTTY: true, term: "xterm-256color", wantErrorPart: "stdin"},
		{name: "stdout is a pipe", stdinIsTTY: true, term: "xterm-256color", wantErrorPart: "stdout"},
		{name: "dumb terminal", stdinIsTTY: true, stdoutIsTTY: true, term: " dumb ", wantErrorPart: "TERM=dumb"},
		{name: "unset term remains supported", stdinIsTTY: true, stdoutIsTTY: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := terminalStartupError(tt.stdinIsTTY, tt.stdoutIsTTY, tt.term)
			if tt.wantErrorPart == "" {
				if err != nil {
					t.Fatalf("terminalStartupError() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErrorPart) {
				t.Fatalf("terminalStartupError() error = %v, want containing %q", err, tt.wantErrorPart)
			}
		})
	}
}
