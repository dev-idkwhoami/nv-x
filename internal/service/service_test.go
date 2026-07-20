package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderUnitUsesResolvedExecutable(t *testing.T) {
	unit := RenderUnit("/usr/bin/nv-x")
	if !strings.Contains(unit, `ExecStart="/usr/bin/nv-x" run`) {
		t.Fatalf("unit does not use resolved executable:\n%s", unit)
	}
}

func TestResolveExecPathUsesConfiguredExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nv-x")
	if err := os.WriteFile(path, []byte("test"), 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveExecPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != path {
		t.Fatalf("expected %q, got %q", path, resolved)
	}
}
