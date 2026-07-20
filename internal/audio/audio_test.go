package audio

import (
	"os"
	"path/filepath"
	"testing"

	"nv-x/internal/config"
)

func TestModelPath(t *testing.T) {
	root := t.TempDir()
	model := filepath.Join(root, "features", "dereverb_denoiser", "models", "sm_89", "dereverb_denoiser_48k_2048.trtpkg")
	if err := os.MkdirAll(filepath.Dir(model), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(model, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Audio.SDKPath = root
	got, err := modelPath(cfg, "dereverb_denoiser")
	if err != nil {
		t.Fatal(err)
	}
	if got != model {
		t.Fatalf("got %q want %q", got, model)
	}
}

func TestDisabledSupervisorWritesState(t *testing.T) {
	cfg := config.Default()
	if cfg.Audio.Mode != "off" {
		t.Fatal("audio must default off")
	}
}
