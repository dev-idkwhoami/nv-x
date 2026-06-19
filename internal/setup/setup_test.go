package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nv-vcam/internal/config"
)

func TestEnsureSDKDryRunPlansBuiltInNGCDownload(t *testing.T) {
	cfg := config.Default()
	cfg.FX.SDKPath = filepath.Join(t.TempDir(), "VideoFX")
	if err := ensureSDK(context.Background(), cfg, Options{DryRun: true}); err != nil {
		t.Fatalf("expected dry-run NGC SDK setup to succeed, got %v", err)
	}
}

func TestRunAsRootRequiresSkippingService(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root guard only applies under root")
	}
	err := Run(context.Background(), config.Default(), Options{})
	if err == nil || !strings.Contains(err.Error(), "do not run nv-vcam setup with sudo") {
		t.Fatalf("expected root guard error, got %v", err)
	}
}

func TestNeedsSudoForPrivilegedSetupSteps(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root never needs sudo")
	}
	if !needsSudo(Options{}) {
		t.Fatal("default setup should validate sudo")
	}
	if needsSudo(Options{SkipSDK: true, SkipMaxine: true, SkipLoopback: true}) {
		t.Fatal("setup without privileged steps should not validate sudo")
	}
}

func TestFindSDKTarball(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "resource")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(nested, "VFXSDK_linux_1.2.0.0.tgz")
	if err := os.WriteFile(want, []byte("tarball"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := findSDKTarball(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestRunStreamingReturnsCommandErrors(t *testing.T) {
	err := runStreaming(context.Background(), "", "sh", "-c", "exit 7")
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected streaming command failure, got %v", err)
	}
}

func TestIsServiceNotLoaded(t *testing.T) {
	err := errors.New("systemctl --user stop nv-vcam.service failed: exit status 5\nFailed to stop nv-vcam.service: Unit nv-vcam.service not loaded.")
	if !isServiceNotLoaded(err) {
		t.Fatal("expected not-loaded systemd error to be recognized")
	}
}
