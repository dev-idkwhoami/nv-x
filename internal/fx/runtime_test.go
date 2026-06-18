package fx

import (
	"path/filepath"
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
