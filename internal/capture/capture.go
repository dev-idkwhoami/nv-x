package capture

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"nv-vcam/internal/config"
)

type State string

const (
	StateDisabled State = "disabled"
	StateIdle     State = "idle"
	StateActive   State = "active"
	StateError    State = "error"
)

type Snapshot struct {
	State        State    `json:"state"`
	Device       string   `json:"device"`
	Dependencies []string `json:"dependencies"`
	Consumers    int      `json:"consumers"`
	Message      string   `json:"message"`
	UpdatedAt    string   `json:"updatedAt"`
}

type Supervisor struct {
	cfg      config.Config
	procRoot string
	logf     func(string, ...any)

	mu       sync.Mutex
	state    State
	message  string
	owned    map[int]bool
	current  *processGroup
	consumer int
	idleText bool
}

func NewSupervisor(cfg config.Config, logf func(string, ...any)) *Supervisor {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Supervisor{
		cfg:      cfg,
		procRoot: "/proc",
		logf:     logf,
		state:    StateDisabled,
		owned:    map[int]bool{},
		idleText: true,
	}
}

func (s *Supervisor) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{
		State:        s.state,
		Device:       s.cfg.Capture.Device,
		Dependencies: MissingDependencies(s.cfg),
		Consumers:    s.consumer,
		Message:      s.message,
		UpdatedAt:    time.Now().Format(time.RFC3339),
	}
}

func (s *Supervisor) Run(ctx context.Context) error {
	if !s.cfg.Capture.Enabled {
		s.setState(StateDisabled, "capture is disabled")
		<-ctx.Done()
		return nil
	}
	if missing := MissingDependencies(s.cfg); len(missing) > 0 {
		s.setState(StateError, "missing dependencies: "+strings.Join(missing, ", "))
		<-ctx.Done()
		return nil
	}

	s.logf("capture supervisor started for %s", s.cfg.Capture.Device)
	if err := s.startIdle(ctx); err != nil {
		s.setState(StateError, err.Error())
		s.logf("idle stream failed: %v", err)
		<-ctx.Done()
		return nil
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer s.stopCurrent(2 * time.Second)

	var noConsumerSince time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			consumers := CountExternalConsumers(s.procRoot, s.cfg.Capture.Device, s.ownedPIDs())
			s.setConsumerCount(consumers)
			switch s.stateValue() {
			case StateIdle:
				if s.currentExited() {
					s.logf("idle stream exited; restarting without text overlay")
					s.stopCurrent(time.Second)
					s.setIdleText(false)
					if err := s.startIdle(ctx); err != nil {
						s.setState(StateError, err.Error())
						s.logf("idle restart failed: %v", err)
					}
					continue
				}
				if consumers > 0 {
					s.logf("external consumer detected on %s; starting Sony RAW capture", s.cfg.Capture.Device)
					s.stopCurrent(2 * time.Second)
					if err := s.startCapture(ctx); err != nil {
						s.setState(StateError, err.Error())
						s.logf("capture start failed: %v", err)
					}
					noConsumerSince = time.Time{}
				}
			case StateActive:
				if consumers > 0 {
					noConsumerSince = time.Time{}
					if s.currentExited() {
						s.logf("capture process exited while consumer remains; restarting")
						s.stopCurrent(time.Second)
						if err := s.startCapture(ctx); err != nil {
							s.setState(StateError, err.Error())
							s.logf("capture restart failed: %v", err)
						}
					}
					continue
				}
				if noConsumerSince.IsZero() {
					noConsumerSince = time.Now()
					continue
				}
				timeout := time.Duration(s.cfg.Capture.IdleTimeoutSeconds) * time.Second
				if timeout <= 0 {
					timeout = 15 * time.Second
				}
				if time.Since(noConsumerSince) >= timeout {
					s.logf("no RAW consumers for %s; returning to idle stream", timeout)
					s.stopCurrent(2 * time.Second)
					if err := s.startIdle(ctx); err != nil {
						s.setState(StateError, err.Error())
						s.logf("idle restart failed: %v", err)
					}
					noConsumerSince = time.Time{}
				}
			case StateError:
				if consumers == 0 {
					s.stopCurrent(time.Second)
					if err := s.startIdle(ctx); err != nil {
						s.setState(StateError, err.Error())
					}
				}
			}
		}
	}
}

func MissingDependencies(cfg config.Config) []string {
	needed := []string{"ffmpeg"}
	if fields := strings.Fields(cfg.Capture.InputCommand); len(fields) > 0 {
		needed = append(needed, fields[0])
	}
	var missing []string
	seen := map[string]bool{}
	for _, name := range needed {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if _, err := exec.LookPath(name); err != nil {
			missing = append(missing, name)
		}
	}
	return missing
}

func DefaultStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "nv-vcam", "capture.json"), nil
}

func ReadState(path string) (Snapshot, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, false
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, false
	}
	return snap, true
}

func IdleFFmpegArgs(cfg config.CaptureConfig, withText bool) []string {
	// Keep the idle stream's negotiated V4L2 format identical to the real
	// capture stream. Some consumers keep using the first format they saw across
	// the idle->capture handoff, which can produce striped/corrupt frames if the
	// resolution or frame rate changes.
	width, height := cfg.Width, cfg.Height
	if width <= 0 {
		width = 2560
	}
	if height <= 0 {
		height = 1440
	}
	fps := cfg.FPS
	if fps <= 0 {
		fps = 25
	}
	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-re",
		"-f", "lavfi",
		"-i", fmt.Sprintf("color=c=black:s=%dx%d:r=%d", width, height, fps),
	}
	if withText && cfg.IdleLabel != "" {
		text := strings.ReplaceAll(cfg.IdleLabel, ":", `\:`)
		text = strings.ReplaceAll(text, "'", `\'`)
		args = append(args, "-vf", fmt.Sprintf("drawtext=text='%s':fontcolor=white:fontsize=48:x=(w-text_w)/2:y=(h-text_h)/2,format=yuv420p", text))
	} else {
		args = append(args, "-vf", "format=yuv420p")
	}
	args = append(args, "-pix_fmt", "yuv420p", "-f", "v4l2", cfg.Device)
	return args
}

func CaptureFFmpegArgs(cfg config.CaptureConfig) []string {
	fps := cfg.FPS
	if fps <= 0 {
		fps = 25
	}
	width, height := cfg.Width, cfg.Height
	if width <= 0 {
		width = 2560
	}
	if height <= 0 {
		height = 1440
	}
	filter := fmt.Sprintf("format=yuv420p,scale=%d:%d,format=yuv420p", width, height)
	if cfg.UseCUDAScale {
		filter = fmt.Sprintf("format=yuv420p,hwupload_cuda,scale_cuda=%d:%d:format=yuv420p,hwdownload,format=yuv420p", width, height)
	}
	return []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-probesize", "32",
		"-analyzeduration", "0",
		"-f", "mjpeg",
		"-r", strconv.Itoa(fps),
		"-i", "-",
		"-vf", filter,
		"-pix_fmt", "yuv420p",
		"-f", "v4l2",
		cfg.Device,
	}
}

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

func (s *Supervisor) startIdle(ctx context.Context) error {
	group, err := startProcessGroup(ctx, "idle", s.logf, []commandSpec{{Name: "ffmpeg", Args: IdleFFmpegArgs(s.cfg.Capture, s.idleTextValue())}})
	if err != nil {
		return err
	}
	s.setCurrent(group, StateIdle, "idle stream is writing to "+s.cfg.Capture.Device)
	return nil
}

func (s *Supervisor) startCapture(ctx context.Context) error {
	fields := strings.Fields(s.cfg.Capture.InputCommand)
	if len(fields) == 0 {
		return errors.New("capture input_command is empty")
	}
	specs := []commandSpec{
		{Name: fields[0], Args: fields[1:]},
		{Name: "ffmpeg", Args: CaptureFFmpegArgs(s.cfg.Capture)},
	}
	group, err := startProcessGroup(ctx, "capture", s.logf, specs)
	if err != nil {
		return err
	}
	s.setCurrent(group, StateActive, "Sony RAW capture is writing to "+s.cfg.Capture.Device)
	return nil
}

func (s *Supervisor) setCurrent(group *processGroup, state State, message string) {
	s.mu.Lock()
	s.current = group
	s.owned = group.PIDs()
	s.state = state
	s.message = message
	snap := s.snapshotLocked()
	s.mu.Unlock()
	s.writeState(snap)
}

func (s *Supervisor) stopCurrent(timeout time.Duration) {
	s.mu.Lock()
	group := s.current
	s.current = nil
	s.owned = map[int]bool{}
	s.mu.Unlock()
	if group != nil {
		group.Stop(timeout)
	}
}

func (s *Supervisor) currentExited() bool {
	s.mu.Lock()
	group := s.current
	s.mu.Unlock()
	return group != nil && group.Exited()
}

func (s *Supervisor) ownedPIDs() map[int]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int]bool, len(s.owned))
	for pid, ok := range s.owned {
		out[pid] = ok
	}
	return out
}

func (s *Supervisor) OwnedPIDs() map[int]bool {
	return s.ownedPIDs()
}

func (s *Supervisor) stateValue() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Supervisor) setState(state State, message string) {
	s.mu.Lock()
	s.state = state
	s.message = message
	snap := s.snapshotLocked()
	s.mu.Unlock()
	s.writeState(snap)
}

func (s *Supervisor) setConsumerCount(count int) {
	s.mu.Lock()
	s.consumer = count
	snap := s.snapshotLocked()
	s.mu.Unlock()
	s.writeState(snap)
}

func (s *Supervisor) idleTextValue() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idleText
}

func (s *Supervisor) setIdleText(value bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idleText = value
}

func (s *Supervisor) snapshotLocked() Snapshot {
	return Snapshot{
		State:        s.state,
		Device:       s.cfg.Capture.Device,
		Dependencies: MissingDependencies(s.cfg),
		Consumers:    s.consumer,
		Message:      s.message,
		UpdatedAt:    time.Now().Format(time.RFC3339),
	}
}

func (s *Supervisor) writeState(snap Snapshot) {
	path, err := DefaultStatePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

type commandSpec struct {
	Name string
	Args []string
}

type processGroup struct {
	name string
	cmds []*exec.Cmd
	done chan struct{}
	once sync.Once
}

func startProcessGroup(ctx context.Context, name string, logf func(string, ...any), specs []commandSpec) (*processGroup, error) {
	group := &processGroup{name: name, done: make(chan struct{})}
	var previous io.ReadCloser
	for i, spec := range specs {
		cmd := exec.CommandContext(ctx, spec.Name, spec.Args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return nil, err
		}
		go scanOutput(name, spec.Name, stderr, logf)
		if previous != nil {
			cmd.Stdin = previous
		}
		if i < len(specs)-1 {
			out, err := cmd.StdoutPipe()
			if err != nil {
				return nil, err
			}
			previous = out
		}
		group.cmds = append(group.cmds, cmd)
	}
	for _, cmd := range group.cmds {
		if err := cmd.Start(); err != nil {
			stopStarted(group.cmds, time.Second)
			return nil, fmt.Errorf("start %s failed: %w", cmd.Path, err)
		}
		logf("%s started pid=%d: %s %s", name, cmd.Process.Pid, filepath.Base(cmd.Path), strings.Join(cmd.Args[1:], " "))
	}
	go func() {
		for _, cmd := range group.cmds {
			if err := cmd.Wait(); err != nil {
				logf("%s exited pid=%d: %v", name, processPID(cmd), err)
			}
		}
		close(group.done)
	}()
	return group, nil
}

func stopStarted(cmds []*exec.Cmd, timeout time.Duration) {
	deadline := time.After(timeout)
	for _, cmd := range cmds {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
	}
	for _, cmd := range cmds {
		if cmd.Process == nil {
			continue
		}
		done := make(chan struct{})
		go func(c *exec.Cmd) {
			_ = c.Wait()
			close(done)
		}(cmd)
		select {
		case <-done:
		case <-deadline:
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}
}

func scanOutput(group, name string, reader io.Reader, logf func(string, ...any)) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			logf("%s/%s: %s", group, name, line)
		}
	}
}

func (g *processGroup) PIDs() map[int]bool {
	out := map[int]bool{}
	for _, cmd := range g.cmds {
		if pid := processPID(cmd); pid > 0 {
			out[pid] = true
		}
	}
	return out
}

func (g *processGroup) Exited() bool {
	select {
	case <-g.done:
		return true
	default:
		return false
	}
}

func (g *processGroup) Stop(timeout time.Duration) {
	g.once.Do(func() {
		for _, cmd := range g.cmds {
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			}
		}
		select {
		case <-g.done:
			return
		case <-time.After(timeout):
		}
		for _, cmd := range g.cmds {
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}
		<-g.done
	})
}

func processPID(cmd *exec.Cmd) int {
	if cmd == nil || cmd.Process == nil {
		return 0
	}
	return cmd.Process.Pid
}
