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
	Camera   CameraConfig
	Output   OutputConfig
	Loopback LoopbackConfig
	FX       FXConfig
	Audio    AudioConfig
	Light    LightConfig
	Service  ServiceConfig
	UI       UIConfig
}

type AudioConfig struct {
	Mode                      string
	InputNode                 string
	MonitorEnabled            bool
	MonitorOutputNode         string
	DereverbDenoiserIntensity float64
	SDKPath                   string
	OutputNodeName            string
	OutputDescription         string
}

type CameraConfig struct {
	InputDevice string
	InputFormat string
	Width       int
	Height      int
	FPS         int
}

type OutputConfig struct {
	Device       string
	VideoNR      int
	Label        string
	OutputFormat string
}

type LoopbackConfig struct {
	ConfigPath    string
	ExclusiveCaps bool
	MaxBuffers    int
}

type FXConfig struct {
	Enabled             bool
	Mode                string
	BackgroundImage     string
	ChromaColor         string
	SDKPath             string
	ModelDir            string
	EnableOSReleaseShim bool
	BlurStrength        float64
}

type LightConfig struct {
	Enabled     bool
	Address     string
	Brightness  int
	Temperature int
	TimeoutMS   int
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
		Camera: CameraConfig{
			InputDevice: "/dev/video0",
			InputFormat: "nv12",
			Width:       1920,
			Height:      1080,
			FPS:         50,
		},
		Output: OutputConfig{
			Device:       "/dev/video10",
			VideoNR:      10,
			Label:        "NV-X Camera",
			OutputFormat: "yuv420p",
		},
		Loopback: LoopbackConfig{
			ConfigPath:    "/etc/modprobe.d/nv-x-v4l2loopback.conf",
			ExclusiveCaps: true,
			MaxBuffers:    8,
		},
		FX: FXConfig{
			Enabled:             true,
			Mode:                "blur",
			BackgroundImage:     "",
			ChromaColor:         "#00ff00",
			SDKPath:             "/usr/local/VideoFX",
			ModelDir:            "/usr/local/VideoFX/lib/models",
			EnableOSReleaseShim: true,
			BlurStrength:        0.75,
		},
		Audio: AudioConfig{
			Mode:                      "off",
			InputNode:                 "",
			MonitorEnabled:            false,
			MonitorOutputNode:         "",
			DereverbDenoiserIntensity: 0.90,
			SDKPath:                   "/usr/local/AudioFX",
			OutputNodeName:            "nv-x-microphone",
			OutputDescription:         "NV-X Microphone",
		},
		Light: LightConfig{
			Enabled:     false,
			Address:     "",
			Brightness:  20,
			Temperature: 206,
			TimeoutMS:   1500,
		},
		Service: ServiceConfig{
			Name:     "nv-x.service",
			ExecPath: "/usr/bin/nv-x",
		},
		UI: UIConfig{
			Theme: "system",
		},
	}
}

func DefaultPath() (string, error) {
	return ExpandPath("~/.config/nv-x/config.toml")
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
	fmt.Fprintf(&b, "[camera]\n")
	fmt.Fprintf(&b, "input_device = %q\n", c.Camera.InputDevice)
	fmt.Fprintf(&b, "input_format = %q\n", c.Camera.InputFormat)
	fmt.Fprintf(&b, "width = %d\n", c.Camera.Width)
	fmt.Fprintf(&b, "height = %d\n", c.Camera.Height)
	fmt.Fprintf(&b, "fps = %d\n\n", c.Camera.FPS)
	fmt.Fprintf(&b, "[output]\n")
	fmt.Fprintf(&b, "device = %q\n", c.Output.Device)
	fmt.Fprintf(&b, "video_nr = %d\n", c.Output.VideoNR)
	fmt.Fprintf(&b, "label = %q\n", c.Output.Label)
	fmt.Fprintf(&b, "output_format = %q\n\n", c.Output.OutputFormat)
	fmt.Fprintf(&b, "[loopback]\n")
	fmt.Fprintf(&b, "config_path = %q\n", c.Loopback.ConfigPath)
	fmt.Fprintf(&b, "exclusive_caps = %t\n", c.Loopback.ExclusiveCaps)
	fmt.Fprintf(&b, "max_buffers = %d\n\n", c.Loopback.MaxBuffers)
	fmt.Fprintf(&b, "[fx]\n")
	fmt.Fprintf(&b, "enabled = %t\n", c.FX.Enabled)
	fmt.Fprintf(&b, "mode = %q\n", c.FX.Mode)
	fmt.Fprintf(&b, "background_image = %q\n", c.FX.BackgroundImage)
	fmt.Fprintf(&b, "chroma_color = %q\n", c.FX.ChromaColor)
	fmt.Fprintf(&b, "sdk_path = %q\n", c.FX.SDKPath)
	fmt.Fprintf(&b, "model_dir = %q\n", c.FX.ModelDir)
	fmt.Fprintf(&b, "enable_os_release_shim = %t\n", c.FX.EnableOSReleaseShim)
	fmt.Fprintf(&b, "blur_strength = %.2f\n\n", c.FX.BlurStrength)
	fmt.Fprintf(&b, "[audio]\n")
	fmt.Fprintf(&b, "mode = %q\n", c.Audio.Mode)
	fmt.Fprintf(&b, "input_node = %q\n", c.Audio.InputNode)
	fmt.Fprintf(&b, "monitor_enabled = %t\n", c.Audio.MonitorEnabled)
	fmt.Fprintf(&b, "monitor_output_node = %q\n", c.Audio.MonitorOutputNode)
	fmt.Fprintf(&b, "dereverb_denoiser_intensity = %.2f\n", c.Audio.DereverbDenoiserIntensity)
	fmt.Fprintf(&b, "sdk_path = %q\n", c.Audio.SDKPath)
	fmt.Fprintf(&b, "output_node_name = %q\n", c.Audio.OutputNodeName)
	fmt.Fprintf(&b, "output_description = %q\n\n", c.Audio.OutputDescription)
	fmt.Fprintf(&b, "[light]\n")
	fmt.Fprintf(&b, "enabled = %t\n", c.Light.Enabled)
	fmt.Fprintf(&b, "address = %q\n", c.Light.Address)
	fmt.Fprintf(&b, "brightness = %d\n", c.Light.Brightness)
	fmt.Fprintf(&b, "temperature = %d\n", c.Light.Temperature)
	fmt.Fprintf(&b, "timeout_ms = %d\n\n", c.Light.TimeoutMS)
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
	case "camera.input_device":
		v, err := parseString(raw)
		cfg.Camera.InputDevice = v
		return err
	case "camera.input_format":
		v, err := parseString(raw)
		if err != nil {
			return err
		}
		if err := ValidateCameraFormat(v); err != nil {
			return err
		}
		cfg.Camera.InputFormat = v
		return nil
	case "camera.width":
		v, err := strconv.Atoi(raw)
		cfg.Camera.Width = v
		return err
	case "camera.height":
		v, err := strconv.Atoi(raw)
		cfg.Camera.Height = v
		return err
	case "camera.fps":
		v, err := strconv.Atoi(raw)
		cfg.Camera.FPS = v
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
	case "output.output_format":
		v, err := parseString(raw)
		if err != nil {
			return err
		}
		if err := ValidateOutputFormat(v); err != nil {
			return err
		}
		cfg.Output.OutputFormat = v
		return nil
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
	case "fx.enabled":
		v, err := strconv.ParseBool(raw)
		cfg.FX.Enabled = v
		return err
	case "fx.mode":
		v, err := parseString(raw)
		if err != nil {
			return err
		}
		if err := ValidateBackgroundMode(v); err != nil {
			return err
		}
		cfg.FX.Mode = v
		return nil
	case "fx.background_image":
		v, err := parseString(raw)
		cfg.FX.BackgroundImage = v
		return err
	case "fx.chroma_color":
		v, err := parseString(raw)
		if err != nil {
			return err
		}
		if err := ValidateChromaColor(v); err != nil {
			return err
		}
		cfg.FX.ChromaColor = v
		return nil
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
	case "audio.mode":
		v, err := parseString(raw)
		if err != nil {
			return err
		}
		if err := ValidateAudioMode(v); err != nil {
			return err
		}
		cfg.Audio.Mode = v
		return nil
	case "audio.input_node":
		v, err := parseString(raw)
		cfg.Audio.InputNode = v
		return err
	case "audio.monitor_enabled":
		v, err := strconv.ParseBool(raw)
		cfg.Audio.MonitorEnabled = v
		return err
	case "audio.monitor_output_node":
		v, err := parseString(raw)
		cfg.Audio.MonitorOutputNode = v
		return err
	case "audio.dereverb_denoiser_intensity":
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return err
		}
		if err := ValidateAudioIntensity(v); err != nil {
			return err
		}
		cfg.Audio.DereverbDenoiserIntensity = v
		return nil
	case "audio.sdk_path":
		v, err := parseString(raw)
		cfg.Audio.SDKPath = v
		return err
	case "audio.output_node_name":
		v, err := parseString(raw)
		cfg.Audio.OutputNodeName = v
		return err
	case "audio.output_description":
		v, err := parseString(raw)
		cfg.Audio.OutputDescription = v
		return err
	case "light.enabled":
		v, err := strconv.ParseBool(raw)
		cfg.Light.Enabled = v
		return err
	case "light.address":
		v, err := parseString(raw)
		cfg.Light.Address = v
		return err
	case "light.brightness":
		v, err := strconv.Atoi(raw)
		if err != nil {
			return err
		}
		if err := ValidateLightBrightness(v); err != nil {
			return err
		}
		cfg.Light.Brightness = v
		return nil
	case "light.temperature":
		v, err := strconv.Atoi(raw)
		if err != nil {
			return err
		}
		if err := ValidateLightTemperature(v); err != nil {
			return err
		}
		cfg.Light.Temperature = v
		return nil
	case "light.timeout_ms":
		v, err := strconv.Atoi(raw)
		if err != nil {
			return err
		}
		if err := ValidateLightTimeout(v); err != nil {
			return err
		}
		cfg.Light.TimeoutMS = v
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

func ValidateAudioMode(mode string) error {
	switch mode {
	case "off", "dereverb_denoiser", "studio_voice_low_latency":
		return nil
	default:
		return fmt.Errorf("invalid audio mode %q: expected off, dereverb_denoiser, or studio_voice_low_latency", mode)
	}
}

func ValidateAudioIntensity(value float64) error {
	if value < 0 || value > 1 {
		return fmt.Errorf("invalid dereverb_denoiser_intensity %.2f: expected 0-1", value)
	}
	return nil
}

func ValidateLightBrightness(value int) error {
	if value < 0 || value > 100 {
		return fmt.Errorf("invalid light brightness %d: expected 0-100", value)
	}
	return nil
}

func ValidateLightTemperature(value int) error {
	if value < 143 || value > 344 {
		return fmt.Errorf("invalid light temperature %d: expected 143-344", value)
	}
	return nil
}

func ValidateLightTimeout(value int) error {
	if value < 100 || value > 30000 {
		return fmt.Errorf("invalid light timeout_ms %d: expected 100-30000", value)
	}
	return nil
}

func ValidateCameraFormat(format string) error {
	switch strings.ToLower(format) {
	case "nv12", "yuv420p", "yu12":
		return nil
	default:
		return fmt.Errorf("invalid camera input_format %q: expected nv12 or yuv420p", format)
	}
}

func ValidateOutputFormat(format string) error {
	switch strings.ToLower(format) {
	case "yuv420p", "yu12":
		return nil
	default:
		return fmt.Errorf("invalid output output_format %q: expected yuv420p", format)
	}
}

func ValidateBackgroundMode(mode string) error {
	switch mode {
	case "blur", "mask", "replace", "chroma":
		return nil
	default:
		return fmt.Errorf("invalid fx mode %q: expected blur, mask, replace, or chroma", mode)
	}
}

func ValidateChromaColor(value string) error {
	if len(value) != 7 || value[0] != '#' {
		return fmt.Errorf("invalid fx chroma_color %q: expected #rrggbb", value)
	}
	for _, ch := range value[1:] {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
			return fmt.Errorf("invalid fx chroma_color %q: expected #rrggbb", value)
		}
	}
	return nil
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
