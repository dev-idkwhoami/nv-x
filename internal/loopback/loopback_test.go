package loopback

import (
	"strings"
	"testing"

	"nv-vcam/internal/config"
)

func TestRenderSingleLoopbackDevice(t *testing.T) {
	got := Render(config.Default())
	for _, want := range []string{
		`devices=1`,
		`video_nr=10`,
		`card_label="NV-vCam"`,
		`exclusive_caps=1`,
		`max_buffers=8`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, `,`) {
		t.Fatalf("only one loopback label should be rendered:\n%s", got)
	}
}

func TestRenderQuotesLabelsWithSpaces(t *testing.T) {
	cfg := config.Default()
	cfg.Output.Label = "NV vCam Output"
	got := Render(cfg)
	want := `card_label="NV vCam Output"`
	if !strings.Contains(got, want) {
		t.Fatalf("expected %q in:\n%s", want, got)
	}
	if strings.Contains(got, `NV\ vCam`) {
		t.Fatalf("labels must be quoted, not space-escaped:\n%s", got)
	}
}
