package config

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Input    InputConfig
	Output   OutputConfig
	Loopback LoopbackConfig
	Capture  CaptureConfig
	FX       FXConfig
	Service  ServiceConfig
	UI       UIConfig
}

type InputConfig struct {
	Device string
	Label  string
}

type OutputConfig struct {
	Device  string
	VideoNR int
	Label   string
}

type LoopbackConfig struct {
	ConfigPath    string
	ExclusiveCaps bool
	MaxBuffers    int
}

type CaptureConfig struct {
	Enabled            bool
	InputCommand       string
	Device             string
	FPS                int
	Width              int
	Height             int
	UseCUDAScale       bool
	IdleTimeoutSeconds int
	IdleLabel          string
}

type FXConfig struct {
	SDKPath             string
	ModelDir            string
	EnableOSReleaseShim bool
	BlurStrength        float64
}

type ServiceConfig struct {
	Name     string
	ExecPath string
}

type UIConfig struct {
	Theme string
}

func Default() Config {
	return Config{
		Input: InputConfig{
			Device: "/dev/video10",
			Label:  "Sony Camera RAW",
		},
		Output: OutputConfig{
			Device:  "/dev/video20",
			VideoNR: 20,
			Label:   "Sony Camera FX",
		},
		Loopback: LoopbackConfig{
			ConfigPath:    "/etc/modprobe.d/nv-vcam-v4l2loopback.conf",
			ExclusiveCaps: true,
			MaxBuffers:    4,
		},
		Capture: CaptureConfig{
			Enabled:            true,
			InputCommand:       "gphoto2 --stdout --capture-movie",
			Device:             "/dev/video10",
			FPS:                25,
			Width:              2560,
			Height:             1440,
			UseCUDAScale:       true,
			IdleTimeoutSeconds: 15,
			IdleLabel:          "nv-vcam idling ...",
		},
		FX: FXConfig{
			SDKPath:             "/usr/local/VideoFX",
			ModelDir:            "/usr/local/VideoFX/lib/models",
			EnableOSReleaseShim: true,
			BlurStrength:        0.75,
		},
		Service: ServiceConfig{
			Name:     "nv-vcam.service",
			ExecPath: "~/.local/bin/nv-vcam",
		},
		UI: UIConfig{
			Theme: "system",
		},
	}
}

func DefaultPath() (string, error) {
	return ExpandPath("~/.config/nv-vcam/config.toml")
}

func ExpandPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

func Load(path string) (Config, error) {
	expanded, err := ExpandPath(path)
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		return Config{}, err
	}
	return Parse(data)
}

func Render(c Config) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "[input]\n")
	fmt.Fprintf(&b, "device = %q\n", c.Input.Device)
	fmt.Fprintf(&b, "label = %q\n\n", c.Input.Label)
	fmt.Fprintf(&b, "[output]\n")
	fmt.Fprintf(&b, "device = %q\n", c.Output.Device)
	fmt.Fprintf(&b, "video_nr = %d\n", c.Output.VideoNR)
	fmt.Fprintf(&b, "label = %q\n\n", c.Output.Label)
	fmt.Fprintf(&b, "[loopback]\n")
	fmt.Fprintf(&b, "config_path = %q\n", c.Loopback.ConfigPath)
	fmt.Fprintf(&b, "exclusive_caps = %t\n", c.Loopback.ExclusiveCaps)
	fmt.Fprintf(&b, "max_buffers = %d\n\n", c.Loopback.MaxBuffers)
	fmt.Fprintf(&b, "[capture]\n")
	fmt.Fprintf(&b, "enabled = %t\n", c.Capture.Enabled)
	fmt.Fprintf(&b, "input_command = %q\n", c.Capture.InputCommand)
	fmt.Fprintf(&b, "device = %q\n", c.Capture.Device)
	fmt.Fprintf(&b, "fps = %d\n", c.Capture.FPS)
	fmt.Fprintf(&b, "width = %d\n", c.Capture.Width)
	fmt.Fprintf(&b, "height = %d\n", c.Capture.Height)
	fmt.Fprintf(&b, "use_cuda_scale = %t\n", c.Capture.UseCUDAScale)
	fmt.Fprintf(&b, "idle_timeout_seconds = %d\n", c.Capture.IdleTimeoutSeconds)
	fmt.Fprintf(&b, "idle_label = %q\n\n", c.Capture.IdleLabel)
	fmt.Fprintf(&b, "[fx]\n")
	fmt.Fprintf(&b, "sdk_path = %q\n", c.FX.SDKPath)
	fmt.Fprintf(&b, "model_dir = %q\n", c.FX.ModelDir)
	fmt.Fprintf(&b, "enable_os_release_shim = %t\n", c.FX.EnableOSReleaseShim)
	fmt.Fprintf(&b, "blur_strength = %.2f\n\n", c.FX.BlurStrength)
	fmt.Fprintf(&b, "[service]\n")
	fmt.Fprintf(&b, "name = %q\n", c.Service.Name)
	fmt.Fprintf(&b, "exec_path = %q\n\n", c.Service.ExecPath)
	fmt.Fprintf(&b, "[ui]\n")
	fmt.Fprintf(&b, "theme = %q\n", c.UI.Theme)
	return b.String()
}

func Parse(data []byte) (Config, error) {
	cfg := Default()
	section := ""
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Config{}, fmt.Errorf("line %d: expected key = value", lineNo)
		}
		key = strings.TrimSpace(key)
		raw := strings.TrimSpace(value)
		if err := assign(&cfg, section, key, raw); err != nil {
			return Config{}, fmt.Errorf("line %d: %w", lineNo, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func assign(cfg *Config, section, key, raw string) error {
	switch section + "." + key {
	case "input.device":
		v, err := parseString(raw)
		cfg.Input.Device = v
		return err
	case "input.label":
		v, err := parseString(raw)
		cfg.Input.Label = v
		return err
	case "output.device":
		v, err := parseString(raw)
		cfg.Output.Device = v
		return err
	case "output.video_nr":
		v, err := strconv.Atoi(raw)
		cfg.Output.VideoNR = v
		return err
	case "output.label":
		v, err := parseString(raw)
		cfg.Output.Label = v
		return err
	case "loopback.config_path":
		v, err := parseString(raw)
		cfg.Loopback.ConfigPath = v
		return err
	case "loopback.exclusive_caps":
		v, err := strconv.ParseBool(raw)
		cfg.Loopback.ExclusiveCaps = v
		return err
	case "loopback.max_buffers":
		v, err := strconv.Atoi(raw)
		cfg.Loopback.MaxBuffers = v
		return err
	case "capture.enabled":
		v, err := strconv.ParseBool(raw)
		cfg.Capture.Enabled = v
		return err
	case "capture.input_command":
		v, err := parseString(raw)
		cfg.Capture.InputCommand = v
		return err
	case "capture.device":
		v, err := parseString(raw)
		cfg.Capture.Device = v
		return err
	case "capture.fps":
		v, err := strconv.Atoi(raw)
		cfg.Capture.FPS = v
		return err
	case "capture.width":
		v, err := strconv.Atoi(raw)
		cfg.Capture.Width = v
		return err
	case "capture.height":
		v, err := strconv.Atoi(raw)
		cfg.Capture.Height = v
		return err
	case "capture.use_cuda_scale":
		v, err := strconv.ParseBool(raw)
		cfg.Capture.UseCUDAScale = v
		return err
	case "capture.idle_timeout_seconds":
		v, err := strconv.Atoi(raw)
		cfg.Capture.IdleTimeoutSeconds = v
		return err
	case "capture.idle_label":
		v, err := parseString(raw)
		cfg.Capture.IdleLabel = v
		return err
	case "fx.sdk_path":
		v, err := parseString(raw)
		cfg.FX.SDKPath = v
		return err
	case "fx.model_dir":
		v, err := parseString(raw)
		cfg.FX.ModelDir = v
		return err
	case "fx.enable_os_release_shim":
		v, err := strconv.ParseBool(raw)
		cfg.FX.EnableOSReleaseShim = v
		return err
	case "fx.blur_strength":
		v, err := strconv.ParseFloat(raw, 64)
		cfg.FX.BlurStrength = v
		return err
	case "fx.onnxruntime_library_path", "fx.model_path", "fx.provider", "fx.device_id":
		// Deprecated pre-Maxine keys are accepted so older config files keep loading.
		return nil
	case "service.name":
		v, err := parseString(raw)
		cfg.Service.Name = v
		return err
	case "service.exec_path":
		v, err := parseString(raw)
		cfg.Service.ExecPath = v
		return err
	case "ui.theme":
		v, err := parseString(raw)
		if err != nil {
			return err
		}
		if err := ValidateTheme(v); err != nil {
			return err
		}
		cfg.UI.Theme = v
		return nil
	default:
		return fmt.Errorf("unknown key %q in section %q", key, section)
	}
}

func ValidateTheme(theme string) error {
	switch theme {
	case "system", "light", "dark":
		return nil
	default:
		return fmt.Errorf("invalid ui theme %q: expected system, light, or dark", theme)
	}
}

func parseString(raw string) (string, error) {
	if strings.HasPrefix(raw, `"`) {
		return strconv.Unquote(raw)
	}
	return raw, nil
}
