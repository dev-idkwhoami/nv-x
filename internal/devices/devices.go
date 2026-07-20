package devices

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

type Device struct {
	SysName    string
	Path       string
	StablePath string
	Name       string
	Capture    bool
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
		path := filepath.Join(devRoot, name)
		capture, err := isVideoCapture(path)
		if err != nil {
			// Keep devices visible when permissions or synthetic test roots prevent
			// capability probing; real metadata nodes are filtered by Capture in the UI.
			capture = true
		}
		out = append(out, Device{
			SysName:    name,
			Path:       path,
			StablePath: stableVideoPath(devRoot, path),
			Name:       strings.TrimSpace(string(label)),
			Capture:    capture,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SysName < out[j].SysName
	})
	return out, nil
}

const (
	vidiocQueryCap            = 0x80685600
	v4l2CapVideoCapture       = 0x00000001
	v4l2CapVideoCaptureMPlane = 0x00001000
	v4l2CapDeviceCaps         = 0x80000000
)

type v4l2Capability struct {
	Driver       [16]byte
	Card         [32]byte
	BusInfo      [32]byte
	Version      uint32
	Capabilities uint32
	DeviceCaps   uint32
	Reserved     [3]uint32
}

func isVideoCapture(path string) (bool, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return false, err
	}
	defer unix.Close(fd)
	var capability v4l2Capability
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), vidiocQueryCap, uintptr(unsafe.Pointer(&capability)))
	if errno != 0 {
		return false, errno
	}
	caps := capability.Capabilities
	if caps&v4l2CapDeviceCaps != 0 {
		caps = capability.DeviceCaps
	}
	return caps&(v4l2CapVideoCapture|v4l2CapVideoCaptureMPlane) != 0, nil
}

func stableVideoPath(devRoot, target string) string {
	entries, err := os.ReadDir(filepath.Join(devRoot, "v4l", "by-id"))
	if err != nil {
		return target
	}
	targetReal, err := filepath.EvalSymlinks(target)
	if err != nil {
		return target
	}
	for _, entry := range entries {
		candidate := filepath.Join(devRoot, "v4l", "by-id", entry.Name())
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil && resolved == targetReal {
			return candidate
		}
	}
	return target
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
