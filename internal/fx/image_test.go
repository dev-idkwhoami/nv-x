package fx

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"nv-vcam/internal/config"
)

func TestRunTestImageRequiresInputAndOutputs(t *testing.T) {
	cfg := config.Default()
	if _, err := RunTestImage(cfg, TestImageOptions{}); err == nil {
		t.Fatal("expected missing input error")
	}
	if _, err := RunTestImage(cfg, TestImageOptions{InputPath: "missing.png"}); err == nil {
		t.Fatal("expected missing blur output error")
	}
	if _, err := RunTestImage(cfg, TestImageOptions{InputPath: "missing.png", BlurPath: "blur.png"}); err == nil {
		t.Fatal("expected missing removed output error")
	}
}

func TestPPMRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.ppm")
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.SetRGBA(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	src.SetRGBA(1, 0, color.RGBA{R: 40, G: 50, B: 60, A: 255})
	src.SetRGBA(0, 1, color.RGBA{R: 70, G: 80, B: 90, A: 255})
	src.SetRGBA(1, 1, color.RGBA{R: 100, G: 110, B: 120, A: 255})
	if err := WritePPM(path, src); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPPM(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() != 2 || got.Bounds().Dy() != 2 {
		t.Fatalf("unexpected bounds: %v", got.Bounds())
	}
	if c := got.RGBAAt(1, 1); c.R != 100 || c.G != 110 || c.B != 120 || c.A != 255 {
		t.Fatalf("unexpected pixel: %+v", c)
	}
}

func TestReadPGMAndCompositeTransparent(t *testing.T) {
	dir := t.TempDir()
	maskPath := filepath.Join(dir, "mask.pgm")
	if err := os.WriteFile(maskPath, []byte("P5\n2 1\n255\n\x00\xff"), 0o644); err != nil {
		t.Fatal(err)
	}
	mask, err := ReadPGM(maskPath)
	if err != nil {
		t.Fatal(err)
	}
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	src.SetRGBA(1, 0, color.RGBA{R: 40, G: 50, B: 60, A: 255})
	out := CompositeTransparent(src, mask)
	if out.RGBAAt(0, 0).A != 0 {
		t.Fatalf("expected first pixel transparent, got %+v", out.RGBAAt(0, 0))
	}
	if out.RGBAAt(1, 0).A != 255 {
		t.Fatalf("expected second pixel opaque, got %+v", out.RGBAAt(1, 0))
	}
}
