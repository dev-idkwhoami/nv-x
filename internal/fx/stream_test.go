package fx

import (
	"strings"
	"testing"

	"nv-vcam/internal/config"
)

func TestNormalizeStreamOptionsUsesNativeDefaults(t *testing.T) {
	cfg := config.Default()
	opts := normalizeStreamOptions(cfg, StreamOptions{})
	if opts.InputDevice != "/dev/video0" || opts.InputFormat != "nv12" {
		t.Fatalf("unexpected input defaults: %+v", opts)
	}
	if opts.OutputDevice != "/dev/video10" || opts.OutputFormat != "yuv420p" {
		t.Fatalf("unexpected output defaults: %+v", opts)
	}
	if opts.Width != 1920 || opts.Height != 1080 || opts.FPS != 50 || opts.BackgroundMode != "blur" || opts.BackgroundImage != "" || opts.ChromaColor != "#00ff00" || opts.BlurStrength != 0.75 || opts.DenoiseEnabled || opts.DenoiseStrength != 0 {
		t.Fatalf("unexpected geometry/effect defaults: %+v", opts)
	}
}

func TestNativeStreamHelperArgs(t *testing.T) {
	args := strings.Join(NativeStreamHelperArgs(DoctorResult{
		SDKPath:  "/opt/VideoFX",
		ModelDir: "/opt/VideoFX/models",
	}, StreamOptions{
		InputDevice:     "/dev/video0",
		InputFormat:     "nv12",
		OutputDevice:    "/dev/video10",
		OutputFormat:    "yuv420p",
		Width:           1920,
		Height:          1080,
		FPS:             50,
		BackgroundMode:  "chroma",
		ChromaColor:     "#00ff00",
		BlurStrength:    0.75,
		DenoiseStrength: 0,
	}, ""), " ")
	for _, want := range []string{
		"native-stream",
		"--sdk-path /opt/VideoFX",
		"--model-dir /opt/VideoFX/models",
		"--input-device /dev/video0",
		"--input-format nv12",
		"--output-device /dev/video10",
		"--output-format yuv420p",
		"--width 1920",
		"--height 1080",
		"--fps 50",
		"--background chroma",
		"--chroma-color #00ff00",
		"--blur-strength 0.750",
		"--denoise 0",
		"--denoise-strength 0",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("expected %q in args:\n%s", want, args)
		}
	}
}

func TestIdleOutputHelperArgs(t *testing.T) {
	args := strings.Join(IdleOutputHelperArgs(DoctorResult{}, StreamOptions{
		OutputDevice: "/dev/video10",
		OutputFormat: "yuv420p",
		Width:        1920,
		Height:       1080,
		FPS:          50,
	}), " ")
	for _, want := range []string{
		"idle-output",
		"--output-device /dev/video10",
		"--output-format yuv420p",
		"--width 1920",
		"--height 1080",
		"--fps 50",
		"--idle-label NV-vCam idling ...",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("expected %q in args:\n%s", want, args)
		}
	}
}

func TestValidateStreamOptionsRejectsSmallFrames(t *testing.T) {
	err := validateStreamOptions(StreamOptions{
		InputDevice:    "/dev/video0",
		InputFormat:    "nv12",
		OutputDevice:   "/dev/video10",
		OutputFormat:   "yuv420p",
		Width:          320,
		Height:         240,
		FPS:            50,
		BackgroundMode: "blur",
		ChromaColor:    "#00ff00",
	})
	if err == nil {
		t.Fatal("expected small frame size error")
	}
}

func TestValidateStreamOptionsRejectsInvalidEffects(t *testing.T) {
	base := StreamOptions{
		InputDevice:     "/dev/video0",
		InputFormat:     "nv12",
		OutputDevice:    "/dev/video10",
		OutputFormat:    "yuv420p",
		Width:           1920,
		Height:          1080,
		FPS:             50,
		BackgroundMode:  "blur",
		ChromaColor:     "#00ff00",
		DenoiseStrength: 0,
	}
	opts := base
	opts.BackgroundMode = "matte"
	if err := validateStreamOptions(opts); err == nil {
		t.Fatal("expected invalid background mode error")
	}
	opts = base
	opts.BackgroundMode = "replace"
	if err := validateStreamOptions(opts); err == nil {
		t.Fatal("expected missing replacement image error")
	}
	opts = base
	opts.DenoiseStrength = 3
	if err := validateStreamOptions(opts); err == nil {
		t.Fatal("expected invalid denoise strength error")
	}
	opts = base
	opts.BackgroundMode = "chroma"
	opts.ChromaColor = "00ff00"
	if err := validateStreamOptions(opts); err == nil {
		t.Fatal("expected invalid chroma color error")
	}
	opts = base
	opts.Height = 1440
	opts.DenoiseEnabled = true
	if err := validateStreamOptions(opts); err == nil {
		t.Fatal("expected denoise max height error")
	}
}

func TestValidateStreamOptionsRejectsUnsupportedFormats(t *testing.T) {
	opts := normalizeStreamOptions(config.Default(), StreamOptions{})
	opts.InputFormat = "mjpeg"
	if err := validateStreamOptions(opts); err == nil {
		t.Fatal("expected invalid input format error")
	}
	opts = normalizeStreamOptions(config.Default(), StreamOptions{})
	opts.OutputFormat = "nv12"
	if err := validateStreamOptions(opts); err == nil {
		t.Fatal("expected invalid output format error")
	}
}
