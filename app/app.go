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

	"nv-vcam/internal/capture"
	"nv-vcam/internal/config"
	"nv-vcam/internal/devices"
	"nv-vcam/internal/loopback"
	svc "nv-vcam/internal/service"
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
	Capture              capture.Snapshot `json:"capture"`
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
	active, out, err := manager.Active(a.timeout())
	serviceErr := ""
	if err != nil {
		serviceErr = compactError(err)
	}
	_, loopbackErr := os.Stat(cfg.Loopback.ConfigPath)
	captureSnapshot := capture.Snapshot{
		State:        capture.StateDisabled,
		Device:       cfg.Capture.Device,
		Dependencies: capture.MissingDependencies(cfg),
		Message:      "capture state file not found; start nv-vcam.service",
	}
	if statePath, err := capture.DefaultStatePath(); err == nil {
		if snap, ok := capture.ReadState(statePath); ok {
			captureSnapshot = snap
		}
	}

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
		ExpectedInput:        cfg.Input.Device,
		ExpectedInputExists:  devices.DeviceExists(cfg.Input.Device),
		ExpectedOutput:       cfg.Output.Device,
		ExpectedOutputExists: devices.DeviceExists(cfg.Output.Device),
		ConfigRendered:       config.Render(cfg),
		Capture:              captureSnapshot,
	}
}

func (a *App) ListDevices() []devices.Device {
	devs, _ := devices.ListDefault()
	return devs
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
		warning = "Another active v4l2loopback config exists. nv-vcam write will refuse unless force is enabled."
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
			return loopback.WriteConfig(cfg, force, true, "nv-vcam")
		}
		return a.writeLoopbackElevated(cfg, force)
	})
	return result("loopback write completed", out, err)
}

func (a *App) ReloadLoopback(dryRun bool) ActionResult {
	cfg := loadEffectiveConfig()
	out, err := captureOutput(func() error {
		if dryRun {
			return loopback.Reload(a.timeout(), cfg, true)
		}
		return a.reloadLoopbackElevated(cfg)
	})
	return result("loopback reload completed", out, err)
}

func (a *App) InstallService(force bool, enable bool, start bool, dryRun bool) ActionResult {
	cfg := loadEffectiveConfig()
	out, err := captureOutput(func() error {
		return svc.Install(a.timeout(), cfg, force, dryRun, enable, start)
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
	out, err := manager.Status(a.timeout())
	return result("service status completed", out, err)
}

func (a *App) serviceAction(message string, fn func(context.Context, svc.Manager) error) ActionResult {
	cfg := loadEffectiveConfig()
	manager := svc.New(cfg.Service.Name)
	out, err := captureOutput(func() error {
		return fn(a.timeout(), manager)
	})
	return result(message, out, err)
}

func (a *App) timeout() context.Context {
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, _ := context.WithTimeout(base, 15*time.Second)
	return ctx
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
		return fmt.Errorf("refusing to write because other v4l2loopback config files exist: %s\nrerun with force enabled if you intentionally want nv-vcam to coexist with them", strings.Join(conflicts, ", "))
	}
	temp, err := os.CreateTemp("", "nv-vcam-v4l2loopback-*.conf")
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
	_, err = runPkexec(a.timeout(), "install", "-m", "0644", temp.Name(), cfg.Loopback.ConfigPath)
	if err != nil {
		return fmt.Errorf("%w\nfallback: %s", err, loopbackWriteFallback(force))
	}
	fmt.Printf("wrote %s\n", cfg.Loopback.ConfigPath)
	return nil
}

func loopbackWriteFallback(force bool) string {
	home, err := os.UserHomeDir()
	binary := "nv-vcam"
	if err == nil {
		binary = filepath.Join(home, ".local", "bin", "nv-vcam")
	}
	cmd := "sudo " + binary + " loopback write"
	if force {
		cmd += " --force"
	}
	return cmd
}

func (a *App) reloadLoopbackElevated(cfg config.Config) error {
	manager := svc.New(cfg.Service.Name)
	if err := manager.Stop(a.timeout(), false); err != nil {
		fmt.Printf("warning: could not stop %s before reload: %v\n", cfg.Service.Name, err)
	}
	script := "modprobe -r v4l2loopback && modprobe v4l2loopback"
	if out, err := runPkexec(a.timeout(), "sh", "-c", script); err != nil {
		if strings.TrimSpace(out) != "" {
			fmt.Println(strings.TrimSpace(out))
		}
		return fmt.Errorf("%w\nfallback:\n  sudo modprobe -r v4l2loopback\n  sudo modprobe v4l2loopback\nif devices are busy, try: fuser -v /dev/video10 /dev/video20", err)
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
