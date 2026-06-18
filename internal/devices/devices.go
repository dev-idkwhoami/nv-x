package devices

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Device struct {
	SysName string
	Path    string
	Name    string
}

func ListDefault() ([]Device, error) {
	return List("/sys/class/video4linux", "/dev")
}

func List(sysRoot, devRoot string) ([]Device, error) {
	entries, err := os.ReadDir(sysRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Device
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "video") {
			continue
		}
		label, _ := os.ReadFile(filepath.Join(sysRoot, name, "name"))
		out = append(out, Device{
			SysName: name,
			Path:    filepath.Join(devRoot, name),
			Name:    strings.TrimSpace(string(label)),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SysName < out[j].SysName
	})
	return out, nil
}

func DeviceExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "video") {
		return false
	}
	_, err = os.Stat(filepath.Join("/sys/class/video4linux", base))
	return err == nil
}

func V4L2CtlList(ctx context.Context) (string, error) {
	path, err := exec.LookPath("v4l2-ctl")
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, "--list-devices")
	data, err := cmd.CombinedOutput()
	if err != nil {
		return string(data), fmt.Errorf("v4l2-ctl --list-devices failed: %w", err)
	}
	return string(data), nil
}
