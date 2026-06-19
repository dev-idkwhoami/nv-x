package loopback

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nv-vcam/internal/config"
	"nv-vcam/internal/devices"
	"nv-vcam/internal/service"
)

type FoundConfig struct {
	Path    string
	Content string
	IsNV    bool
}

func Render(cfg config.Config) string {
	exclusive := "0"
	if cfg.Loopback.ExclusiveCaps {
		exclusive = "1"
	}
	return fmt.Sprintf("# Managed by nv-vcam\noptions v4l2loopback devices=1 video_nr=%d card_label=%q exclusive_caps=%s max_buffers=%d\n",
		cfg.Output.VideoNR,
		cfg.Output.Label,
		exclusive,
		cfg.Loopback.MaxBuffers,
	)
}

func FindConfigs(dir, nvPath string) ([]FoundConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var found []FoundConfig
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".disabled") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "v4l2loopback") {
			found = append(found, FoundConfig{
				Path:    path,
				Content: string(data),
				IsNV:    path == nvPath,
			})
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Path < found[j].Path })
	return found, nil
}

func ModuleLoaded() bool {
	data, err := os.ReadFile("/proc/modules")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "v4l2loopback ") {
			return true
		}
	}
	return false
}

func WriteConfig(cfg config.Config, force, dryRun bool, argv0 string) error {
	target := cfg.Loopback.ConfigPath
	found, err := FindConfigs(filepath.Dir(target), target)
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
		return fmt.Errorf("refusing to write because other v4l2loopback config files exist: %s\nrerun with --force if you intentionally want nv-vcam to coexist with them", strings.Join(conflicts, ", "))
	}
	if dryRun {
		fmt.Printf("would write %s:\n%s", target, Render(cfg))
		return nil
	}
	if os.Geteuid() != 0 {
		cmd := sudoCommand(argv0, "loopback", "write")
		if force {
			cmd += " --force"
		}
		return fmt.Errorf("root is required to write %s\nrun: %s", target, cmd)
	}
	if _, err := os.Stat(target); err == nil && !force {
		return fmt.Errorf("%s already exists; use --force to overwrite", target)
	}
	return os.WriteFile(target, []byte(Render(cfg)), 0o644)
}

func Reload(ctx context.Context, cfg config.Config, dryRun bool) error {
	svc := service.New(cfg.Service.Name)
	if err := svc.Stop(ctx, dryRun); err != nil {
		fmt.Printf("warning: could not stop %s before reload: %v\n", cfg.Service.Name, err)
	}

	if os.Geteuid() != 0 {
		fmt.Println("root is required to reload v4l2loopback")
		fmt.Println("run:")
		fmt.Printf("  sudo modprobe -r v4l2loopback\n")
		fmt.Printf("  sudo modprobe v4l2loopback\n")
		return nil
	}
	if err := run(ctx, dryRun, "modprobe", "-r", "v4l2loopback"); err != nil {
		if isBusy(err) {
			return fmt.Errorf("%w\nv4l2loopback appears busy; try: fuser -v %s", err, cfg.Output.Device)
		}
		return err
	}
	if err := run(ctx, dryRun, "modprobe", "v4l2loopback"); err != nil {
		return err
	}
	time.Sleep(250 * time.Millisecond)
	list, err := devices.ListDefault()
	if err != nil {
		return err
	}
	for _, dev := range list {
		fmt.Printf("%s %s\n", dev.Path, dev.Name)
	}
	return nil
}

func run(ctx context.Context, dryRun bool, name string, args ...string) error {
	if dryRun {
		fmt.Printf("would run: %s %s\n", name, strings.Join(args, " "))
		return nil
	}
	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s failed: %w\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(buf.String()))
	}
	if out := strings.TrimSpace(buf.String()); out != "" {
		fmt.Println(out)
	}
	return nil
}

func sudoCommand(argv0 string, args ...string) string {
	name := argv0
	if name == "" {
		name = "nv-vcam"
	}
	return "sudo " + name + " " + strings.Join(args, " ")
}

func isBusy(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "busy") || strings.Contains(msg, "in use")
}
