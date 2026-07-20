package setup

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"nv-x/internal/audio"
	"nv-x/internal/config"
	"nv-x/internal/fx"
	"nv-x/internal/loopback"
	svc "nv-x/internal/service"
)

const defaultSDKNGCResource = "nvidia/maxine/vfx_sdk_core:1.2.0.0_linux"
const defaultAFXNGCResource = "nvidia/maxine/maxine_linux_audio_effects_sdk:2.1.0"

type Options struct {
	Force        bool
	DryRun       bool
	SkipConfig   bool
	SkipSDK      bool
	SkipMaxine   bool
	SkipLoopback bool
	SkipReload   bool
	SkipService  bool
	SkipVideo    bool
	SkipAudio    bool
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
		return fmt.Errorf("do not run nv-x setup with sudo; run it as your normal desktop user so the systemd user service is installed for the right account")
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
	if err := cleanupLegacy(ctx, opts.DryRun); err != nil {
		return fmt.Errorf("legacy nv-vcam cleanup failed: %w", err)
	}
	if !opts.SkipVideo && !opts.SkipSDK {
		if err := ensureSDK(ctx, cfg, opts); err != nil {
			return fmt.Errorf("Maxine SDK Core setup failed: %w", err)
		}
	}
	if !opts.SkipVideo && !opts.SkipMaxine {
		if err := ensureMaxine(ctx, cfg, opts); err != nil {
			return fmt.Errorf("Maxine setup failed: %w", err)
		}
	}
	if !opts.SkipAudio {
		if err := ensureAFXSDK(ctx, cfg, opts); err != nil {
			return fmt.Errorf("Maxine Audio Effects SDK setup failed: %w", err)
		}
		if err := ensureAFXFeatures(ctx, cfg, opts); err != nil {
			return fmt.Errorf("Maxine audio feature setup failed: %w", err)
		}
	}
	if !opts.SkipVideo && !opts.SkipLoopback {
		if err := ensureLoopbackConfig(ctx, cfg, opts); err != nil {
			return fmt.Errorf("loopback config setup failed: %w", err)
		}
		if !opts.SkipReload {
			if err := reloadLoopback(ctx, cfg, opts.DryRun); err != nil {
				return fmt.Errorf("loopback reload failed: %w", err)
			}
		}
	}
	if !opts.SkipVideo && !opts.SkipMaxine {
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
	if !opts.SkipAudio {
		if opts.DryRun {
			fmt.Println("would run audio doctor for dereverb/denoiser and Studio Voice Low Latency")
		} else {
			result := audio.Doctor(cfg)
			if !result.HelperOK {
				return fmt.Errorf("Maxine AFX runtime check failed: %s", result.Message)
			}
			fmt.Printf("ok: audio doctor passed: %s\n", result.Message)
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
	return (!opts.SkipVideo && (!opts.SkipSDK || !opts.SkipMaxine)) || !opts.SkipAudio || !opts.SkipLoopback
}

func cleanupLegacy(ctx context.Context, dryRun bool) error {
	legacy := svc.New("nv-vcam.service")
	_ = legacy.Stop(ctx, dryRun)
	if dryRun {
		fmt.Println("would disable and remove legacy nv-vcam installed artifacts (user config/state preserved)")
	} else {
		_ = legacy.Disable(ctx, false)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	paths := []string{
		filepath.Join(home, ".config/systemd/user/nv-vcam.service"),
		filepath.Join(home, ".local/bin/nv-vcam"),
		filepath.Join(home, ".local/bin/nv-vcam-gui"),
		filepath.Join(home, ".local/bin/nv-vcam-maxine-helper"),
		filepath.Join(home, ".local/lib/nv-vcam/nv-vcam-os-release-shim.so"),
		filepath.Join(home, ".local/share/applications/nv-vcam-gui.desktop"),
		filepath.Join(home, ".local/share/icons/hicolor/256x256/apps/nv-vcam-gui.png"),
	}
	for _, path := range paths {
		if dryRun {
			fmt.Printf("would remove %s\n", path)
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := run(ctx, dryRun, "", "sudo", "rm", "-f", "/etc/modprobe.d/nv-vcam-v4l2loopback.conf"); err != nil {
		return err
	}
	return legacy.DaemonReload(ctx, dryRun)
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
	return filepath.Join(cache, "nv-x", "ngc"), nil
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

func ensureAFXSDK(ctx context.Context, cfg config.Config, opts Options) error {
	root, err := config.ExpandPath(cfg.Audio.SDKPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "nvafx", "include", "nvAudioEffects.h")); err == nil {
		fmt.Printf("ok: Maxine Audio Effects SDK exists: %s\n", root)
		return nil
	}
	tarball, err := downloadAFXSDK(ctx, opts)
	if err != nil {
		return err
	}
	if err := run(ctx, opts.DryRun, "", "sudo", "mkdir", "-p", root); err != nil {
		return err
	}
	if err := run(ctx, opts.DryRun, "", "sudo", "tar", "-xzf", tarball, "-C", root, "--strip-components=2"); err != nil {
		return err
	}
	if !opts.DryRun {
		fmt.Printf("ok: Maxine Audio Effects SDK extracted to %s\n", root)
	}
	return nil
}

func downloadAFXSDK(ctx context.Context, opts Options) (string, error) {
	dir, err := sdkDownloadDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "afx")
	if opts.DryRun {
		fmt.Printf("would run: ngc registry resource download-version %s in %s\n", defaultAFXNGCResource, dir)
		return filepath.Join(dir, "NVIDIA_AFX_SDK_Linux_<version>.tar.gz"), nil
	}
	if cached, _ := findAFXTarball(dir); cached != "" {
		return cached, nil
	}
	if _, err := exec.LookPath("ngc"); err != nil {
		return "", fmt.Errorf("ngc CLI not found; install it and run ngc config set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := runStreaming(ctx, dir, "ngc", "registry", "resource", "download-version", defaultAFXNGCResource); err != nil {
		return "", err
	}
	result, err := findAFXTarball(dir)
	if err != nil {
		return "", err
	}
	if result == "" {
		return "", fmt.Errorf("NGC download completed but no NVIDIA_AFX_SDK_Linux_*.tar.gz was found under %s", dir)
	}
	return result, nil
}

func findAFXTarball(root string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "NVIDIA_AFX_SDK_Linux_") && strings.HasSuffix(entry.Name(), ".tar.gz") {
			found = path
		}
		return nil
	})
	if os.IsNotExist(err) {
		return "", nil
	}
	return found, err
}

type afxFeature struct {
	effect       string
	directory    string
	modelPattern string
}

func ensureAFXFeatures(ctx context.Context, cfg config.Config, opts Options) error {
	root, _ := config.ExpandPath(cfg.Audio.SDKPath)
	features := []afxFeature{
		{
			effect:       "dereverb_denoiser-48k",
			directory:    "dereverb_denoiser",
			modelPattern: "models/sm_*/dereverb_denoiser_48k_*.trtpkg",
		},
		{
			effect:       "studio_voice-48k",
			directory:    "studio_voice",
			modelPattern: "models/sm_*/studio_voice_low_latency_48k_*.trtpkg",
		},
	}
	missing := make([]afxFeature, 0, len(features))
	for _, feature := range features {
		matches, _ := filepath.Glob(filepath.Join(root, "features", feature.directory, feature.modelPattern))
		if len(matches) != 1 {
			missing = append(missing, feature)
		}
	}
	if len(missing) == 0 {
		fmt.Println("ok: Maxine audio features already installed")
		return nil
	}
	script := filepath.Join(root, "features", "download_features.sh")
	if _, err := os.Stat(script); err != nil && !opts.DryRun {
		return fmt.Errorf("%s not found", script)
	}
	key, err := ngcAPIKey()
	if err != nil && !opts.DryRun {
		return err
	}
	if opts.DryRun {
		for _, feature := range missing {
			fmt.Printf("would install AFX feature: %s with auto-detected GPU architecture\n", feature.effect)
		}
		return nil
	}
	old, existed := os.LookupEnv("NGC_API_KEY")
	if err := os.Setenv("NGC_API_KEY", key); err != nil {
		return err
	}
	defer func() {
		if existed {
			_ = os.Setenv("NGC_API_KEY", old)
		} else {
			_ = os.Unsetenv("NGC_API_KEY")
		}
	}()
	patchedScript, err := createPatchedAFXDownloader(script)
	if err != nil {
		return err
	}
	defer os.Remove(patchedScript)

	// NVIDIA's downloader uses shared, fixed /tmp files and does not reliably
	// fail when a request does. Download each effect into an isolated staging
	// directory using unique manifests, verify it, and only then install it.
	for _, feature := range missing {
		staging, err := os.MkdirTemp("", "nv-x-afx-feature-*")
		if err != nil {
			return err
		}
		args := []string{"--ngc-org", opts.NGCOrg, "--ngc-team", opts.NGCTeam, "--effects", feature.effect, "--output-dir", staging}
		if err := runStreaming(ctx, filepath.Dir(script), patchedScript, args...); err != nil {
			os.RemoveAll(staging)
			return fmt.Errorf("download %s: %w", feature.effect, err)
		}
		source := filepath.Join(staging, feature.directory)
		matches, _ := filepath.Glob(filepath.Join(source, feature.modelPattern))
		if len(matches) != 1 {
			os.RemoveAll(staging)
			return fmt.Errorf("downloader completed but did not install the expected %s model", feature.effect)
		}
		target := filepath.Join(root, "features", feature.directory)
		if err := run(ctx, false, "", "sudo", "rm", "-rf", target); err != nil {
			os.RemoveAll(staging)
			return err
		}
		if err := run(ctx, false, "", "sudo", "cp", "-a", source, target); err != nil {
			os.RemoveAll(staging)
			return err
		}
		if err := os.RemoveAll(staging); err != nil {
			return err
		}
		fmt.Printf("ok: Maxine audio feature installed: %s\n", feature.effect)
	}
	return nil
}

func createPatchedAFXDownloader(script string) (string, error) {
	content, err := os.ReadFile(script)
	if err != nil {
		return "", err
	}
	patched, err := patchAFXDownloader(string(content))
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp("", "nv-x-download-features-*.sh")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if _, err := file.WriteString(patched); err != nil {
		file.Close()
		os.Remove(name)
		return "", err
	}
	if err := file.Chmod(0o700); err != nil {
		file.Close()
		os.Remove(name)
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func patchAFXDownloader(content string) (string, error) {
	const shebang = "#!/bin/bash\n"
	if !strings.HasPrefix(content, shebang) {
		return "", fmt.Errorf("unsupported NVIDIA AFX downloader: missing bash shebang")
	}
	patched := strings.Replace(content, shebang, shebang+"set -e\n", 1)
	replacements := [][2]string{
		{"local TEMP_FILE=/tmp/temp_list", "local TEMP_FILE=$(mktemp /tmp/nv-x-afx-list.XXXXXX)"},
		{"local TEMP_TAR_FILE=/tmp/temp_lib.tar.gz", "local TEMP_TAR_FILE=$(mktemp /tmp/nv-x-afx-lib.XXXXXX.tar.gz)"},
	}
	for _, replacement := range replacements {
		if !strings.Contains(patched, replacement[0]) {
			return "", fmt.Errorf("unsupported NVIDIA AFX downloader: expected temporary-file declaration not found")
		}
		patched = strings.Replace(patched, replacement[0], replacement[1], 1)
	}
	return patched, nil
}

func ngcAPIKey() (string, error) {
	for _, name := range []string{"NGC_API_KEY", "NGC_CLI_API_KEY"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	file, err := os.Open(filepath.Join(home, ".ngc", "config"))
	if err == nil {
		defer file.Close()
		if key, err := readNGCAPIKey(file); err != nil {
			return "", err
		} else if key != "" {
			return key, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read NGC config: %w", err)
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("NGC API key not found in the environment or ~/.ngc/config, and setup is not running in an interactive terminal; run ngc config set or export NGC_API_KEY")
	}
	fmt.Fprint(os.Stderr, "NGC API key not found in the environment or ~/.ngc/config.\nEnter NGC API key: ")
	value, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read NGC API key: %w", err)
	}
	key := strings.TrimSpace(string(value))
	if key == "" {
		return "", fmt.Errorf("NGC API key cannot be empty")
	}
	return key, nil
}

func readNGCAPIKey(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "apikey") && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), nil
		}
	}
	return "", scanner.Err()
}

func runPrivilegedAudio(ctx context.Context, dir, name string, args ...string) error {
	if os.Geteuid() == 0 {
		return run(ctx, false, dir, name, args...)
	}
	full := append([]string{"--preserve-env=NGC_API_KEY", name}, args...)
	return run(ctx, false, dir, "sudo", full...)
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
	key, err := ngcAPIKey()
	if err != nil {
		return err
	}
	old, existed := os.LookupEnv("NGC_CLI_API_KEY")
	if err := os.Setenv("NGC_CLI_API_KEY", key); err != nil {
		return err
	}
	defer func() {
		if existed {
			_ = os.Setenv("NGC_CLI_API_KEY", old)
		} else {
			_ = os.Unsetenv("NGC_CLI_API_KEY")
		}
	}()

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
		return fmt.Errorf("refusing to write because other v4l2loopback config files exist: %s\nrerun with --force if you intentionally want nv-x to coexist with them", strings.Join(conflicts, ", "))
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
	return strings.Contains(msg, "Unit nv-x.service not loaded") || strings.Contains(msg, "not loaded")
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
