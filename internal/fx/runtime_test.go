package fx

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"nv-vcam/internal/config"
)

func TestDoctorReportsMissingRuntime(t *testing.T) {
	cfg := config.Default()
	cfg.FX.ONNXRuntimeLibraryPath = filepath.Join(t.TempDir(), "missing.so")
	result := Doctor(cfg)
	if result.RuntimeOK {
		t.Fatal("expected runtime check to fail")
	}
	if result.Message == "" {
		t.Fatal("expected diagnostic message")
	}
}

func TestResolveRuntimeLibraryUsesConfiguredPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "libonnxruntime.so")
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveRuntimeLibrary(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("expected %q, got %q", path, got)
	}
}

func TestMissingSharedLibrariesParsesLDDOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script test")
	}
	dir := t.TempDir()
	fakeLDD := filepath.Join(dir, "ldd")
	if err := os.WriteFile(fakeLDD, []byte("#!/usr/bin/env sh\ncat <<'EOF'\nlibcublasLt.so.12 => not found\nlibcudart.so.12 => not found\nlibc.so.6 => /usr/lib/libc.so.6\nEOF\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	missing := MissingSharedLibraries("/tmp/libonnxruntime_providers_cuda.so")
	if len(missing) != 2 || missing[0] != "libcublasLt.so.12" || missing[1] != "libcudart.so.12" {
		t.Fatalf("unexpected missing libraries: %#v", missing)
	}
}
