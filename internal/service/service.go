package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"nv-x/internal/config"
)

type Manager struct {
	Name string
}

func New(name string) Manager {
	return Manager{Name: name}
}

func UserServicePath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", name), nil
}

func RenderUnit(execPath string) string {
	return `[Unit]
Description=NV-X NVIDIA video and audio effects service
After=default.target pipewire.service wireplumber.service

[Service]
Type=simple
ExecStart=` + strconv.Quote(execPath) + ` run
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
`
}

func Install(ctx context.Context, cfg config.Config, force, dryRun, enable, start bool) error {
	path, err := UserServicePath(cfg.Service.Name)
	if err != nil {
		return err
	}
	execPath, err := resolveExecPath(cfg.Service.ExecPath)
	if err != nil {
		return err
	}
	unit := RenderUnit(execPath)
	if dryRun {
		fmt.Printf("would write %s:\n%s", path, unit)
	} else {
		if _, err := os.Stat(path); err == nil && !force {
			return fmt.Errorf("%s already exists; use --force to overwrite", path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
			return err
		}
	}
	m := New(cfg.Service.Name)
	if err := m.DaemonReload(ctx, dryRun); err != nil {
		return err
	}
	if enable {
		if err := m.Enable(ctx, dryRun); err != nil {
			return err
		}
	}
	if start {
		if err := m.Start(ctx, dryRun); err != nil {
			return err
		}
	}
	return nil
}

func resolveExecPath(configured string) (string, error) {
	if configured != "" {
		expanded, err := config.ExpandPath(configured)
		if err != nil {
			return "", err
		}
		if filepath.IsAbs(expanded) && isExecutable(expanded) {
			return expanded, nil
		}
		if !strings.ContainsRune(expanded, filepath.Separator) {
			if found, err := exec.LookPath(expanded); err == nil {
				return found, nil
			}
		}
	}
	current, err := os.Executable()
	if err == nil {
		current, err = filepath.Abs(current)
	}
	if err == nil && isExecutable(current) {
		return current, nil
	}
	return "", fmt.Errorf("nv-x executable not found at configured path %q or current executable", configured)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func (m Manager) Exists() bool {
	path, err := UserServicePath(m.Name)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func (m Manager) Active(ctx context.Context) (bool, string, error) {
	out, err := systemctl(ctx, false, "is-active", m.Name)
	if err != nil {
		return false, out, err
	}
	return strings.TrimSpace(out) == "active", out, nil
}

func (m Manager) DaemonReload(ctx context.Context, dryRun bool) error {
	_, err := systemctl(ctx, dryRun, "daemon-reload")
	return err
}

func (m Manager) Enable(ctx context.Context, dryRun bool) error {
	_, err := systemctl(ctx, dryRun, "enable", m.Name)
	return err
}

func (m Manager) Disable(ctx context.Context, dryRun bool) error {
	_, err := systemctl(ctx, dryRun, "disable", m.Name)
	return err
}

func (m Manager) Start(ctx context.Context, dryRun bool) error {
	_, err := systemctl(ctx, dryRun, "start", m.Name)
	return err
}

func (m Manager) Stop(ctx context.Context, dryRun bool) error {
	_, err := systemctl(ctx, dryRun, "stop", m.Name)
	return err
}

func (m Manager) Restart(ctx context.Context, dryRun bool) error {
	_, err := systemctl(ctx, dryRun, "restart", m.Name)
	return err
}

func (m Manager) Status(ctx context.Context) (string, error) {
	return systemctl(ctx, false, "status", m.Name)
}

func systemctl(ctx context.Context, dryRun bool, args ...string) (string, error) {
	full := append([]string{"--user"}, args...)
	if dryRun {
		fmt.Printf("would run: systemctl %s\n", strings.Join(full, " "))
		return "", nil
	}
	cmd := exec.CommandContext(ctx, "systemctl", full...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	if err != nil {
		return out, fmt.Errorf("systemctl %s failed: %w\n%s", strings.Join(full, " "), err, strings.TrimSpace(out))
	}
	return out, nil
}
