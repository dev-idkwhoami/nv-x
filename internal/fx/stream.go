package fx

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

	"nv-vcam/internal/capture"
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

type StreamOptions struct {
	InputDevice     string
	InputFormat     string
	OutputDevice    string
	OutputFormat    string
	Width           int
	Height          int
	FPS             int
	BackgroundMode  string
	BackgroundImage string
	ChromaColor     string
	BlurStrength    float64
	DenoiseEnabled  bool
	DenoiseStrength int
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
	}
}

func (s *Supervisor) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{
		State:        s.state,
		Device:       fxOutputDevice(s.cfg),
		Dependencies: MissingDependencies(s.cfg),
		Consumers:    s.consumer,
		Message:      s.message,
		UpdatedAt:    time.Now().Format(time.RFC3339),
	}
}

func (s *Supervisor) Run(ctx context.Context) error {
	if !s.cfg.FX.Enabled {
		s.setState(StateDisabled, "fx is disabled")
		<-ctx.Done()
		return nil
	}
	if missing := MissingDependencies(s.cfg); len(missing) > 0 {
		s.setState(StateError, "missing dependencies: "+strings.Join(missing, ", "))
		<-ctx.Done()
		return nil
	}

	s.logf("fx supervisor started for %s", fxOutputDevice(s.cfg))
	if err := s.startIdle(ctx); err != nil {
		s.setState(StateError, err.Error())
		s.logf("fx idle stream failed: %v", err)
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
			consumers := capture.CountExternalConsumers(s.procRoot, fxOutputDevice(s.cfg), s.ownedPIDs())
			s.setConsumerCount(consumers)
			switch s.stateValue() {
			case StateIdle:
				if s.currentExited() {
					s.logf("fx idle stream exited; restarting")
					s.stopCurrent(time.Second)
					if err := s.startIdle(ctx); err != nil {
						s.setState(StateError, err.Error())
						s.logf("fx idle restart failed: %v", err)
					}
					continue
				}
				if consumers > 0 {
					s.logf("external consumer detected on %s; starting Maxine FX", fxOutputDevice(s.cfg))
					s.stopCurrent(2 * time.Second)
					if err := s.startFX(ctx); err != nil {
						s.setState(StateError, err.Error())
						s.logf("fx start failed: %v", err)
					}
					noConsumerSince = time.Time{}
				}
			case StateActive:
				if consumers > 0 {
					noConsumerSince = time.Time{}
					if s.currentExited() {
						s.logf("fx pipeline exited while consumer remains; restarting")
						s.stopCurrent(time.Second)
						if err := s.startFX(ctx); err != nil {
							s.setState(StateError, err.Error())
							s.logf("fx restart failed: %v", err)
						}
					}
					continue
				}
				if noConsumerSince.IsZero() {
					noConsumerSince = time.Now()
					continue
				}
				timeout := 2 * time.Second
				if time.Since(noConsumerSince) >= timeout {
					s.logf("no FX consumers for %s; stopping native stream", timeout)
					s.stopCurrent(2 * time.Second)
					if err := s.startIdle(ctx); err != nil {
						s.setState(StateError, err.Error())
						s.logf("fx idle restart failed: %v", err)
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
	var missing []string
	_, result := maxineEnv(cfg)
	if result.HelperPath == "" {
		missing = append(missing, "nv-vcam-maxine-helper")
	}
	if result.OSReleaseShim && result.ShimPath == "" {
		missing = append(missing, "nv-vcam-os-release-shim.so")
	}
	return missing
}

func DefaultStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "nv-vcam", "fx.json"), nil
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

func RunStream(ctx context.Context, cfg config.Config, opts StreamOptions, logf func(string, ...any)) error {
	opts = normalizeStreamOptions(cfg, opts)
	if err := validateStreamOptions(opts); err != nil {
		return err
	}
	if missing := MissingDependencies(cfg); len(missing) > 0 {
		return fmt.Errorf("missing dependencies: %s", strings.Join(missing, ", "))
	}
	group, err := startFXPipeline(ctx, cfg, opts, logf)
	if err != nil {
		return err
	}
	defer group.Stop(2 * time.Second)
	select {
	case <-ctx.Done():
		return nil
	case <-group.done:
		return errors.New("fx stream pipeline exited")
	}
}

func RunTransfer(ctx context.Context, cfg config.Config, opts StreamOptions, logf func(string, ...any)) error {
	opts = normalizeStreamOptions(cfg, opts)
	if err := validateTransferOptions(opts); err != nil {
		return err
	}
	if missing := MissingDependencies(cfg); len(missing) > 0 {
		return fmt.Errorf("missing dependencies: %s", strings.Join(missing, ", "))
	}
	env, result := maxineEnv(cfg)
	if result.HelperPath == "" {
		return errors.New("Maxine helper binary not found; run make build")
	}
	group, err := startProcessGroup(ctx, "fx-transfer", nil, logf, []commandSpec{
		{Name: result.HelperPath, Args: NativeTransferHelperArgs(result, opts), Env: env},
	})
	if err != nil {
		return err
	}
	defer group.Stop(2 * time.Second)
	select {
	case <-ctx.Done():
		return nil
	case <-group.done:
		return errors.New("fx transfer pipeline exited")
	}
}

func normalizeStreamOptions(cfg config.Config, opts StreamOptions) StreamOptions {
	if opts.InputDevice == "" {
		opts.InputDevice = fxInputDevice(cfg)
	}
	if opts.InputFormat == "" {
		opts.InputFormat = cfg.Camera.InputFormat
	}
	if opts.OutputDevice == "" {
		opts.OutputDevice = fxOutputDevice(cfg)
	}
	if opts.OutputFormat == "" {
		opts.OutputFormat = cfg.Output.OutputFormat
	}
	if opts.Width <= 0 {
		opts.Width = cfg.Camera.Width
	}
	if opts.Height <= 0 {
		opts.Height = cfg.Camera.Height
	}
	if opts.FPS <= 0 {
		opts.FPS = cfg.Camera.FPS
	}
	if opts.BackgroundMode == "" {
		opts.BackgroundMode = cfg.FX.Mode
	}
	if opts.BackgroundImage == "" {
		opts.BackgroundImage = cfg.FX.BackgroundImage
	}
	if opts.ChromaColor == "" {
		opts.ChromaColor = cfg.FX.ChromaColor
	}
	if opts.BlurStrength <= 0 {
		opts.BlurStrength = cfg.FX.BlurStrength
	}
	if !opts.DenoiseEnabled {
		opts.DenoiseEnabled = cfg.FX.DenoiseEnabled
	}
	if opts.DenoiseStrength != 0 && opts.DenoiseStrength != 1 {
		opts.DenoiseStrength = cfg.FX.DenoiseStrength
	}
	return opts
}

func validateStreamOptions(opts StreamOptions) error {
	if opts.InputDevice == "" {
		return errors.New("--input is required")
	}
	if err := config.ValidateCameraFormat(opts.InputFormat); err != nil {
		return err
	}
	if opts.OutputDevice == "" {
		return errors.New("--output is required")
	}
	if err := config.ValidateOutputFormat(opts.OutputFormat); err != nil {
		return err
	}
	if opts.Width < 512 || opts.Height < 288 {
		return fmt.Errorf("fx stream size must be at least 512x288, got %dx%d", opts.Width, opts.Height)
	}
	if opts.FPS <= 0 {
		return fmt.Errorf("fx stream fps must be positive, got %d", opts.FPS)
	}
	if err := config.ValidateBackgroundMode(opts.BackgroundMode); err != nil {
		return err
	}
	if opts.BackgroundMode == "replace" && opts.BackgroundImage == "" {
		return errors.New("fx mode replace requires fx.background_image or --background-image; live V4L2 output cannot be transparent")
	}
	if err := config.ValidateChromaColor(opts.ChromaColor); err != nil {
		return err
	}
	if opts.DenoiseStrength != 0 && opts.DenoiseStrength != 1 {
		return fmt.Errorf("fx stream denoise strength must be 0 or 1, got %d", opts.DenoiseStrength)
	}
	if opts.DenoiseEnabled && opts.Height > 1080 {
		return fmt.Errorf("fx denoise supports up to 1080p input height, got %d; disable denoise or lower fx.height", opts.Height)
	}
	return nil
}

func validateTransferOptions(opts StreamOptions) error {
	if opts.InputDevice == "" {
		return errors.New("--input is required")
	}
	if !strings.EqualFold(opts.InputFormat, "nv12") {
		return fmt.Errorf("fx transfer input format must be nv12, got %q", opts.InputFormat)
	}
	if opts.OutputDevice == "" {
		return errors.New("--output is required")
	}
	if err := config.ValidateOutputFormat(opts.OutputFormat); err != nil {
		return err
	}
	if opts.Width < 512 || opts.Height < 288 {
		return fmt.Errorf("fx transfer size must be at least 512x288, got %dx%d", opts.Width, opts.Height)
	}
	if opts.FPS <= 0 {
		return fmt.Errorf("fx transfer fps must be positive, got %d", opts.FPS)
	}
	return nil
}

func (s *Supervisor) startFX(ctx context.Context) error {
	group, err := startFXPipeline(ctx, s.cfg, StreamOptions{}, s.logf)
	if err != nil {
		return err
	}
	s.setCurrent(group, StateActive, "Maxine FX is writing to "+fxOutputDevice(s.cfg))
	return nil
}

func (s *Supervisor) startIdle(ctx context.Context) error {
	opts := normalizeStreamOptions(s.cfg, StreamOptions{})
	if err := validateStreamOptions(opts); err != nil {
		return err
	}
	env, result := maxineEnv(s.cfg)
	if result.HelperPath == "" {
		return errors.New("Maxine helper binary not found; run make build")
	}
	group, err := startProcessGroup(ctx, "fx-idle", nil, s.logf, []commandSpec{
		{Name: result.HelperPath, Args: IdleOutputHelperArgs(result, opts), Env: env},
	})
	if err != nil {
		return err
	}
	s.setCurrent(group, StateIdle, "idle stream is writing to "+fxOutputDevice(s.cfg))
	return nil
}

func startFXPipeline(ctx context.Context, cfg config.Config, opts StreamOptions, logf func(string, ...any)) (*processGroup, error) {
	opts = normalizeStreamOptions(cfg, opts)
	if err := validateStreamOptions(opts); err != nil {
		return nil, err
	}
	env, result := maxineEnv(cfg)
	if result.HelperPath == "" {
		return nil, errors.New("Maxine helper binary not found; run make build")
	}
	if result.OSReleaseShim && result.ShimPath == "" {
		return nil, errors.New("CachyOS os-release shim not found; run make build")
	}
	if len(result.MissingFiles) > 0 {
		return nil, fmt.Errorf("Maxine SDK installation is incomplete: %s", strings.Join(result.MissingFiles, ", "))
	}
	var cleanup func()
	replacementPath := ""
	if opts.BackgroundMode == "replace" {
		var err error
		replacementPath, cleanup, err = prepareReplacementPPM(opts)
		if err != nil {
			return nil, err
		}
	}

	specs := []commandSpec{
		{Name: result.HelperPath, Args: NativeStreamHelperArgs(result, opts, replacementPath), Env: env},
	}
	group, err := startProcessGroupWithCleanup(ctx, "fx", nil, logf, cleanup, specs)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, err
	}
	return group, nil
}

func NativeStreamHelperArgs(result DoctorResult, opts StreamOptions, replacementPath string) []string {
	return []string{
		"native-stream",
		"--sdk-path", result.SDKPath,
		"--model-dir", result.ModelDir,
		"--input-device", opts.InputDevice,
		"--input-format", opts.InputFormat,
		"--output-device", opts.OutputDevice,
		"--output-format", opts.OutputFormat,
		"--width", strconv.Itoa(opts.Width),
		"--height", strconv.Itoa(opts.Height),
		"--fps", strconv.Itoa(opts.FPS),
		"--background", opts.BackgroundMode,
		"--replacement", replacementPath,
		"--chroma-color", opts.ChromaColor,
		"--blur-strength", fmt.Sprintf("%.3f", opts.BlurStrength),
		"--denoise", boolArg(opts.DenoiseEnabled),
		"--denoise-strength", strconv.Itoa(opts.DenoiseStrength),
	}
}

func NativeTransferHelperArgs(result DoctorResult, opts StreamOptions) []string {
	return []string{
		"native-transfer",
		"--sdk-path", result.SDKPath,
		"--model-dir", result.ModelDir,
		"--input-device", opts.InputDevice,
		"--input-format", opts.InputFormat,
		"--output-device", opts.OutputDevice,
		"--output-format", opts.OutputFormat,
		"--width", strconv.Itoa(opts.Width),
		"--height", strconv.Itoa(opts.Height),
		"--fps", strconv.Itoa(opts.FPS),
	}
}

func IdleOutputHelperArgs(result DoctorResult, opts StreamOptions) []string {
	return []string{
		"idle-output",
		"--output-device", opts.OutputDevice,
		"--output-format", opts.OutputFormat,
		"--width", strconv.Itoa(opts.Width),
		"--height", strconv.Itoa(opts.Height),
		"--fps", strconv.Itoa(opts.FPS),
		"--idle-label", "NV-vCam idling ...",
	}
}

func prepareReplacementPPM(opts StreamOptions) (string, func(), error) {
	path, err := config.ExpandPath(opts.BackgroundImage)
	if err != nil {
		return "", nil, err
	}
	img, err := LoadImage(path)
	if err != nil {
		return "", nil, fmt.Errorf("load replacement image: %w", err)
	}
	dir, err := os.MkdirTemp("", "nv-vcam-bg-*")
	if err != nil {
		return "", nil, err
	}
	out := filepath.Join(dir, "replacement.ppm")
	if err := WritePPM(out, ResizeCover(img, opts.Width, opts.Height)); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	return out, func() { _ = os.RemoveAll(dir) }, nil
}

func boolArg(value bool) string {
	if value {
		return "1"
	}
	return "0"
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

func (s *Supervisor) snapshotLocked() Snapshot {
	return Snapshot{
		State:        s.state,
		Device:       fxOutputDevice(s.cfg),
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
	Env  []string
}

type processGroup struct {
	name    string
	cmds    []*exec.Cmd
	done    chan struct{}
	once    sync.Once
	cleanup func()
}

func startProcessGroup(ctx context.Context, name string, env []string, logf func(string, ...any), specs []commandSpec) (*processGroup, error) {
	return startProcessGroupWithCleanup(ctx, name, env, logf, nil, specs)
}

func startProcessGroupWithCleanup(ctx context.Context, name string, env []string, logf func(string, ...any), cleanup func(), specs []commandSpec) (*processGroup, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	group := &processGroup{name: name, done: make(chan struct{}), cleanup: cleanup}
	var previous io.ReadCloser
	for i, spec := range specs {
		cmd := exec.CommandContext(ctx, spec.Name, spec.Args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if len(spec.Env) > 0 {
			cmd.Env = spec.Env
		} else if len(env) > 0 {
			cmd.Env = env
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return nil, err
		}
		go scanOutput(name, filepath.Base(spec.Name), stderr, logf)
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
		if group.cleanup != nil {
			group.cleanup()
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

func fxInputDevice(cfg config.Config) string {
	return cfg.Camera.InputDevice
}

func fxOutputDevice(cfg config.Config) string {
	return cfg.Output.Device
}
