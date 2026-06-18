package fx

import (
	"strings"
	"testing"

	"nv-vcam/internal/config"
)

func TestFXInputFFmpegArgs(t *testing.T) {
	opts := StreamOptions{
		InputDevice:  "/dev/video10",
		OutputDevice: "/dev/video20",
		Width:        2560,
		Height:       1440,
		FPS:          25,
	}
	args := strings.Join(FXInputFFmpegArgs(opts), " ")
	for _, want := range []string{
		"-f v4l2",
		"-framerate 25",
		"-video_size 2560x1440",
		"-i /dev/video10",
		"format=bgr24",
		"-pix_fmt bgr24",
		"-f rawvideo -",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("expected %q in args:\n%s", want, args)
		}
	}
}

func TestFXOutputFFmpegArgs(t *testing.T) {
	opts := StreamOptions{
		OutputDevice: "/dev/video20",
		Width:        2560,
		Height:       1440,
		FPS:          25,
	}
	args := strings.Join(FXOutputFFmpegArgs(opts), " ")
	for _, want := range []string{
		"-f rawvideo",
		"-pix_fmt bgr24",
		"-s 2560x1440",
		"-r 25",
		"format=yuv420p",
		"-f v4l2 /dev/video20",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("expected %q in args:\n%s", want, args)
		}
	}
}

func TestNormalizeStreamOptionsUsesConfigDefaults(t *testing.T) {
	cfg := config.Default()
	opts := normalizeStreamOptions(cfg, StreamOptions{})
	if opts.InputDevice != "/dev/video10" || opts.OutputDevice != "/dev/video20" {
		t.Fatalf("unexpected devices: %+v", opts)
	}
	if opts.Width != 2560 || opts.Height != 1440 || opts.FPS != 25 || opts.BackgroundMode != "blur" || opts.BackgroundImage != "" || opts.ChromaColor != "#00ff00" || opts.BlurStrength != 0.75 || opts.DenoiseEnabled || opts.DenoiseStrength != 0 {
		t.Fatalf("unexpected geometry/effect defaults: %+v", opts)
	}
}

func TestValidateStreamOptionsRejectsSmallFrames(t *testing.T) {
	err := validateStreamOptions(StreamOptions{
		InputDevice:  "/dev/video10",
		OutputDevice: "/dev/video20",
		Width:        320,
		Height:       240,
		FPS:          25,
	})
	if err == nil {
		t.Fatal("expected small frame size error")
	}
}

func TestValidateStreamOptionsRejectsInvalidEffects(t *testing.T) {
	err := validateStreamOptions(StreamOptions{
		InputDevice:     "/dev/video10",
		OutputDevice:    "/dev/video20",
		Width:           2560,
		Height:          1440,
		FPS:             25,
		BackgroundMode:  "matte",
		DenoiseStrength: 0,
	})
	if err == nil {
		t.Fatal("expected invalid background mode error")
	}
	err = validateStreamOptions(StreamOptions{
		InputDevice:    "/dev/video10",
		OutputDevice:   "/dev/video20",
		Width:          2560,
		Height:         1440,
		FPS:            25,
		BackgroundMode: "replace",
	})
	if err == nil {
		t.Fatal("expected missing replacement image error")
	}
	err = validateStreamOptions(StreamOptions{
		InputDevice:     "/dev/video10",
		OutputDevice:    "/dev/video20",
		Width:           2560,
		Height:          1440,
		FPS:             25,
		BackgroundMode:  "blur",
		DenoiseStrength: 3,
	})
	if err == nil {
		t.Fatal("expected invalid denoise strength error")
	}
	err = validateStreamOptions(StreamOptions{
		InputDevice:     "/dev/video10",
		OutputDevice:    "/dev/video20",
		Width:           2560,
		Height:          1440,
		FPS:             25,
		BackgroundMode:  "chroma",
		ChromaColor:     "00ff00",
		DenoiseStrength: 0,
	})
	if err == nil {
		t.Fatal("expected invalid chroma color error")
	}
	err = validateStreamOptions(StreamOptions{
		InputDevice:     "/dev/video10",
		OutputDevice:    "/dev/video20",
		Width:           2560,
		Height:          1440,
		FPS:             25,
		BackgroundMode:  "blur",
		DenoiseEnabled:  true,
		DenoiseStrength: 0,
	})
	if err == nil {
		t.Fatal("expected denoise max height error")
	}
}

func TestInputOwnedPIDsMergesFXAndRAWOwnedPIDs(t *testing.T) {
	supervisor := NewSupervisor(config.Default(), nil)
	supervisor.owned = map[int]bool{100: true}
	supervisor.SetInputIgnorePIDsFunc(func() map[int]bool {
		return map[int]bool{200: true}
	})
	got := supervisor.inputOwnedPIDs()
	if !got[100] || !got[200] || len(got) != 2 {
		t.Fatalf("unexpected owned pid set: %#v", got)
	}
}
