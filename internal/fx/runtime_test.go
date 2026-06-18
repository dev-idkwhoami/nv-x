package fx

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMissingSharedLibrariesParsesLDDOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script test")
	}
	dir := t.TempDir()
	fakeLDD := filepath.Join(dir, "ldd")
	if err := os.WriteFile(fakeLDD, []byte("#!/usr/bin/env sh\ncat <<'EOF'\nlibcublasLt.so.12 => not found\nlibcudart.so.12 => not found\nlibc.so.6 => /usr/lib/libc.so.6\nEOF\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	env := os.Environ()
	missing := MissingSharedLibraries("/tmp/libVideoFX.so", env)
	if len(missing) != 2 || missing[0] != "libcublasLt.so.12" || missing[1] != "libcudart.so.12" {
		t.Fatalf("unexpected missing libraries: %#v", missing)
	}
}

func TestHelperValue(t *testing.T) {
	output := "sdk_version=1.2.0\nmaxine_smoke_ok=true\n"
	if got := helperValue(output, "sdk_version"); got != "1.2.0" {
		t.Fatalf("expected sdk version, got %q", got)
	}
	if got := helperValue(output, "missing"); got != "" {
		t.Fatalf("expected missing key to be empty, got %q", got)
	}
}
