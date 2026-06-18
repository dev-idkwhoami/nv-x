package loopback

import (
	"strings"
	"testing"

	"nv-vcam/internal/config"
)

func TestRenderQuotesLabelsWithSpaces(t *testing.T) {
	got := Render(config.Default())
	want := `card_label="Sony Camera RAW,Sony Camera FX"`
	if !strings.Contains(got, want) {
		t.Fatalf("expected %q in:\n%s", want, got)
	}
	if strings.Contains(got, `Sony\ Camera`) {
		t.Fatalf("labels must be quoted, not space-escaped:\n%s", got)
	}
}
