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
		`device = "/dev/video10"`,
		`label = "Sony Camera RAW"`,
		`video_nr = 20`,
		`exclusive_caps = true`,
		`input_command = "gphoto2 --stdout --capture-movie"`,
		`idle_label = "nv-vcam idling ..."`,
		`idle_enabled = true`,
		`input_device = "/dev/video10"`,
		`output_device = "/dev/video20"`,
		`width = 2560`,
		`height = 1440`,
		`fps = 25`,
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
	if parsed.Input.Device != "/dev/video10" || parsed.Output.VideoNR != 20 {
		t.Fatalf("unexpected parsed config: %+v", parsed)
	}
	if parsed.UI.Theme != "system" {
		t.Fatalf("expected system theme, got %q", parsed.UI.Theme)
	}
	if !parsed.Capture.Enabled || parsed.Capture.Device != "/dev/video10" || parsed.Capture.Width != 2560 {
		t.Fatalf("unexpected parsed capture config: %+v", parsed.Capture)
	}
	if !parsed.FX.Enabled || !parsed.FX.IdleEnabled || parsed.FX.InputDevice != "/dev/video10" || parsed.FX.OutputDevice != "/dev/video20" || parsed.FX.Width != 2560 || parsed.FX.Height != 1440 || parsed.FX.FPS != 25 {
		t.Fatalf("unexpected parsed fx stream config: %+v", parsed.FX)
	}
	if parsed.FX.SDKPath != "/usr/local/VideoFX" || parsed.FX.ModelDir != "/usr/local/VideoFX/lib/models" || !parsed.FX.EnableOSReleaseShim || parsed.FX.BlurStrength != 0.75 {
		t.Fatalf("unexpected parsed fx config: %+v", parsed.FX)
	}
}

func TestParseAcceptsDeprecatedONNXKeys(t *testing.T) {
	rendered := Render(Default()) + `
[fx]
onnxruntime_library_path = "/tmp/libonnxruntime.so"
model_path = "/tmp/model.onnx"
provider = "cuda"
device_id = 0
`
	if _, err := Parse([]byte(rendered)); err != nil {
		t.Fatalf("expected deprecated fx keys to be ignored, got %v", err)
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
