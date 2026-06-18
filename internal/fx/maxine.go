package fx

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"nv-vcam/internal/config"
)

const fakeOSRelease = `NAME="Ubuntu"
VERSION="24.04 LTS (Noble Numbat)"
ID=ubuntu
ID_LIKE=debian
PRETTY_NAME="Ubuntu 24.04 LTS"
VERSION_ID="24.04"
`

type DoctorResult struct {
	SDKPath          string
	ModelDir         string
	HelperPath       string
	ShimPath         string
	OSReleaseShim    bool
	SDKVersion       string
	SDKExists        bool
	FeaturesOK       bool
	ModelsOK         bool
	LinkerOK         bool
	HelperOK         bool
	MissingFiles     []string
	MissingLibraries []string
	Message          string
	HelperOutput     string
}

type TestImageOptions struct {
	InputPath       string
	BlurPath        string
	RemovedPath     string
	MaskPath        string
	FinalPath       string
	DenoisePath     string
	BackgroundMode  string
	BackgroundImage string
	ChromaColor     string
	BlurStrength    float64
	DenoiseEnabled  bool
	DenoiseStrength int
}

type TestImageResult struct {
	InputPath       string
	BlurPath        string
	RemovedPath     string
	MaskPath        string
	FinalPath       string
	DenoisePath     string
	Width           int
	Height          int
	Runtime         string
	BackgroundMode  string
	BackgroundImage string
	ChromaColor     string
	BlurStrength    float64
	DenoiseEnabled  bool
	DenoiseStrength int
}

func Doctor(cfg config.Config) DoctorResult {
	env, result := maxineEnv(cfg)
	result.SDKExists = len(result.MissingFiles) == 0
	result.FeaturesOK = requiredFeatureFilesPresent(result.MissingFiles)
	result.ModelsOK = requiredModelFilesPresent(result.MissingFiles)
	if result.HelperPath == "" {
		result.Message = "Maxine helper binary not found; run make build"
		return result
	}
	if result.OSReleaseShim && result.ShimPath == "" {
		result.Message = "CachyOS os-release shim not found; run make build"
		return result
	}
	if len(result.MissingFiles) > 0 {
		result.Message = "Maxine SDK installation is incomplete"
		return result
	}

	result.MissingLibraries = MissingSharedLibraries(filepath.Join(result.SDKPath, "lib", "libVideoFX.so"), env)
	result.LinkerOK = len(result.MissingLibraries) == 0
	if !result.LinkerOK {
		result.Message = "Maxine shared library dependencies are missing"
		return result
	}

	doctorBackgroundMode := cfg.FX.BackgroundMode
	if doctorBackgroundMode == "replace" {
		doctorBackgroundMode = "blur"
	}
	cmd := exec.Command(result.HelperPath, "doctor",
		"--sdk-path", result.SDKPath,
		"--model-dir", result.ModelDir,
		"--background", doctorBackgroundMode,
		"--blur-strength", fmt.Sprintf("%.3f", cfg.FX.BlurStrength),
		"--denoise", boolArg(cfg.FX.DenoiseEnabled),
		"--denoise-strength", strconv.Itoa(cfg.FX.DenoiseStrength),
	)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	result.HelperOutput = strings.TrimSpace(string(out))
	if err != nil {
		result.Message = fmt.Sprintf("Maxine helper smoke test failed: %v", err)
		return result
	}
	result.HelperOK = true
	result.SDKVersion = helperValue(result.HelperOutput, "sdk_version")
	result.Message = "Maxine SDK initialized and GreenScreen/BackgroundBlur smoke test passed"
	return result
}

func RunTestImage(cfg config.Config, opts TestImageOptions) (TestImageResult, error) {
	if opts.InputPath == "" {
		return TestImageResult{}, fmt.Errorf("--input is required")
	}
	if opts.BlurPath == "" {
		return TestImageResult{}, fmt.Errorf("--blur-output is required")
	}
	if opts.RemovedPath == "" {
		return TestImageResult{}, fmt.Errorf("--removed-output is required")
	}
	if opts.BlurStrength <= 0 {
		opts.BlurStrength = cfg.FX.BlurStrength
	}
	if opts.BackgroundMode == "" {
		opts.BackgroundMode = cfg.FX.BackgroundMode
	}
	if err := config.ValidateBackgroundMode(opts.BackgroundMode); err != nil {
		return TestImageResult{}, err
	}
	if opts.BackgroundImage == "" {
		opts.BackgroundImage = cfg.FX.BackgroundImage
	}
	backgroundImage, err := config.ExpandPath(opts.BackgroundImage)
	if err != nil {
		return TestImageResult{}, err
	}
	opts.BackgroundImage = backgroundImage
	if opts.ChromaColor == "" {
		opts.ChromaColor = cfg.FX.ChromaColor
	}
	if err := config.ValidateChromaColor(opts.ChromaColor); err != nil {
		return TestImageResult{}, err
	}
	if !opts.DenoiseEnabled {
		opts.DenoiseEnabled = cfg.FX.DenoiseEnabled
	}
	if opts.DenoiseStrength != 0 && opts.DenoiseStrength != 1 {
		opts.DenoiseStrength = cfg.FX.DenoiseStrength
	}

	env, doctor := maxineEnv(cfg)
	if doctor.HelperPath == "" {
		return TestImageResult{}, fmt.Errorf("Maxine helper binary not found; run make build")
	}
	if doctor.OSReleaseShim && doctor.ShimPath == "" {
		return TestImageResult{}, fmt.Errorf("CachyOS os-release shim not found; run make build")
	}
	if len(doctor.MissingFiles) > 0 {
		return TestImageResult{}, fmt.Errorf("Maxine SDK installation is incomplete: %s", strings.Join(doctor.MissingFiles, ", "))
	}

	src, err := LoadImage(opts.InputPath)
	if err != nil {
		return TestImageResult{}, err
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 512 || height < 288 {
		return TestImageResult{}, fmt.Errorf("Maxine test image must be at least 512x288, got %dx%d", width, height)
	}
	if opts.DenoiseEnabled && height > 1080 {
		return TestImageResult{}, fmt.Errorf("Maxine denoise supports up to 1080p input height, got %d; disable denoise or use a smaller input image", height)
	}

	dir, err := os.MkdirTemp("", "nv-vcam-fx-*")
	if err != nil {
		return TestImageResult{}, err
	}
	defer os.RemoveAll(dir)

	inputPPM := filepath.Join(dir, "input.ppm")
	maskPGM := filepath.Join(dir, "mask.pgm")
	blurPPM := filepath.Join(dir, "blur.ppm")
	finalPPM := filepath.Join(dir, "final.ppm")
	denoisePPM := filepath.Join(dir, "denoise.ppm")
	if err := WritePPM(inputPPM, src); err != nil {
		return TestImageResult{}, err
	}

	cmd := exec.Command(doctor.HelperPath, "test-image",
		"--sdk-path", doctor.SDKPath,
		"--model-dir", doctor.ModelDir,
		"--input", inputPPM,
		"--mask", maskPGM,
		"--blur", blurPPM,
		"--final", finalPPM,
		"--denoise-output", denoisePPM,
		"--background", opts.BackgroundMode,
		"--blur-strength", fmt.Sprintf("%.3f", opts.BlurStrength),
		"--chroma-color", opts.ChromaColor,
		"--denoise", boolArg(opts.DenoiseEnabled),
		"--denoise-strength", strconv.Itoa(opts.DenoiseStrength),
	)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return TestImageResult{}, fmt.Errorf("Maxine helper test-image failed: %v\n%s", err, strings.TrimSpace(string(out)))
	}

	mask, err := ReadPGM(maskPGM)
	if err != nil {
		return TestImageResult{}, err
	}
	blur, err := ReadPPM(blurPPM)
	if err != nil {
		return TestImageResult{}, err
	}
	final, err := ReadPPM(finalPPM)
	if err != nil {
		return TestImageResult{}, err
	}
	denoised, err := ReadPPM(denoisePPM)
	if err != nil {
		return TestImageResult{}, err
	}

	if opts.MaskPath != "" {
		if err := SaveImage(opts.MaskPath, mask); err != nil {
			return TestImageResult{}, err
		}
	}
	if err := SaveImage(opts.BlurPath, blur); err != nil {
		return TestImageResult{}, err
	}
	if err := SaveImage(opts.RemovedPath, CompositeTransparent(src, mask)); err != nil {
		return TestImageResult{}, err
	}
	if opts.FinalPath != "" {
		if opts.BackgroundMode == "replace" {
			replaced, err := CompositeReplacement(src, mask, opts.BackgroundImage)
			if err != nil {
				return TestImageResult{}, err
			}
			if err := SaveImage(opts.FinalPath, replaced); err != nil {
				return TestImageResult{}, err
			}
		} else {
			if err := SaveImage(opts.FinalPath, final); err != nil {
				return TestImageResult{}, err
			}
		}
	}
	if opts.DenoisePath != "" {
		if err := SaveImage(opts.DenoisePath, denoised); err != nil {
			return TestImageResult{}, err
		}
	}

	return TestImageResult{
		InputPath:       opts.InputPath,
		BlurPath:        opts.BlurPath,
		RemovedPath:     opts.RemovedPath,
		MaskPath:        opts.MaskPath,
		FinalPath:       opts.FinalPath,
		DenoisePath:     opts.DenoisePath,
		Width:           width,
		Height:          height,
		Runtime:         "maxine",
		BackgroundMode:  opts.BackgroundMode,
		BackgroundImage: opts.BackgroundImage,
		ChromaColor:     opts.ChromaColor,
		BlurStrength:    opts.BlurStrength,
		DenoiseEnabled:  opts.DenoiseEnabled,
		DenoiseStrength: opts.DenoiseStrength,
	}, nil
}

func maxineEnv(cfg config.Config) ([]string, DoctorResult) {
	sdkPath, _ := config.ExpandPath(cfg.FX.SDKPath)
	modelDir, _ := config.ExpandPath(cfg.FX.ModelDir)
	helperPath := findHelper()
	shimPath := findShim()
	needsShim := cfg.FX.EnableOSReleaseShim && isArchLike()

	libPaths := []string{
		filepath.Join(sdkPath, "lib"),
		filepath.Join(sdkPath, "external", "cuda", "lib"),
		filepath.Join(sdkPath, "external", "tensorrt", "lib"),
		filepath.Join(sdkPath, "features", "nvvfxgreenscreen", "lib"),
		filepath.Join(sdkPath, "features", "nvvfxbackgroundblur", "lib"),
		filepath.Join(sdkPath, "features", "nvvfxdenoising", "lib"),
	}
	env := os.Environ()
	env = upsertEnv(env, "LD_LIBRARY_PATH", strings.Join(append(libPaths, envValue("LD_LIBRARY_PATH")...), string(os.PathListSeparator)))
	if needsShim && shimPath != "" {
		fakePath := filepath.Join(os.TempDir(), "nv-vcam-fake-os-release")
		_ = os.WriteFile(fakePath, []byte(fakeOSRelease), 0o644)
		env = upsertEnv(env, "LD_PRELOAD", shimPath)
		env = upsertEnv(env, "NV_VCAM_FAKE_OS_RELEASE", fakePath)
	}

	missing := missingMaxineFiles(sdkPath, modelDir)
	return env, DoctorResult{
		SDKPath:       sdkPath,
		ModelDir:      modelDir,
		HelperPath:    helperPath,
		ShimPath:      shimPath,
		OSReleaseShim: needsShim,
		MissingFiles:  missing,
	}
}

func missingMaxineFiles(sdkPath, modelDir string) []string {
	required := []string{
		filepath.Join(sdkPath, "include", "nvVideoEffects.h"),
		filepath.Join(sdkPath, "lib", "libVideoFX.so"),
		filepath.Join(sdkPath, "lib", "libNVCVImage.so"),
		filepath.Join(sdkPath, "features", "nvvfxgreenscreen", "lib", "libnvVFXGreenScreen.so"),
		filepath.Join(sdkPath, "features", "nvvfxbackgroundblur", "lib", "libnvVFXBackgroundBlur.so"),
	}
	var missing []string
	for _, path := range required {
		if _, err := os.Stat(path); err != nil {
			missing = append(missing, path)
		}
	}
	for _, pattern := range []string{
		filepath.Join(modelDir, "AIGS_*_89_*.engine.trtpkg"),
	} {
		matches, _ := filepath.Glob(pattern)
		if len(matches) == 0 {
			missing = append(missing, pattern)
		}
	}
	return missing
}

func requiredFeatureFilesPresent(missing []string) bool {
	for _, path := range missing {
		if strings.Contains(path, "/features/") {
			return false
		}
	}
	return true
}

func requiredModelFilesPresent(missing []string) bool {
	for _, path := range missing {
		if strings.Contains(path, ".engine.trtpkg") {
			return false
		}
	}
	return true
}

func MissingSharedLibraries(path string, env []string) []string {
	if path == "" {
		return nil
	}
	cmd := exec.Command("ldd", path)
	cmd.Env = env
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return nil
	}
	var missing []string
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "=> not found") {
			continue
		}
		name, _, _ := strings.Cut(line, "=>")
		name = strings.TrimSpace(name)
		if name != "" {
			missing = append(missing, name)
		}
	}
	return missing
}

func findHelper() string {
	if path := os.Getenv("NV_VCAM_MAXINE_HELPER"); path != "" {
		return pathIfExists(path)
	}
	if exe, err := os.Executable(); err == nil {
		if path := pathIfExists(filepath.Join(filepath.Dir(exe), "nv-vcam-maxine-helper")); path != "" {
			return path
		}
	}
	if path, err := exec.LookPath("nv-vcam-maxine-helper"); err == nil {
		return path
	}
	if path := pathIfExists("bin/nv-vcam-maxine-helper"); path != "" {
		return path
	}
	return ""
}

func findShim() string {
	if path := os.Getenv("NV_VCAM_MAXINE_OS_RELEASE_SHIM"); path != "" {
		return pathIfExists(path)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, path := range []string{
			filepath.Join(dir, "nv-vcam-os-release-shim.so"),
			filepath.Join(filepath.Dir(dir), "lib", "nv-vcam", "nv-vcam-os-release-shim.so"),
		} {
			if found := pathIfExists(path); found != "" {
				return found
			}
		}
	}
	for _, path := range []string{"bin/nv-vcam-os-release-shim.so", filepath.Join(os.Getenv("HOME"), ".local", "lib", "nv-vcam", "nv-vcam-os-release-shim.so")} {
		if found := pathIfExists(path); found != "" {
			return found
		}
	}
	return ""
}

func pathIfExists(path string) string {
	if path == "" {
		return ""
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

func isArchLike() bool {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS == "linux"
	}
	text := strings.ToLower(string(data))
	return strings.Contains(text, "id_like=arch") || strings.Contains(text, "id=cachyos") || strings.Contains(text, "id=arch")
}

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func envValue(key string) []string {
	if value := os.Getenv(key); value != "" {
		return []string{value}
	}
	return nil
}

func helperValue(output, key string) string {
	for _, line := range strings.Split(output, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && k == key {
			return v
		}
	}
	return ""
}

func LoadImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		fallback, fallbackErr := LoadImageWithFFmpeg(path)
		if fallbackErr == nil {
			return fallback, nil
		}
		return nil, fmt.Errorf("decode %s: %w; ffmpeg fallback failed: %v", path, err, fallbackErr)
	}
	return img, nil
}

func LoadImageWithFFmpeg(path string) (image.Image, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, err
	}
	cmd := exec.Command("ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-i", path,
		"-frames:v", "1",
		"-f", "image2pipe",
		"-vcodec", "ppm",
		"-",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return DecodePPM(out)
}

func SaveImage(path string, img image.Image) error {
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
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

func WritePPM(path string, img image.Image) error {
	bounds := img.Bounds()
	var b bytes.Buffer
	fmt.Fprintf(&b, "P6\n%d %d\n255\n", bounds.Dx(), bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			b.WriteByte(byte(r >> 8))
			b.WriteByte(byte(g >> 8))
			b.WriteByte(byte(bl >> 8))
		}
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func ReadPPM(path string) (*image.RGBA, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, err := DecodePPM(data)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return img, nil
}

func DecodePPM(data []byte) (*image.RGBA, error) {
	reader := bytes.NewReader(data)
	width, height, err := readPNMHeader(reader, "P6")
	if err != nil {
		return nil, err
	}
	pixels := make([]byte, width*height*3)
	if _, err := io.ReadFull(reader, pixels); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		img.Pix[i*4+0] = pixels[i*3+0]
		img.Pix[i*4+1] = pixels[i*3+1]
		img.Pix[i*4+2] = pixels[i*3+2]
		img.Pix[i*4+3] = 255
	}
	return img, nil
}

func ReadPGM(path string) (*image.Gray, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	reader := bytes.NewReader(data)
	width, height, err := readPNMHeader(reader, "P5")
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	pixels := make([]byte, width*height)
	if _, err := io.ReadFull(reader, pixels); err != nil {
		return nil, err
	}
	return &image.Gray{Pix: pixels, Stride: width, Rect: image.Rect(0, 0, width, height)}, nil
}

func readPNMHeader(r *bytes.Reader, magic string) (int, int, error) {
	got, err := readToken(r)
	if err != nil {
		return 0, 0, err
	}
	if got != magic {
		return 0, 0, fmt.Errorf("expected %s, got %s", magic, got)
	}
	ws, err := readToken(r)
	if err != nil {
		return 0, 0, err
	}
	hs, err := readToken(r)
	if err != nil {
		return 0, 0, err
	}
	ms, err := readToken(r)
	if err != nil {
		return 0, 0, err
	}
	width, err := strconv.Atoi(ws)
	if err != nil {
		return 0, 0, err
	}
	height, err := strconv.Atoi(hs)
	if err != nil {
		return 0, 0, err
	}
	maxValue, err := strconv.Atoi(ms)
	if err != nil {
		return 0, 0, err
	}
	if width <= 0 || height <= 0 || maxValue != 255 {
		return 0, 0, fmt.Errorf("invalid header %dx%d max %d", width, height, maxValue)
	}
	return width, height, nil
}

func readToken(r *bytes.Reader) (string, error) {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == '#' {
			for {
				c, err := r.ReadByte()
				if err != nil {
					return "", err
				}
				if c == '\n' {
					break
				}
			}
			continue
		}
		if b != ' ' && b != '\n' && b != '\r' && b != '\t' {
			_ = r.UnreadByte()
			break
		}
	}
	var token strings.Builder
	for {
		b, err := r.ReadByte()
		if err != nil {
			if token.Len() > 0 {
				return token.String(), nil
			}
			return "", err
		}
		if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
			return token.String(), nil
		}
		token.WriteByte(b)
	}
}

func CompositeTransparent(src image.Image, mask *image.Gray) *image.RGBA {
	bounds := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(out, out.Bounds(), image.Transparent, image.Point{}, draw.Src)
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			r, g, b, _ := src.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			alpha := mask.GrayAt(x, y).Y
			out.SetRGBA(x, y, color.RGBA{
				R: uint8(r >> 8),
				G: uint8(g >> 8),
				B: uint8(b >> 8),
				A: alpha,
			})
		}
	}
	return out
}

func CompositeReplacement(src image.Image, mask *image.Gray, replacementPath string) (image.Image, error) {
	if replacementPath == "" {
		return CompositeTransparent(src, mask), nil
	}
	replacement, err := LoadImage(replacementPath)
	if err != nil {
		return nil, fmt.Errorf("load replacement image: %w", err)
	}
	bounds := src.Bounds()
	bg := ResizeCover(replacement, bounds.Dx(), bounds.Dy())
	out := image.NewRGBA(bg.Bounds())
	draw.Draw(out, out.Bounds(), bg, image.Point{}, draw.Src)
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			alpha := uint32(mask.GrayAt(x, y).Y)
			invAlpha := uint32(255 - mask.GrayAt(x, y).Y)
			sr, sg, sb, _ := src.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			br, bgc, bb, _ := out.At(x, y).RGBA()
			out.SetRGBA(x, y, color.RGBA{
				R: uint8(((sr>>8)*alpha + (br>>8)*invAlpha) / 255),
				G: uint8(((sg>>8)*alpha + (bgc>>8)*invAlpha) / 255),
				B: uint8(((sb>>8)*alpha + (bb>>8)*invAlpha) / 255),
				A: 255,
			})
		}
	}
	return out, nil
}

func ResizeCover(src image.Image, width, height int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	if width <= 0 || height <= 0 {
		return out
	}
	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return out
	}
	scaleW := float64(width) / float64(srcW)
	scaleH := float64(height) / float64(srcH)
	scale := scaleW
	if scaleH > scale {
		scale = scaleH
	}
	scaledW := int(float64(srcW)*scale + 0.5)
	scaledH := int(float64(srcH)*scale + 0.5)
	if scaledW < width {
		scaledW = width
	}
	if scaledH < height {
		scaledH = height
	}
	offsetX := (scaledW - width) / 2
	offsetY := (scaledH - height) / 2
	for y := 0; y < height; y++ {
		srcY := bounds.Min.Y + ((y+offsetY)*srcH)/scaledH
		for x := 0; x < width; x++ {
			srcX := bounds.Min.X + ((x+offsetX)*srcW)/scaledW
			out.Set(x, y, src.At(srcX, srcY))
		}
	}
	return out
}
