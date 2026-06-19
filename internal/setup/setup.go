package setup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"nv-vcam/internal/config"
	"nv-vcam/internal/fx"
	"nv-vcam/internal/loopback"
	svc "nv-vcam/internal/service"
)

const defaultSDKNGCResource = "nvidia/maxine/vfx_sdk_core:1.2.0.0_linux"

type Options struct {
	Force        bool
	DryRun       bool
	SkipConfig   bool
	SkipSDK      bool
	SkipMaxine   bool
	SkipLoopback bool
	SkipReload   bool
	SkipService  bool
	Enable       bool
	Start        bool
	Features     string
	GPU          string
	Version      string
	NGCOrg       string
	NGCTeam      string
}

func Run(ctx context.Context, cfg config.Config, opts Options) error {
	if os.Geteuid() == 0 && !opts.SkipService {
		return fmt.Errorf("do not run nv-vcam setup with sudo; run it as your normal desktop user so the systemd user service is installed for the right account")
	}
	if opts.Features == "" {
		opts.Features = "nvvfxgreenscreen,nvvfxbackgroundblur"
	}
	if opts.NGCOrg == "" {
		opts.NGCOrg = "nvidia"
	}
	if opts.NGCTeam == "" {
		opts.NGCTeam = "maxine"
	}
	if !opts.SkipConfig {
		if err := ensureConfig(opts.Force, opts.DryRun); err != nil {
			return fmt.Errorf("config setup failed: %w", err)
		}
		if opts.Force {
			cfg = config.Default()
		}
	}
	if needsSudo(opts) {
		fmt.Println("validating sudo access for SDK, loopback, and module setup")
		if err := validateSudo(ctx, opts.DryRun); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}
	}
	if !opts.SkipSDK {
		if err := ensureSDK(ctx, cfg, opts); err != nil {
			return fmt.Errorf("Maxine SDK Core setup failed: %w", err)
		}
	}
	if !opts.SkipMaxine {
		if err := ensureMaxine(ctx, cfg, opts); err != nil {
			return fmt.Errorf("Maxine setup failed: %w", err)
		}
	}
	if !opts.SkipLoopback {
		if err := ensureLoopbackConfig(ctx, cfg, opts); err != nil {
			return fmt.Errorf("loopback config setup failed: %w", err)
		}
		if !opts.SkipReload {
			if err := reloadLoopback(ctx, cfg, opts.DryRun); err != nil {
				return fmt.Errorf("loopback reload failed: %w", err)
			}
		}
	}
	if !opts.SkipMaxine {
		result := fx.Doctor(cfg)
		if opts.DryRun {
			fmt.Printf("would run fx doctor: %s\n", result.Message)
		} else {
			if !result.HelperOK {
				return fmt.Errorf("Maxine FX runtime check failed")
			}
			fmt.Printf("ok: fx doctor passed: %s\n", result.Message)
		}
	}
	if !opts.SkipService {
		if err := svc.Install(ctx, cfg, opts.Force, opts.DryRun, opts.Enable, opts.Start); err != nil {
			return fmt.Errorf("service setup failed: %w", err)
		}
		if opts.DryRun {
			fmt.Printf("would install user service: %s\n", cfg.Service.Name)
		} else {
			fmt.Printf("ok: user service installed: %s\n", cfg.Service.Name)
		}
	}
	fmt.Println("ok: setup complete")
	return nil
}

func needsSudo(opts Options) bool {
	if os.Geteuid() == 0 {
		return false
	}
	return !opts.SkipSDK || !opts.SkipMaxine || !opts.SkipLoopback
}

func validateSudo(ctx context.Context, dryRun bool) error {
	return run(ctx, dryRun, "", "sudo", "-v")
}

func ensureSDK(ctx context.Context, cfg config.Config, opts Options) error {
	sdkPath, err := config.ExpandPath(cfg.FX.SDKPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(sdkPath, "features", "install_feature.sh")); err == nil {
		fmt.Printf("ok: Maxine SDK Core exists: %s\n", sdkPath)
		return nil
	} else if err != nil && !os.IsNotExist(err) && !os.IsPermission(err) {
		return err
	}
	tarball, err := downloadSDK(ctx, opts)
	if err != nil {
		return err
	}
	fmt.Printf("extracting Maxine SDK Core to %s\n", filepath.Dir(sdkPath))
	if err := run(ctx, opts.DryRun, "", "sudo", "tar", "-xf", tarball, "-C", filepath.Dir(sdkPath)); err != nil {
		return err
	}
	if !opts.DryRun {
		fmt.Printf("ok: Maxine SDK Core extracted to %s\n", sdkPath)
	}
	return nil
}

func downloadSDK(ctx context.Context, opts Options) (string, error) {
	downloadDir, err := sdkDownloadDir()
	if err != nil {
		return "", err
	}
	if opts.DryRun {
		fmt.Printf("would run: ngc registry resource download-version %s in %s\n", defaultSDKNGCResource, downloadDir)
		return filepath.Join(downloadDir, "VFXSDK_linux_<version>.tgz"), nil
	}
	if _, err := exec.LookPath("ngc"); err != nil {
		return "", fmt.Errorf("ngc CLI not found; install it and run ngc config set")
	}
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return "", err
	}
	if cached, err := findSDKTarball(downloadDir); err != nil {
		return "", err
	} else if cached != "" {
		fmt.Printf("ok: using cached SDK tarball: %s\n", cached)
		return cached, nil
	}
	fmt.Printf("downloading Maxine SDK Core from NGC: %s\n", defaultSDKNGCResource)
	if err := runStreaming(ctx, downloadDir, "ngc", "registry", "resource", "download-version", defaultSDKNGCResource); err != nil {
		return "", fmt.Errorf("%w\nrun ngc config set if NGC authentication is not configured", err)
	}
	tarball, err := findSDKTarball(downloadDir)
	if err != nil {
		return "", err
	}
	if tarball == "" {
		return "", fmt.Errorf("NGC download completed but no VFXSDK_linux_*.tgz was found under %s", downloadDir)
	}
	fmt.Printf("ok: Maxine SDK Core downloaded: %s\n", tarball)
	return tarball, nil
}

func sdkDownloadDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "nv-vcam", "ngc"), nil
}

func findSDKTarball(root string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || found != "" {
			return nil
		}
		name := entry.Name()
		if strings.HasPrefix(name, "VFXSDK_linux_") && strings.HasSuffix(name, ".tgz") {
			found = path
		}
		return nil
	})
	return found, err
}

func ensureConfig(force, dryRun bool) error {
	path, err := config.DefaultPath()
	if err != nil {
		return err
	}
	rendered := config.Render(config.Default())
	if _, err := os.Stat(path); err == nil && !force {
		fmt.Printf("ok: config exists: %s\n", path)
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if dryRun {
		fmt.Printf("would write %s:\n%s", path, rendered)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
		return err
	}
	fmt.Printf("ok: wrote config: %s\n", path)
	return nil
}

func ensureMaxine(ctx context.Context, cfg config.Config, opts Options) error {
	before := fx.Doctor(cfg)
	if before.HelperOK {
		fmt.Println("ok: Maxine features already installed")
		return nil
	}
	if opts.DryRun {
		fmt.Printf("would install Maxine features: %s\n", opts.Features)
		return nil
	}
	sdkPath, err := config.ExpandPath(cfg.FX.SDKPath)
	if err != nil {
		return err
	}
	script := filepath.Join(sdkPath, "features", "install_feature.sh")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("%s not found; install/extract NVIDIA VFX SDK Core to %s first", script, sdkPath)
	}
	if os.Getenv("NGC_CLI_API_KEY") == "" {
		return fmt.Errorf("NGC_CLI_API_KEY is required; run: export NGC_CLI_API_KEY=<your_api_key>")
	}

	fmt.Printf("installing Maxine features: %s\n", opts.Features)
	args := []string{"-f", opts.Features, "--ngc-org", opts.NGCOrg, "--ngc-team", opts.NGCTeam}
	if opts.GPU != "" {
		args = append(args, "-g", opts.GPU)
	}
	if opts.Version != "" {
		args = append(args, "-v", opts.Version)
	}
	if err := runPrivileged(ctx, opts.DryRun, filepath.Dir(script), script, args...); err != nil {
		return err
	}
	after := fx.Doctor(cfg)
	if !after.HelperOK {
		return fmt.Errorf("feature installation completed but doctor still failed: %s", after.Message)
	}
	fmt.Printf("ok: Maxine features installed: %s\n", opts.Features)
	return nil
}

func ensureLoopbackConfig(ctx context.Context, cfg config.Config, opts Options) error {
	target := cfg.Loopback.ConfigPath
	found, err := loopback.FindConfigs(filepath.Dir(target), target)
	if err != nil {
		return err
	}
	var conflicts []string
	for _, item := range found {
		if !item.IsNV {
			conflicts = append(conflicts, item.Path)
		}
	}
	if len(conflicts) > 0 && !opts.Force {
		return fmt.Errorf("refusing to write because other v4l2loopback config files exist: %s\nrerun with --force if you intentionally want nv-vcam to coexist with them", strings.Join(conflicts, ", "))
	}
	if _, err := os.Stat(target); err == nil && !opts.Force {
		fmt.Printf("ok: loopback config exists: %s\n", target)
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if opts.DryRun {
		fmt.Printf("would write %s:\n%s", target, loopback.Render(cfg))
		return nil
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
	if err := run(ctx, false, "", "sudo", "install", "-m", "0644", temp.Name(), target); err != nil {
		return err
	}
	fmt.Printf("ok: wrote loopback config: %s\n", target)
	return nil
}

func reloadLoopback(ctx context.Context, cfg config.Config, dryRun bool) error {
	manager := svc.New(cfg.Service.Name)
	if err := manager.Stop(ctx, dryRun); err != nil {
		if isServiceNotLoaded(err) {
			fmt.Printf("ok: %s is not loaded; continuing with loopback reload\n", cfg.Service.Name)
		} else {
			fmt.Printf("warning: could not stop %s before reload: %v\n", cfg.Service.Name, err)
		}
	}
	if err := run(ctx, dryRun, "", "sudo", "modprobe", "-r", "v4l2loopback"); err != nil {
		return err
	}
	if err := run(ctx, dryRun, "", "sudo", "modprobe", "v4l2loopback"); err != nil {
		return err
	}
	if !dryRun {
		fmt.Println("ok: reloaded v4l2loopback")
	}
	return nil
}

func isServiceNotLoaded(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Unit nv-vcam.service not loaded") || strings.Contains(msg, "not loaded")
}

func runPrivileged(ctx context.Context, dryRun bool, dir, name string, args ...string) error {
	if os.Geteuid() == 0 {
		return run(ctx, dryRun, dir, name, args...)
	}
	full := append([]string{"--preserve-env=NGC_CLI_API_KEY", name}, args...)
	return run(ctx, dryRun, dir, "sudo", full...)
}

func run(ctx context.Context, dryRun bool, dir, name string, args ...string) error {
	if dryRun {
		fmt.Printf("would run: %s %s\n", name, strings.Join(args, " "))
		return nil
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s failed: %w\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(buf.String()))
	}
	return nil
}

func runStreaming(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
