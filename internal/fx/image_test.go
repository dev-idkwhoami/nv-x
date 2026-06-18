package fx

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestRunTestImageWritesOutputAndMask(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.png")
	output := filepath.Join(dir, "out.png")
	mask := filepath.Join(dir, "mask.png")
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 4), B: 120, A: 255})
		}
	}
	if err := SaveImage(input, img); err != nil {
		t.Fatal(err)
	}

	result, err := RunTestImage(TestImageOptions{
		InputPath:  input,
		OutputPath: output,
		MaskPath:   mask,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Width != 64 || result.Height != 48 || result.Runtime != "placeholder-cpu" {
		t.Fatalf("unexpected result: %+v", result)
	}
	for _, path := range []string{output, mask} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
		if info.Size() == 0 {
			t.Fatalf("expected %s to be non-empty", path)
		}
	}
}

func TestRunTestImageRequiresInputAndOutput(t *testing.T) {
	if _, err := RunTestImage(TestImageOptions{}); err == nil {
		t.Fatal("expected missing input error")
	}
	if _, err := RunTestImage(TestImageOptions{InputPath: "missing.png"}); err == nil {
		t.Fatal("expected missing output error")
	}
}

func TestPlaceholderPersonMaskHasForeground(t *testing.T) {
	mask := PlaceholderPersonMask(80, 60)
	if mask.GrayAt(40, 20).Y == 0 {
		t.Fatal("expected head region to be foreground")
	}
	if mask.GrayAt(0, 0).Y != 0 {
		t.Fatal("expected corner to be background")
	}
}
