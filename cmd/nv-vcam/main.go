package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nv-vcam/internal/capture"
	"nv-vcam/internal/config"
	"nv-vcam/internal/devices"
	"nv-vcam/internal/fx"
	"nv-vcam/internal/loopback"
	"nv-vcam/internal/runner"
	svc "nv-vcam/internal/service"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		usage()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	switch args[1] {
	case "status":
		return status(ctx)
	case "list":
		return list(ctx)
	case "config":
		return configCmd(args[2:])
	case "loopback":
		return loopbackCmd(ctx, args[0], args[2:])
	case "service":
		return serviceCmd(ctx, args[2:])
	case "fx":
		return fxCmd(args[2:])
	case "run":
		cfg := loadEffectiveConfig()
		return runner.Run(context.Background(), cfg)
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[1])
	}
}

func usage() {
	fmt.Println(`usage:
  nv-vcam status
  nv-vcam list
  nv-vcam config show
  nv-vcam config write [--force] [--dry-run]
  nv-vcam loopback show
  nv-vcam loopback write [--force] [--dry-run]
  nv-vcam loopback reload [--dry-run]
  nv-vcam service install [--force] [--dry-run] [--enable] [--start]
  nv-vcam service start|stop|restart|status [--dry-run]
  nv-vcam fx doctor
  nv-vcam fx test-image --input path --output path [--mask path]
  nv-vcam run`)
}

func fxCmd(args []string) error {
	if len(args) < 1 {
		return errors.New("fx requires test-image")
	}
	switch args[0] {
	case "doctor":
		cfg := loadEffectiveConfig()
		result := fx.Doctor(cfg)
		fmt.Println("fx doctor")
		fmt.Printf("onnxruntime: %s\n", result.RuntimeLibraryPath)
		fmt.Printf("provider: %s\n", result.Provider)
		fmt.Printf("device_id: %d\n", result.DeviceID)
		fmt.Printf("runtime_ok: %t\n", result.RuntimeOK)
		fmt.Printf("cuda_provider_ok: %t\n", result.CUDAProviderOK)
		fmt.Printf("model: %s\n", result.ModelPath)
		fmt.Printf("model_exists: %t\n", result.ModelExists)
		fmt.Printf("message: %s\n", result.Message)
		if !result.RuntimeOK || (strings.EqualFold(result.Provider, "cuda") && !result.CUDAProviderOK) {
			return errors.New("fx runtime check failed")
		}
		return nil
	case "test-image":
		fs := flag.NewFlagSet("fx test-image", flag.ContinueOnError)
		input := fs.String("input", "", "input image path")
		output := fs.String("output", "", "output preview image path")
		mask := fs.String("mask", "", "optional output mask image path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := fx.RunTestImage(fx.TestImageOptions{
			InputPath:  *input,
			OutputPath: *output,
			MaskPath:   *mask,
		})
		if err != nil {
			return err
		}
		fmt.Printf("fx test-image completed\n")
		fmt.Printf("input: %s\n", result.InputPath)
		fmt.Printf("output: %s\n", result.OutputPath)
		if result.MaskPath != "" {
			fmt.Printf("mask: %s\n", result.MaskPath)
		}
		fmt.Printf("size: %dx%d\n", result.Width, result.Height)
		fmt.Printf("runtime: %s\n", result.Runtime)
		return nil
	default:
		return fmt.Errorf("unknown fx command %q", args[0])
	}
}

func configCmd(args []string) error {
	if len(args) < 1 {
		return errors.New("config requires show or write")
	}
	switch args[0] {
	case "show":
		cfg := loadEffectiveConfig()
		fmt.Print(config.Render(cfg))
		return nil
	case "write":
		fs := flag.NewFlagSet("config write", flag.ContinueOnError)
		force := fs.Bool("force", false, "overwrite existing config")
		dryRun := fs.Bool("dry-run", false, "print changes without writing")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return writeConfig(*force, *dryRun)
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func writeConfig(force, dryRun bool) error {
	path, err := config.DefaultPath()
	if err != nil {
		return err
	}
	rendered := config.Render(config.Default())
	if dryRun {
		fmt.Printf("would write %s:\n%s", path, rendered)
		return nil
	}
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists; use --force to overwrite", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(rendered), 0o644)
}

func loopbackCmd(ctx context.Context, argv0 string, args []string) error {
	if len(args) < 1 {
		return errors.New("loopback requires show, write, or reload")
	}
	cfg := loadEffectiveConfig()
	switch args[0] {
	case "show":
		return loopbackShow(cfg)
	case "write":
		fs := flag.NewFlagSet("loopback write", flag.ContinueOnError)
		force := fs.Bool("force", false, "overwrite and bypass conflict checks")
		dryRun := fs.Bool("dry-run", false, "print changes without writing")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return loopback.WriteConfig(cfg, *force, *dryRun, argv0)
	case "reload":
		fs := flag.NewFlagSet("loopback reload", flag.ContinueOnError)
		dryRun := fs.Bool("dry-run", false, "print commands without running")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return loopback.Reload(ctx, cfg, *dryRun)
	default:
		return fmt.Errorf("unknown loopback command %q", args[0])
	}
}

func loopbackShow(cfg config.Config) error {
	found, err := loopback.FindConfigs("/etc/modprobe.d", cfg.Loopback.ConfigPath)
	if err != nil {
		return err
	}
	if len(found) == 0 {
		fmt.Println("no v4l2loopback config files found in /etc/modprobe.d")
		return nil
	}
	fmt.Printf("nv-vcam config path: %s\n", cfg.Loopback.ConfigPath)
	for _, item := range found {
		marker := ""
		if item.IsNV {
			marker = " (nv-vcam)"
		}
		fmt.Printf("\n== %s%s ==\n%s", item.Path, marker, item.Content)
		if !strings.HasSuffix(item.Content, "\n") {
			fmt.Println()
		}
	}
	if len(found) > 1 {
		fmt.Println("\nwarning: multiple v4l2loopback config files may conflict during module load")
	}
	return nil
}

func serviceCmd(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return errors.New("service requires install, start, stop, restart, or status")
	}
	cfg := loadEffectiveConfig()
	manager := svc.New(cfg.Service.Name)
	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("service install", flag.ContinueOnError)
		force := fs.Bool("force", false, "overwrite service file")
		dryRun := fs.Bool("dry-run", false, "print changes without writing or running")
		enable := fs.Bool("enable", false, "enable service after install")
		start := fs.Bool("start", false, "start service after install")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return svc.Install(ctx, cfg, *force, *dryRun, *enable, *start)
	case "start":
		dryRun, err := dryRunOnly(args[1:], "service start")
		if err != nil {
			return err
		}
		return manager.Start(ctx, dryRun)
	case "stop":
		dryRun, err := dryRunOnly(args[1:], "service stop")
		if err != nil {
			return err
		}
		return manager.Stop(ctx, dryRun)
	case "restart":
		dryRun, err := dryRunOnly(args[1:], "service restart")
		if err != nil {
			return err
		}
		return manager.Restart(ctx, dryRun)
	case "status":
		out, err := manager.Status(ctx)
		if out != "" {
			fmt.Print(out)
		}
		return err
	default:
		return fmt.Errorf("unknown service command %q", args[0])
	}
}

func dryRunOnly(args []string, name string) (bool, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "print command without running")
	if err := fs.Parse(args); err != nil {
		return false, err
	}
	return *dryRun, nil
}

func list(ctx context.Context) error {
	devs, err := devices.ListDefault()
	if err != nil {
		return err
	}
	printDevices(devs)
	if out, err := devices.V4L2CtlList(ctx); err == nil && strings.TrimSpace(out) != "" {
		fmt.Println("\nv4l2-ctl --list-devices:")
		fmt.Print(out)
	}
	return nil
}

func status(ctx context.Context) error {
	cfg := loadEffectiveConfig()
	fmt.Println("devices:")
	devs, err := devices.ListDefault()
	if err != nil {
		fmt.Printf("  error: %v\n", err)
	} else {
		printDevices(devs)
	}
	fmt.Printf("\nv4l2loopback loaded: %t\n", loopback.ModuleLoaded())

	if _, err := os.Stat(cfg.Loopback.ConfigPath); err == nil {
		fmt.Printf("nv-vcam loopback config: exists (%s)\n", cfg.Loopback.ConfigPath)
	} else if os.IsNotExist(err) {
		fmt.Printf("nv-vcam loopback config: missing (%s)\n", cfg.Loopback.ConfigPath)
	} else {
		fmt.Printf("nv-vcam loopback config: error: %v\n", err)
	}

	manager := svc.New(cfg.Service.Name)
	fmt.Printf("systemd user service file: %t\n", manager.Exists())
	active, _, err := manager.Active(ctx)
	if err != nil {
		fmt.Printf("systemd user service active: unknown (%s)\n", compactError(err))
	} else {
		fmt.Printf("systemd user service active: %t\n", active)
	}
	fmt.Printf("expected input %s exists: %t\n", cfg.Input.Device, devices.DeviceExists(cfg.Input.Device))
	fmt.Printf("expected output %s exists: %t\n", cfg.Output.Device, devices.DeviceExists(cfg.Output.Device))
	fmt.Printf("capture device: %s\n", cfg.Capture.Device)
	if missing := capture.MissingDependencies(cfg); len(missing) > 0 {
		fmt.Printf("capture dependencies: missing %s\n", strings.Join(missing, ", "))
	} else {
		fmt.Println("capture dependencies: ok")
	}
	if statePath, err := capture.DefaultStatePath(); err == nil {
		if snap, ok := capture.ReadState(statePath); ok {
			fmt.Printf("capture state: %s (%s), consumers=%d, updated=%s\n", snap.State, snap.Message, snap.Consumers, snap.UpdatedAt)
		} else {
			fmt.Printf("capture state: unavailable (%s missing or unreadable)\n", statePath)
		}
	}
	return nil
}

func printDevices(devs []devices.Device) {
	if len(devs) == 0 {
		fmt.Println("  no video devices detected")
		return
	}
	for _, dev := range devs {
		fmt.Printf("  %-12s %-20s %s\n", dev.Path, dev.SysName, dev.Name)
	}
}

func loadEffectiveConfig() config.Config {
	path, err := config.DefaultPath()
	if err != nil {
		return config.Default()
	}
	cfg, err := config.Load(path)
	if err != nil {
		return config.Default()
	}
	return cfg
}

func compactError(err error) string {
	if err == nil {
		return ""
	}
	lines := strings.Split(err.Error(), "\n")
	var kept []string
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
