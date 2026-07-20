package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"nv-x/internal/audio"
	"nv-x/internal/config"
	"nv-x/internal/devices"
	"nv-x/internal/fx"
	"nv-x/internal/loopback"
	"nv-x/internal/runner"
	svc "nv-x/internal/service"
	"nv-x/internal/setup"
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
	case "audio":
		return audioCmd(ctx, args[2:])
	case "setup":
		setupCtx, setupCancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer setupCancel()
		return setupCmd(setupCtx, args[2:])
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
  nv-x status
  nv-x list
  nv-x config show
  nv-x config write [--force] [--dry-run]
  nv-x loopback show
  nv-x loopback write [--force] [--dry-run]
  nv-x loopback reload [--dry-run]
  nv-x service install [--force] [--dry-run] [--enable] [--start]
  nv-x service start|stop|restart|status [--dry-run]
  nv-x fx doctor
	 nv-x audio list
	 nv-x audio doctor
  nv-x fx test-image --input path --blur-output path --removed-output path [--mask path] [--final-output path] [--background blur|mask|replace|chroma] [--background-image path] [--chroma-color #00ff00] [--blur-strength value]
  nv-x fx stream [--input /dev/video0] [--output /dev/video10] [--width 1920] [--height 1080] [--fps 50] [--background blur|mask|replace|chroma] [--background-image path] [--chroma-color #00ff00] [--blur-strength value]
  nv-x fx transfer [--input /dev/video0] [--output /dev/video10] [--width 1920] [--height 1080] [--fps 50]
  nv-x setup [--force] [--dry-run] [--skip-video] [--skip-audio] [--skip-loopback] [--skip-service]
  nv-x run`)
}

func fxCmd(args []string) error {
	if len(args) < 1 {
		return errors.New("fx requires doctor, test-image, stream, or transfer")
	}
	switch args[0] {
	case "doctor":
		cfg := loadEffectiveConfig()
		result := fx.Doctor(cfg)
		fmt.Println("fx doctor")
		fmt.Printf("backend: maxine\n")
		fmt.Printf("sdk_path: %s\n", result.SDKPath)
		fmt.Printf("model_dir: %s\n", result.ModelDir)
		fmt.Printf("helper: %s\n", result.HelperPath)
		fmt.Printf("os_release_shim: %t\n", result.OSReleaseShim)
		if result.ShimPath != "" {
			fmt.Printf("shim: %s\n", result.ShimPath)
		}
		if result.SDKVersion != "" {
			fmt.Printf("sdk_version: %s\n", result.SDKVersion)
		}
		fmt.Printf("sdk_exists: %t\n", result.SDKExists)
		fmt.Printf("features_ok: %t\n", result.FeaturesOK)
		fmt.Printf("models_ok: %t\n", result.ModelsOK)
		fmt.Printf("linker_ok: %t\n", result.LinkerOK)
		fmt.Printf("helper_ok: %t\n", result.HelperOK)
		for _, path := range result.MissingFiles {
			fmt.Printf("missing_file: %s\n", path)
		}
		for _, lib := range result.MissingLibraries {
			fmt.Printf("missing_library: %s\n", lib)
		}
		fmt.Printf("message: %s\n", result.Message)
		if !result.HelperOK {
			return errors.New("Maxine FX runtime check failed")
		}
		return nil
	case "test-image":
		fs := flag.NewFlagSet("fx test-image", flag.ContinueOnError)
		input := fs.String("input", "", "input image path")
		blurOutput := fs.String("blur-output", "", "background blur output image path")
		removedOutput := fs.String("removed-output", "", "transparent background-removed output image path")
		mask := fs.String("mask", "", "optional output mask image path")
		finalOutput := fs.String("final-output", "", "optional selected effect chain output image path")
		backgroundMode := fs.String("background", "", "background mode: blur, mask, replace, or chroma")
		backgroundImage := fs.String("background-image", "", "replacement background image path")
		chromaColor := fs.String("chroma-color", "", "chroma background color as #rrggbb")
		blurStrength := fs.Float64("blur-strength", 0, "background blur strength")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		cfg := loadEffectiveConfig()
		result, err := fx.RunTestImage(cfg, fx.TestImageOptions{
			InputPath:       *input,
			BlurPath:        *blurOutput,
			RemovedPath:     *removedOutput,
			MaskPath:        *mask,
			FinalPath:       *finalOutput,
			BackgroundMode:  *backgroundMode,
			BackgroundImage: *backgroundImage,
			ChromaColor:     *chromaColor,
			BlurStrength:    *blurStrength,
		})
		if err != nil {
			return err
		}
		fmt.Printf("fx test-image completed\n")
		fmt.Printf("input: %s\n", result.InputPath)
		fmt.Printf("blur_output: %s\n", result.BlurPath)
		fmt.Printf("removed_output: %s\n", result.RemovedPath)
		if result.MaskPath != "" {
			fmt.Printf("mask: %s\n", result.MaskPath)
		}
		if result.FinalPath != "" {
			fmt.Printf("final_output: %s\n", result.FinalPath)
		}
		fmt.Printf("size: %dx%d\n", result.Width, result.Height)
		fmt.Printf("runtime: %s\n", result.Runtime)
		fmt.Printf("background: %s\n", result.BackgroundMode)
		if result.BackgroundImage != "" {
			fmt.Printf("background_image: %s\n", result.BackgroundImage)
		}
		fmt.Printf("chroma_color: %s\n", result.ChromaColor)
		fmt.Printf("blur_strength: %.2f\n", result.BlurStrength)
		return nil
	case "stream":
		fs := flag.NewFlagSet("fx stream", flag.ContinueOnError)
		input := fs.String("input", "", "input video device")
		output := fs.String("output", "", "output v4l2loopback video device")
		width := fs.Int("width", 0, "frame width")
		height := fs.Int("height", 0, "frame height")
		fps := fs.Int("fps", 0, "frame rate")
		backgroundMode := fs.String("background", "", "background mode: blur, mask, replace, or chroma")
		backgroundImage := fs.String("background-image", "", "replacement background image path")
		chromaColor := fs.String("chroma-color", "", "chroma background color as #rrggbb")
		blurStrength := fs.Float64("blur-strength", 0, "background blur strength")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		cfg := loadEffectiveConfig()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return fx.RunStream(ctx, cfg, fx.StreamOptions{
			InputDevice:     *input,
			OutputDevice:    *output,
			Width:           *width,
			Height:          *height,
			FPS:             *fps,
			BackgroundMode:  *backgroundMode,
			BackgroundImage: *backgroundImage,
			ChromaColor:     *chromaColor,
			BlurStrength:    *blurStrength,
		}, func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		})
	case "transfer":
		fs := flag.NewFlagSet("fx transfer", flag.ContinueOnError)
		input := fs.String("input", "", "input video device")
		output := fs.String("output", "", "output v4l2loopback video device")
		width := fs.Int("width", 0, "frame width")
		height := fs.Int("height", 0, "frame height")
		fps := fs.Int("fps", 0, "frame rate")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		cfg := loadEffectiveConfig()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return fx.RunTransfer(ctx, cfg, fx.StreamOptions{
			InputDevice:  *input,
			OutputDevice: *output,
			Width:        *width,
			Height:       *height,
			FPS:          *fps,
		}, func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		})
	default:
		return fmt.Errorf("unknown fx command %q", args[0])
	}
}

func audioCmd(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return errors.New("audio requires list or doctor")
	}
	cfg := loadEffectiveConfig()
	switch args[0] {
	case "list":
		sources, err := audio.ListSources(ctx, cfg.Audio.OutputNodeName)
		if err != nil {
			return err
		}
		for _, source := range sources {
			marker := ""
			if source.Default {
				marker = " (default)"
			}
			fmt.Printf("%s\t%s%s\n", source.NodeName, source.Description, marker)
		}
		return nil
	case "doctor":
		result := audio.Doctor(cfg)
		fmt.Printf("audio doctor\nsdk_path: %s\nhelper: %s\n", result.SDKPath, result.HelperPath)
		for _, model := range result.Models {
			fmt.Printf("model: %s\n", model)
		}
		fmt.Printf("message: %s\n", result.Message)
		if result.HelperOutput != "" {
			fmt.Print(result.HelperOutput)
		}
		if !result.HelperOK {
			return errors.New("Maxine AFX runtime check failed")
		}
		return nil
	default:
		return fmt.Errorf("unknown audio command %q", args[0])
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
	fmt.Printf("nv-x config path: %s\n", cfg.Loopback.ConfigPath)
	for _, item := range found {
		marker := ""
		if item.IsNV {
			marker = " (nv-x)"
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

func setupCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	force := fs.Bool("force", false, "overwrite existing config, loopback config, and service file")
	dryRun := fs.Bool("dry-run", false, "print changes and commands without writing or running")
	skipConfig := fs.Bool("skip-config", false, "skip user config creation")
	skipSDK := fs.Bool("skip-sdk", false, "skip Maxine SDK Core extraction")
	skipMaxine := fs.Bool("skip-maxine", false, "skip Maxine feature installation and doctor")
	skipLoopback := fs.Bool("skip-loopback", false, "skip loopback config and reload")
	skipReload := fs.Bool("skip-reload", false, "skip v4l2loopback reload")
	skipService := fs.Bool("skip-service", false, "skip user service install")
	skipVideo := fs.Bool("skip-video", false, "skip video SDK, features, doctor, and loopback setup")
	skipAudio := fs.Bool("skip-audio", false, "skip audio SDK, features, and doctor")
	noEnable := fs.Bool("no-enable", false, "do not enable the user service")
	noStart := fs.Bool("no-start", false, "do not start the user service")
	features := fs.String("features", "nvvfxgreenscreen,nvvfxbackgroundblur", "comma-separated Maxine features to install")
	gpu := fs.String("gpu", "", "optional Maxine feature installer GPU architecture")
	featureVersion := fs.String("feature-version", "", "optional Maxine feature version")
	ngcOrg := fs.String("ngc-org", "nvidia", "NGC org for Maxine feature install")
	ngcTeam := fs.String("ngc-team", "maxine", "NGC team for Maxine feature install")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := loadEffectiveConfig()
	return setup.Run(ctx, cfg, setup.Options{
		Force:        *force,
		DryRun:       *dryRun,
		SkipConfig:   *skipConfig,
		SkipSDK:      *skipSDK,
		SkipMaxine:   *skipMaxine,
		SkipLoopback: *skipLoopback,
		SkipReload:   *skipReload,
		SkipService:  *skipService,
		SkipVideo:    *skipVideo,
		SkipAudio:    *skipAudio,
		Enable:       !*noEnable,
		Start:        !*noStart,
		Features:     *features,
		GPU:          *gpu,
		Version:      *featureVersion,
		NGCOrg:       *ngcOrg,
		NGCTeam:      *ngcTeam,
	})
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
		fmt.Printf("nv-x loopback config: exists (%s)\n", cfg.Loopback.ConfigPath)
	} else if os.IsNotExist(err) {
		fmt.Printf("nv-x loopback config: missing (%s)\n", cfg.Loopback.ConfigPath)
	} else {
		fmt.Printf("nv-x loopback config: error: %v\n", err)
	}

	manager := svc.New(cfg.Service.Name)
	fmt.Printf("systemd user service file: %t\n", manager.Exists())
	active, _, err := manager.Active(ctx)
	if err != nil {
		fmt.Printf("systemd user service active: unknown (%s)\n", compactError(err))
	} else {
		fmt.Printf("systemd user service active: %t\n", active)
	}
	fmt.Printf("expected input %s exists: %t\n", cfg.Camera.InputDevice, devices.DeviceExists(cfg.Camera.InputDevice))
	fmt.Printf("expected output %s exists: %t\n", cfg.Output.Device, devices.DeviceExists(cfg.Output.Device))
	fmt.Printf("camera format: %s %dx%d @ %dfps\n", cfg.Camera.InputFormat, cfg.Camera.Width, cfg.Camera.Height, cfg.Camera.FPS)
	fmt.Printf("output format: %s\n", cfg.Output.OutputFormat)
	fmt.Printf("fx enabled: %t\n", cfg.FX.Enabled)
	fmt.Printf("fx input: %s\n", cfg.Camera.InputDevice)
	fmt.Printf("fx output: %s\n", cfg.Output.Device)
	fmt.Printf("light auto-control: %t\n", cfg.Light.Enabled)
	if cfg.Light.Address != "" {
		fmt.Printf("light address: %s\n", cfg.Light.Address)
	} else {
		fmt.Println("light address: auto from elgato-light-toggle config")
	}
	if missing := fx.MissingDependencies(cfg); len(missing) > 0 {
		fmt.Printf("fx dependencies: missing %s\n", strings.Join(missing, ", "))
	} else {
		fmt.Println("fx dependencies: ok")
	}
	if statePath, err := fx.DefaultStatePath(); err == nil {
		if snap, ok := fx.ReadState(statePath); ok {
			fmt.Printf("fx state: %s (%s), consumers=%d, updated=%s\n", snap.State, snap.Message, snap.Consumers, snap.UpdatedAt)
		} else {
			fmt.Printf("fx state: unavailable (%s missing or unreadable)\n", statePath)
		}
	}
	fmt.Printf("audio mode: %s\n", cfg.Audio.Mode)
	if cfg.Audio.InputNode == "" {
		fmt.Println("audio input: system default")
	} else {
		fmt.Printf("audio input: %s\n", cfg.Audio.InputNode)
	}
	if statePath, err := audio.DefaultStatePath(); err == nil {
		if snap, ok := audio.ReadState(statePath); ok {
			fmt.Printf("audio state: %s (%s), updated=%s\n", snap.State, snap.Message, snap.UpdatedAt)
		} else {
			fmt.Printf("audio state: unavailable (%s missing or unreadable)\n", statePath)
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
