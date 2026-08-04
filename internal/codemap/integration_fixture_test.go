package codemap

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"teak/internal/toolpath"
)

func TestCodemapCommandsAgainstDeterministicFixture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "codemap-fixture")
	const script = `#!/bin/sh
case "$1" in
  status)
    [ "$2" = "--full" ] && [ "$3" = "--json" ] || exit 3
    printf '%s\n' '{"registered":true,"nodes":12,"edges":9,"files":3,"stale":{"changed":2,"new":1,"deleted":0}}'
    ;;
  structural-manifest) printf '%s\n' '{"schema_version":1,"export_schema_version":1,"project":"fixture","project_key":"0123456789ab","index_fingerprint":"fingerprint","total_records":12,"complete":true,"freshness":{"checked":true,"fresh":false,"changed":2,"new":1,"deleted":0}}' ;;
  context) printf '%s\n' '{"definitions":[{"symbol":"main","file":"main.go","start_line":1,"end_line":3}],"callers":[],"callees":[],"references":[],"tests":[]}' ;;
  *) printf '%s\n' 'unsupported fixture command' >&2; exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	status, err := FullStatus(WithFullStatus(context.Background()), root)
	if err != nil {
		t.Fatalf("FullStatus() error = %v", err)
	}
	if status.Nodes != 12 || status.Edges != 9 || status.Files != 3 ||
		status.Stale.Changed != 2 || status.Stale.New != 1 || status.Ready() {
		t.Fatalf("FullStatus() = %#v, want normalized stale fixture", status)
	}

	manifest, err := StructuralManifest(context.Background(), root)
	if err != nil {
		t.Fatalf("StructuralManifest() error = %v", err)
	}
	if manifest.TotalRecords != 12 || manifest.Freshness.Changed != 2 || manifest.Freshness.Fresh {
		t.Fatalf("StructuralManifest() = %#v, want normalized stale fixture", manifest)
	}

	result, err := Context(context.Background(), root, "main")
	if err != nil {
		t.Fatalf("Context() error = %v", err)
	}
	if len(result.Definitions) != 1 || result.Definitions[0].Symbol != "main" {
		t.Fatalf("Context() = %#v, want fixture definition", result)
	}
}

func TestEnsureReadySharesOneIndexBuildPerWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	aliasParent := t.TempDir()
	rootAlias := filepath.Join(aliasParent, "workspace-link")
	if err := os.Symlink(root, rootAlias); err != nil {
		t.Fatalf("symlink workspace: %v", err)
	}
	fixture := filepath.Join(t.TempDir(), "codemap-single-flight-fixture")
	const script = `#!/bin/sh
case "$1" in
  status)
    if [ -f .codemap-indexed ]; then
      printf '%s\n' '{"registered":true,"stale":{"changed":0,"new":0,"deleted":0}}'
      exit 0
    fi
    printf '%s\n' 'not a codemap project' >&2
    exit 1
    ;;
  structural-manifest)
    if [ -f .codemap-indexed ]; then
      printf '%s\n' '{"schema_version":1,"export_schema_version":1,"complete":true,"freshness":{"checked":true,"fresh":true,"changed":0,"new":0,"deleted":0}}'
      exit 0
    fi
    printf '%s\n' 'not a codemap project' >&2
    exit 1
    ;;
  init)
    printf 'init\n' >> .codemap-calls
    ;;
  index)
    printf 'index\n' >> .codemap-calls
    sleep 1
    touch .codemap-indexed
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() {
		CancelIndexing()
		_ = WaitForIndexingShutdown(context.Background())
		toolpath.Configure(nil)
	})

	const callers = 8
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	roots := []string{root, rootAlias}
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(workspace string) {
			defer wg.Done()
			errs <- EnsureReady(context.Background(), workspace)
		}(roots[i%len(roots)])
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("EnsureReady() error = %v", err)
		}
	}

	calls, err := os.ReadFile(filepath.Join(root, ".codemap-calls"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(calls)); got != "init\nindex" {
		t.Fatalf("fixture index calls = %q, want one init and one index across workspace aliases", got)
	}
	if !statusEventuallyReady(root) {
		t.Fatal("fixture did not become ready")
	}
}

func statusEventuallyReady(root string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		status, err := Status(ctx, root)
		if err == nil && status.Ready() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(10 * time.Millisecond):
		}
	}
}
