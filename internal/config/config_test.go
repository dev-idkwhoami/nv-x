package config

import (
	"strings"
	"testing"
)

func TestExpandPath(t *testing.T) {
	got, err := ExpandPath("~/example")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "/example") || strings.HasPrefix(got, "~") {
		t.Fatalf("expected expanded path, got %q", got)
	}
}

func TestRenderAndParseDefault(t *testing.T) {
	rendered := Render(Default())
	for _, want := range []string{
		`[camera]`,
		`input_device = "/dev/video0"`,
		`input_format = "nv12"`,
		`width = 1920`,
		`height = 1080`,
		`fps = 50`,
		`device = "/dev/video10"`,
		`label = "NV-vCam"`,
		`video_nr = 10`,
		`output_format = "yuv420p"`,
		`exclusive_caps = true`,
		`max_buffers = 8`,
		`mode = "blur"`,
		`background_image = ""`,
		`chroma_color = "#00ff00"`,
		`sdk_path = "/usr/local/VideoFX"`,
		`model_dir = "/usr/local/VideoFX/lib/models"`,
		`enable_os_release_shim = true`,
		`blur_strength = 0.75`,
		`theme = "system"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, rendered)
		}
	}

	parsed, err := Parse([]byte(rendered))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Camera.InputDevice != "/dev/video0" || parsed.Camera.InputFormat != "nv12" || parsed.Camera.Width != 1920 || parsed.Camera.Height != 1080 || parsed.Camera.FPS != 50 {
		t.Fatalf("unexpected parsed config: %+v", parsed)
	}
	if parsed.Output.Device != "/dev/video10" || parsed.Output.VideoNR != 10 || parsed.Output.Label != "NV-vCam" || parsed.Output.OutputFormat != "yuv420p" {
		t.Fatalf("unexpected parsed output config: %+v", parsed.Output)
	}
	if parsed.UI.Theme != "system" {
		t.Fatalf("expected system theme, got %q", parsed.UI.Theme)
	}
	if !parsed.FX.Enabled || parsed.FX.Mode != "blur" || parsed.FX.BackgroundImage != "" || parsed.FX.ChromaColor != "#00ff00" || parsed.FX.SDKPath != "/usr/local/VideoFX" || parsed.FX.ModelDir != "/usr/local/VideoFX/lib/models" || !parsed.FX.EnableOSReleaseShim || parsed.FX.BlurStrength != 0.75 {
		t.Fatalf("unexpected parsed fx config: %+v", parsed.FX)
	}
}

func TestParseRejectsOldCaptureSection(t *testing.T) {
	if _, err := Parse([]byte("[capture]\ninput_command = \"gphoto2 --stdout --capture-movie\"\n")); err == nil {
		t.Fatal("expected old capture config to be rejected")
	}
}

func TestParseRejectsInvalidTheme(t *testing.T) {
	rendered := strings.Replace(Render(Default()), `theme = "system"`, `theme = "midnight"`, 1)
	if _, err := Parse([]byte(rendered)); err == nil {
		t.Fatal("expected invalid theme error")
	}
}

func TestValidateTheme(t *testing.T) {
	for _, theme := range []string{"system", "light", "dark"} {
		if err := ValidateTheme(theme); err != nil {
			t.Fatalf("expected %q to be valid: %v", theme, err)
		}
	}
	if err := ValidateTheme("blue"); err == nil {
		t.Fatal("expected invalid theme error")
	}
}

func TestValidateBackgroundMode(t *testing.T) {
	for _, mode := range []string{"blur", "mask", "replace", "chroma"} {
		if err := ValidateBackgroundMode(mode); err != nil {
			t.Fatalf("expected %q to be valid: %v", mode, err)
		}
	}
	if err := ValidateBackgroundMode("matte"); err == nil {
		t.Fatal("expected invalid background mode error")
	}
}

func TestValidateChromaColor(t *testing.T) {
	for _, color := range []string{"#00ff00", "#0F0fAa"} {
		if err := ValidateChromaColor(color); err != nil {
			t.Fatalf("expected %q to be valid: %v", color, err)
		}
	}
	for _, color := range []string{"00ff00", "#00ff0", "#00ff0x"} {
		if err := ValidateChromaColor(color); err == nil {
			t.Fatalf("expected %q to be invalid", color)
		}
	}
}
