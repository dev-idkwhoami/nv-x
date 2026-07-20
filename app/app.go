package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"nv-x/internal/audio"
	"nv-x/internal/config"
	"nv-x/internal/devices"
	"nv-x/internal/fx"
	"nv-x/internal/loopback"
	svc "nv-x/internal/service"
)

type App struct {
	ctx context.Context
}

type ActionResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Output  string `json:"output"`
}

type ThemeView struct {
	Theme string `json:"theme"`
}

type ConfigView struct {
	Path     string        `json:"path"`
	Found    bool          `json:"found"`
	Rendered string        `json:"rendered"`
	Config   config.Config `json:"config"`
}

type UserSettings struct {
	CameraInput       string  `json:"cameraInput"`
	Mode              string  `json:"mode"`
	AudioMode         string  `json:"audioMode"`
	AudioInputNode    string  `json:"audioInputNode"`
	AudioIntensity    float64 `json:"audioIntensity"`
	MonitorEnabled    bool    `json:"monitorEnabled"`
	MonitorOutputNode string  `json:"monitorOutputNode"`
	LightEnabled      bool    `json:"lightEnabled"`
	LightAddress      string  `json:"lightAddress"`
	LightBrightness   int     `json:"lightBrightness"`
	LightTemperature  int     `json:"lightTemperature"`
	BlurStrength      float64 `json:"blurStrength"`
	ChromaColor       string  `json:"chromaColor"`
	BackgroundImage   string  `json:"backgroundImage"`
	Theme             string  `json:"theme"`
}

type LoopbackView struct {
	TargetPath string                 `json:"targetPath"`
	Found      []loopback.FoundConfig `json:"found"`
	Conflict   bool                   `json:"conflict"`
	Warning    string                 `json:"warning"`
	Rendered   string                 `json:"rendered"`
}

type ServiceView struct {
	Name   string `json:"name"`
	Exists bool   `json:"exists"`
	Active bool   `json:"active"`
	Error  string `json:"error"`
	Output string `json:"output"`
}

type AppStatus struct {
	Devices              []devices.Device `json:"devices"`
	V4L2LoopbackLoaded   bool             `json:"v4l2LoopbackLoaded"`
	LoopbackConfigExists bool             `json:"loopbackConfigExists"`
	LoopbackConfigPath   string           `json:"loopbackConfigPath"`
	Service              ServiceView      `json:"service"`
	ExpectedInput        string           `json:"expectedInput"`
	ExpectedInputExists  bool             `json:"expectedInputExists"`
	ExpectedOutput       string           `json:"expectedOutput"`
	ExpectedOutputExists bool             `json:"expectedOutputExists"`
	ConfigRendered       string           `json:"configRendered"`
	FX                   fx.Snapshot      `json:"fx"`
	Audio                audio.Snapshot   `json:"audio"`
	AudioSources         []audio.Source   `json:"audioSources"`
	AudioSinks           []audio.Source   `json:"audioSinks"`
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) GetStatus() AppStatus {
	cfg := loadEffectiveConfig()
	devs, _ := devices.ListDefault()
	manager := svc.New(cfg.Service.Name)
	ctx, cancel := a.timeout()
	defer cancel()
	active, out, err := manager.Active(ctx)
	serviceErr := ""
	if err != nil {
		serviceErr = compactError(err)
	}
	_, loopbackErr := os.Stat(cfg.Loopback.ConfigPath)
	fxSnapshot := fx.Snapshot{
		State:        fx.StateDisabled,
		Device:       cfg.Output.Device,
		Dependencies: fx.MissingDependencies(cfg),
		Message:      "fx state file not found; start nv-x.service",
	}
	if statePath, err := fx.DefaultStatePath(); err == nil {
		if snap, ok := fx.ReadState(statePath); ok {
			fxSnapshot = snap
		}
	}
	audioSnapshot := audio.Snapshot{State: audio.StateDisabled, Mode: cfg.Audio.Mode, InputNode: cfg.Audio.InputNode, OutputNode: cfg.Audio.OutputNodeName, Message: "audio state file not found; start nv-x.service"}
	if statePath, err := audio.DefaultStatePath(); err == nil {
		if snap, ok := audio.ReadState(statePath); ok {
			audioSnapshot = snap
		}
	}
	audioSources, _ := audio.ListSources(ctx, cfg.Audio.OutputNodeName)
	audioSinks, _ := audio.ListSinks(ctx)

	return AppStatus{
		Devices:              devs,
		V4L2LoopbackLoaded:   loopback.ModuleLoaded(),
		LoopbackConfigExists: loopbackErr == nil,
		LoopbackConfigPath:   cfg.Loopback.ConfigPath,
		Service: ServiceView{
			Name:   cfg.Service.Name,
			Exists: manager.Exists(),
			Active: active,
			Error:  serviceErr,
			Output: strings.TrimSpace(out),
		},
		ExpectedInput:        cfg.Camera.InputDevice,
		ExpectedInputExists:  devices.DeviceExists(cfg.Camera.InputDevice),
		ExpectedOutput:       cfg.Output.Device,
		ExpectedOutputExists: devices.DeviceExists(cfg.Output.Device),
		ConfigRendered:       config.Render(cfg),
		FX:                   fxSnapshot,
		Audio:                audioSnapshot,
		AudioSources:         audioSources,
		AudioSinks:           audioSinks,
	}
}

func (a *App) ListDevices() []devices.Device {
	devs, _ := devices.ListDefault()
	return devs
}

func (a *App) ListAudioSources() []audio.Source {
	cfg := loadEffectiveConfig()
	ctx, cancel := a.timeout()
	defer cancel()
	sources, _ := audio.ListSources(ctx, cfg.Audio.OutputNodeName)
	return sources
}

func (a *App) GetConfig() ConfigView {
	path, _ := config.DefaultPath()
	cfg, found := loadConfigWithFound()
	return ConfigView{
		Path:     path,
		Found:    found,
		Rendered: config.Render(cfg),
		Config:   cfg,
	}
}

func (a *App) WriteConfig(force bool, dryRun bool) ActionResult {
	out, err := captureOutput(func() error {
		path, err := config.DefaultPath()
		if err != nil {
			return err
		}
		cfg := loadEffectiveConfig()
		rendered := config.Render(cfg)
		if dryRun {
			fmt.Printf("would write %s:\n%s", path, rendered)
			return nil
		}
		if _, err := os.Stat(path); err == nil && !force {
			return fmt.Errorf("%s already exists; use force to overwrite", path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(rendered), 0o644)
	})
	return result("config write completed", out, err)
}

func (a *App) GetTheme() ThemeView {
	cfg := loadEffectiveConfig()
	return ThemeView{Theme: cfg.UI.Theme}
}

func (a *App) SetTheme(theme string) ActionResult {
	if err := config.ValidateTheme(theme); err != nil {
		return result("", "", err)
	}
	cfg := loadEffectiveConfig()
	cfg.UI.Theme = theme
	out, err := captureOutput(func() error {
		return saveConfig(cfg)
	})
	return result("theme updated", out, err)
}

func (a *App) SaveUserSettings(settings UserSettings) ActionResult {
	cfg := loadEffectiveConfig()
	if err := config.ValidateBackgroundMode(settings.Mode); err != nil {
		return result("", "", err)
	}
	if err := config.ValidateAudioMode(settings.AudioMode); err != nil {
		return result("", "", err)
	}
	if err := config.ValidateAudioIntensity(settings.AudioIntensity); err != nil {
		return result("", "", err)
	}
	if err := config.ValidateLightBrightness(settings.LightBrightness); err != nil {
		return result("", "", err)
	}
	if err := config.ValidateLightTemperature(settings.LightTemperature); err != nil {
		return result("", "", err)
	}
	if settings.BlurStrength < 0 || settings.BlurStrength > 1 {
		return result("", "", fmt.Errorf("invalid blur strength %.2f: expected 0-1", settings.BlurStrength))
	}
	if err := config.ValidateChromaColor(settings.ChromaColor); err != nil {
		return result("", "", err)
	}
	if err := config.ValidateTheme(settings.Theme); err != nil {
		return result("", "", err)
	}
	cfg.FX.Mode = settings.Mode
	if strings.TrimSpace(settings.CameraInput) == "" {
		return result("", "", fmt.Errorf("camera input is required"))
	}
	cfg.Camera.InputDevice = strings.TrimSpace(settings.CameraInput)
	cfg.Audio.Mode = settings.AudioMode
	cfg.Audio.InputNode = strings.TrimSpace(settings.AudioInputNode)
	cfg.Audio.DereverbDenoiserIntensity = settings.AudioIntensity
	cfg.Audio.MonitorEnabled = settings.MonitorEnabled
	cfg.Audio.MonitorOutputNode = strings.TrimSpace(settings.MonitorOutputNode)
	cfg.FX.BlurStrength = settings.BlurStrength
	cfg.FX.ChromaColor = settings.ChromaColor
	cfg.FX.BackgroundImage = strings.TrimSpace(settings.BackgroundImage)
	cfg.Light.Enabled = settings.LightEnabled
	cfg.Light.Address = strings.TrimSpace(settings.LightAddress)
	cfg.Light.Brightness = settings.LightBrightness
	cfg.Light.Temperature = settings.LightTemperature
	cfg.UI.Theme = settings.Theme
	out, err := captureOutput(func() error {
		return saveConfig(cfg)
	})
	return result("settings updated", out, err)
}

func (a *App) ChooseBackgroundImage() (string, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title: "Select background image",
		Filters: []runtime.FileFilter{
			{DisplayName: "Images", Pattern: "*.jpg;*.jpeg;*.png"},
			{DisplayName: "All Files", Pattern: "*"},
		},
	})
}

func (a *App) ShowLoopback() LoopbackView {
	cfg := loadEffectiveConfig()
	found, _ := loopback.FindConfigs("/etc/modprobe.d", cfg.Loopback.ConfigPath)
	conflict := false
	for _, item := range found {
		if !item.IsNV {
			conflict = true
			break
		}
	}
	warning := ""
	if conflict {
		warning = "Another active v4l2loopback config exists. nv-x write will refuse unless force is enabled."
	}
	return LoopbackView{
		TargetPath: cfg.Loopback.ConfigPath,
		Found:      found,
		Conflict:   conflict,
		Warning:    warning,
		Rendered:   loopback.Render(cfg),
	}
}

func (a *App) WriteLoopback(force bool, dryRun bool) ActionResult {
	cfg := loadEffectiveConfig()
	out, err := captureOutput(func() error {
		if dryRun {
			return loopback.WriteConfig(cfg, force, true, "nv-x")
		}
		return a.writeLoopbackElevated(cfg, force)
	})
	return result("loopback write completed", out, err)
}

func (a *App) ReloadLoopback(dryRun bool) ActionResult {
	cfg := loadEffectiveConfig()
	out, err := captureOutput(func() error {
		if dryRun {
			ctx, cancel := a.timeout()
			defer cancel()
			return loopback.Reload(ctx, cfg, true)
		}
		return a.reloadLoopbackElevated(cfg)
	})
	return result("loopback reload completed", out, err)
}

func (a *App) InstallService(force bool, enable bool, start bool, dryRun bool) ActionResult {
	cfg := loadEffectiveConfig()
	out, err := captureOutput(func() error {
		ctx, cancel := a.timeout()
		defer cancel()
		return svc.Install(ctx, cfg, force, dryRun, enable, start)
	})
	return result("service install completed", out, err)
}

func (a *App) StartService() ActionResult {
	return a.serviceAction("service start completed", func(ctx context.Context, manager svc.Manager) error {
		return manager.Start(ctx, false)
	})
}

func (a *App) StopService() ActionResult {
	return a.serviceAction("service stop completed", func(ctx context.Context, manager svc.Manager) error {
		return manager.Stop(ctx, false)
	})
}

func (a *App) RestartService() ActionResult {
	return a.serviceAction("service restart completed", func(ctx context.Context, manager svc.Manager) error {
		return manager.Restart(ctx, false)
	})
}

func (a *App) GetServiceStatus() ActionResult {
	cfg := loadEffectiveConfig()
	manager := svc.New(cfg.Service.Name)
	ctx, cancel := a.timeout()
	defer cancel()
	out, err := manager.Status(ctx)
	return result("service status completed", out, err)
}

func (a *App) serviceAction(message string, fn func(context.Context, svc.Manager) error) ActionResult {
	cfg := loadEffectiveConfig()
	manager := svc.New(cfg.Service.Name)
	out, err := captureOutput(func() error {
		ctx, cancel := a.timeout()
		defer cancel()
		return fn(ctx, manager)
	})
	return result(message, out, err)
}

func (a *App) timeout() (context.Context, context.CancelFunc) {
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(base, 15*time.Second)
}

func loadConfigWithFound() (config.Config, bool) {
	path, err := config.DefaultPath()
	if err != nil {
		return config.Default(), false
	}
	cfg, err := config.Load(path)
	if err != nil {
		return config.Default(), false
	}
	return cfg, true
}

func loadEffectiveConfig() config.Config {
	cfg, _ := loadConfigWithFound()
	return cfg
}

func saveConfig(cfg config.Config) error {
	path, err := config.DefaultPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(config.Render(cfg)), 0o644)
}

func (a *App) writeLoopbackElevated(cfg config.Config, force bool) error {
	found, err := loopback.FindConfigs(filepath.Dir(cfg.Loopback.ConfigPath), cfg.Loopback.ConfigPath)
	if err != nil {
		return err
	}
	var conflicts []string
	for _, item := range found {
		if !item.IsNV {
			conflicts = append(conflicts, item.Path)
		}
	}
	if len(conflicts) > 0 && !force {
		return fmt.Errorf("refusing to write because other v4l2loopback config files exist: %s\nrerun with force enabled if you intentionally want nv-x to coexist with them", strings.Join(conflicts, ", "))
	}
	temp, err := os.CreateTemp("", "nv-x-v4l2loopback-*.conf")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.WriteString(loopback.Render(cfg)); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temp.Name(), 0o644); err != nil {
		return err
	}
	ctx, cancel := a.timeout()
	defer cancel()
	_, err = runPkexec(ctx, "install", "-m", "0644", temp.Name(), cfg.Loopback.ConfigPath)
	if err != nil {
		return fmt.Errorf("%w\nfallback: %s", err, loopbackWriteFallback(force))
	}
	fmt.Printf("wrote %s\n", cfg.Loopback.ConfigPath)
	return nil
}

func loopbackWriteFallback(force bool) string {
	home, err := os.UserHomeDir()
	binary := "nv-x"
	if err == nil {
		binary = filepath.Join(home, ".local", "bin", "nv-x")
	}
	cmd := "sudo " + binary + " loopback write"
	if force {
		cmd += " --force"
	}
	return cmd
}

func (a *App) reloadLoopbackElevated(cfg config.Config) error {
	manager := svc.New(cfg.Service.Name)
	ctx, cancel := a.timeout()
	defer cancel()
	if err := manager.Stop(ctx, false); err != nil {
		fmt.Printf("warning: could not stop %s before reload: %v\n", cfg.Service.Name, err)
	}
	script := "modprobe -r v4l2loopback && modprobe v4l2loopback"
	if out, err := runPkexec(ctx, "sh", "-c", script); err != nil {
		if strings.TrimSpace(out) != "" {
			fmt.Println(strings.TrimSpace(out))
		}
		cfg := loadEffectiveConfig()
		return fmt.Errorf("%w\nfallback:\n  sudo modprobe -r v4l2loopback\n  sudo modprobe v4l2loopback\nif devices are busy, try: fuser -v %s", err, cfg.Output.Device)
	} else if strings.TrimSpace(out) != "" {
		fmt.Println(strings.TrimSpace(out))
	}
	fmt.Println("reloaded v4l2loopback")
	return nil
}

func runPkexec(ctx context.Context, args ...string) (string, error) {
	if _, err := exec.LookPath("pkexec"); err != nil {
		return "", fmt.Errorf("pkexec is not available: %w", err)
	}
	cmd := exec.CommandContext(ctx, "pkexec", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return buf.String(), fmt.Errorf("pkexec %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(buf.String()))
	}
	return buf.String(), nil
}

func result(success string, output string, err error) ActionResult {
	output = strings.TrimSpace(output)
	if err != nil {
		return ActionResult{
			OK:      false,
			Message: compactError(err),
			Output:  output,
		}
	}
	return ActionResult{
		OK:      true,
		Message: success,
		Output:  output,
	}
}

func captureOutput(fn func() error) (string, error) {
	old := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = old
	}()

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&buf, reader)
		done <- copyErr
	}()

	runErr := fn()
	closeErr := writer.Close()
	copyErr := <-done
	if closeErr != nil && runErr == nil {
		runErr = closeErr
	}
	if copyErr != nil && runErr == nil {
		runErr = copyErr
	}
	return buf.String(), runErr
}

func compactError(err error) string {
	if err == nil {
		return ""
	}
	lines := strings.Split(err.Error(), "\n")
	kept := make([]string, 0, len(lines))
	seen := map[string]bool{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		kept = append(kept, line)
	}
	return strings.Join(kept, "; ")
}
