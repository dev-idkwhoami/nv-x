package capture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nv-vcam/internal/config"
)

func TestCaptureFFmpegArgsUsesCudaScale(t *testing.T) {
	cfg := config.Default().Capture
	args := strings.Join(CaptureFFmpegArgs(cfg), " ")
	if !strings.Contains(args, "scale_cuda=2560:1440:format=yuv420p") {
		t.Fatalf("expected CUDA scale args, got:\n%s", args)
	}
	if !strings.Contains(args, "-f v4l2 /dev/video10") {
		t.Fatalf("expected v4l2 output, got:\n%s", args)
	}
}

func TestCaptureFFmpegArgsUsesCPUScale(t *testing.T) {
	cfg := config.Default().Capture
	cfg.UseCUDAScale = false
	args := strings.Join(CaptureFFmpegArgs(cfg), " ")
	if strings.Contains(args, "scale_cuda") {
		t.Fatalf("did not expect CUDA scale args:\n%s", args)
	}
	if !strings.Contains(args, "scale=2560:1440") {
		t.Fatalf("expected CPU scale args, got:\n%s", args)
	}
}

func TestIdleFFmpegArgsIncludesDrawText(t *testing.T) {
	cfg := config.Default().Capture
	args := strings.Join(IdleFFmpegArgs(cfg, true), " ")
	if !strings.Contains(args, "drawtext") || !strings.Contains(args, "nv-vcam idling") {
		t.Fatalf("expected drawtext idle args, got:\n%s", args)
	}
}

func TestIdleFFmpegArgsMatchesCaptureFormat(t *testing.T) {
	cfg := config.Default().Capture
	args := strings.Join(IdleFFmpegArgs(cfg, false), " ")
	if !strings.Contains(args, "s=2560x1440:r=25") {
		t.Fatalf("expected idle stream to match capture geometry/fps, got:\n%s", args)
	}
}

func TestCountExternalConsumersExcludesOwnedPIDs(t *testing.T) {
	root := t.TempDir()
	device := filepath.Join(root, "video10")
	if err := os.WriteFile(device, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	makeFD := func(pid, fd string) {
		fdDir := filepath.Join(root, pid, "fd")
		if err := os.MkdirAll(fdDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(device, filepath.Join(fdDir, fd)); err != nil {
			t.Fatal(err)
		}
	}
	makeFD("100", "3")
	makeFD("200", "4")

	got := CountExternalConsumers(root, device, map[int]bool{100: true})
	if got != 1 {
		t.Fatalf("expected one external consumer, got %d", got)
	}
}
