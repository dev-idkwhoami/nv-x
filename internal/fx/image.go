package fx

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type TestImageOptions struct {
	InputPath  string
	OutputPath string
	MaskPath   string
}

type TestImageResult struct {
	InputPath  string
	OutputPath string
	MaskPath   string
	Width      int
	Height     int
	Runtime    string
}

func RunTestImage(opts TestImageOptions) (TestImageResult, error) {
	if opts.InputPath == "" {
		return TestImageResult{}, fmt.Errorf("--input is required")
	}
	if opts.OutputPath == "" {
		return TestImageResult{}, fmt.Errorf("--output is required")
	}
	src, err := LoadImage(opts.InputPath)
	if err != nil {
		return TestImageResult{}, err
	}
	bounds := src.Bounds()
	mask := PlaceholderPersonMask(bounds.Dx(), bounds.Dy())
	out := CompositeBackground(src, mask, color.RGBA{R: 28, G: 34, B: 44, A: 255})

	if opts.MaskPath != "" {
		if err := SaveImage(opts.MaskPath, mask); err != nil {
			return TestImageResult{}, err
		}
	}
	if err := SaveImage(opts.OutputPath, out); err != nil {
		return TestImageResult{}, err
	}
	return TestImageResult{
		InputPath:  opts.InputPath,
		OutputPath: opts.OutputPath,
		MaskPath:   opts.MaskPath,
		Width:      bounds.Dx(),
		Height:     bounds.Dy(),
		Runtime:    "placeholder-cpu",
	}, nil
}

func LoadImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return img, nil
}

func SaveImage(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return jpeg.Encode(file, img, &jpeg.Options{Quality: 92})
	default:
		return png.Encode(file, img)
	}
}

func PlaceholderPersonMask(width, height int) *image.Gray {
	mask := image.NewGray(image.Rect(0, 0, width, height))
	if width <= 0 || height <= 0 {
		return mask
	}
	cx := float64(width) / 2
	bodyCY := float64(height) * 0.64
	bodyRX := float64(width) * 0.24
	bodyRY := float64(height) * 0.34
	headCY := float64(height) * 0.28
	headR := math.Min(float64(width), float64(height)) * 0.13

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			xf := float64(x)
			yf := float64(y)
			body := math.Pow((xf-cx)/bodyRX, 2) + math.Pow((yf-bodyCY)/bodyRY, 2)
			head := math.Hypot(xf-cx, yf-headCY) / headR
			alpha := 0.0
			if body <= 1 {
				alpha = math.Max(alpha, 1-body)
			}
			if head <= 1 {
				alpha = math.Max(alpha, 1-head)
			}
			soft := uint8(math.Min(255, math.Max(0, alpha*320)))
			mask.SetGray(x, y, color.Gray{Y: soft})
		}
	}
	return mask
}

func CompositeBackground(src image.Image, mask *image.Gray, bg color.Color) *image.RGBA {
	bounds := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(out, out.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			alpha := float64(mask.GrayAt(x, y).Y) / 255
			sr, sg, sb, _ := src.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			br, bg, bb, _ := out.At(x, y).RGBA()
			out.SetRGBA(x, y, color.RGBA{
				R: uint8((float64(sr>>8) * alpha) + (float64(br>>8) * (1 - alpha))),
				G: uint8((float64(sg>>8) * alpha) + (float64(bg>>8) * (1 - alpha))),
				B: uint8((float64(sb>>8) * alpha) + (float64(bb>>8) * (1 - alpha))),
				A: 255,
			})
		}
	}
	return out
}
