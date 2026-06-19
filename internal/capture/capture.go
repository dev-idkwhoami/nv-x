package capture

import (
	"os"
	"path/filepath"
	"strconv"
)

func CountExternalConsumers(procRoot, device string, owned map[int]bool) int {
	count := 0
	_ = walkDeviceConsumers(procRoot, device, func(pid int) {
		if !owned[pid] {
			count++
		}
	})
	return count
}

func walkDeviceConsumers(procRoot, device string, fn func(int)) error {
	deviceReal, _ := filepath.EvalSymlinks(device)
	if deviceReal == "" {
		deviceReal = device
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		fdDir := filepath.Join(procRoot, entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		seen := false
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			targetReal, _ := filepath.EvalSymlinks(target)
			if target == device || targetReal == deviceReal {
				seen = true
				break
			}
		}
		if seen {
			fn(pid)
		}
	}
	return nil
}
