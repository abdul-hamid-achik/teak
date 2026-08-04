package bob

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"teak/internal/toolpath"
)

func writeBobFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bob")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func configureBob(t *testing.T, fixture string) {
	t.Helper()
	toolpath.Configure(map[string]string{"bob": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })
}

func TestCheckParsesDriftResultWhenBobWritesStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	fixture := writeBobFixture(t, `
printf '%s\n' '{"ok":false,"drifted":["main.go"],"conflicts":[]}'
printf '%s\n' 'drift detected' >&2
exit 3
`)
	configureBob(t, fixture)

	result, err := Check(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Check() error = %v, want drift result", err)
	}
	if result.OK || len(result.Drifted) != 1 || result.Drifted[0] != "main.go" {
		t.Fatalf("Check() = %#v, want parsed drift result", result)
	}
}

func TestPlanRejectsOversizedOutputBeforeParsing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	fixture := writeBobFixture(t, `
head -c 5000000 /dev/zero
`)
	configureBob(t, fixture)

	_, err := Plan(context.Background(), t.TempDir())
	if !errors.Is(err, toolpath.ErrOutputLimit) {
		t.Fatalf("Plan() error = %v, want toolpath.ErrOutputLimit", err)
	}
}
