package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"nv-vcam/internal/config"
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

func RenderUnit() string {
	return `[Unit]
Description=nv-vcam RAW virtual camera service
After=default.target

[Service]
Type=simple
ExecStart=%h/.local/bin/nv-vcam run
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
	if dryRun {
		fmt.Printf("would write %s:\n%s", path, RenderUnit())
	} else {
		if _, err := os.Stat(path); err == nil && !force {
			return fmt.Errorf("%s already exists; use --force to overwrite", path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(RenderUnit()), 0o644); err != nil {
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
