package audio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nv-x/internal/config"
)

type State string

const (
	StateDisabled State = "disabled"
	StateStarting State = "starting"
	StateActive   State = "active"
	StateDegraded State = "degraded"
	StateError    State = "error"
)

type Snapshot struct {
	State         State  `json:"state"`
	Mode          string `json:"mode"`
	InputNode     string `json:"inputNode"`
	ResolvedInput string `json:"resolvedInput"`
	OutputNode    string `json:"outputNode"`
	Message       string `json:"message"`
	Restarts      int    `json:"restarts"`
	UpdatedAt     string `json:"updatedAt"`
}

type Source struct {
	NodeName    string `json:"nodeName"`
	Description string `json:"description"`
	Default     bool   `json:"default"`
	Available   bool   `json:"available"`
}

type pactlSource struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	State       string `json:"state"`
}

func listPactlDevices(ctx context.Context, kind, defaultCommand, exclude string) ([]Source, error) {
	defaultOut, _ := exec.CommandContext(ctx, "pactl", defaultCommand).Output()
	defaultName := strings.TrimSpace(string(defaultOut))
	out, err := exec.CommandContext(ctx, "pactl", "-f", "json", "list", kind).Output()
	if err != nil {
		return nil, fmt.Errorf("list PipeWire %s through pactl: %w", kind, err)
	}
	var raw []pactlSource
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode PipeWire %s: %w", kind, err)
	}
	result := make([]Source, 0, len(raw))
	for _, item := range raw {
		if item.Name == "" || item.Name == exclude || (kind == "sources" && strings.HasSuffix(item.Name, ".monitor")) {
			continue
		}
		result = append(result, Source{
			NodeName: item.Name, Description: item.Description,
			Default: item.Name == defaultName, Available: !strings.EqualFold(item.State, "UNAVAILABLE"),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Default != result[j].Default {
			return result[i].Default
		}
		return strings.ToLower(result[i].Description) < strings.ToLower(result[j].Description)
	})
	return result, nil
}

func ListSources(ctx context.Context, exclude string) ([]Source, error) {
	return listPactlDevices(ctx, "sources", "get-default-source", exclude)
}

func ListSinks(ctx context.Context) ([]Source, error) {
	return listPactlDevices(ctx, "sinks", "get-default-sink", "")
}

func DefaultStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "nv-x", "audio.json"), nil
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

func writeState(s Snapshot) {
	path, err := DefaultStatePath()
	if err != nil {
		return
	}
	s.UpdatedAt = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, append(data, '\n'), 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

type DoctorResult struct {
	HelperPath   string   `json:"helperPath"`
	SDKPath      string   `json:"sdkPath"`
	Models       []string `json:"models"`
	HelperOK     bool     `json:"helperOK"`
	Message      string   `json:"message"`
	HelperOutput string   `json:"helperOutput"`
}

func Doctor(cfg config.Config) DoctorResult {
	helper := findHelper()
	result := DoctorResult{HelperPath: helper, SDKPath: cfg.Audio.SDKPath}
	if helper == "" {
		result.Message = "audio helper binary not found; run make build"
		return result
	}
	for _, mode := range []string{"dereverb_denoiser", "studio_voice_low_latency"} {
		model, err := modelPath(cfg, mode)
		if err != nil {
			result.Message = err.Error()
			return result
		}
		result.Models = append(result.Models, model)
		cmd := exec.Command(helper, "doctor", "--sdk-path", cfg.Audio.SDKPath, "--mode", mode,
			"--model", model, "--intensity", strconv.FormatFloat(cfg.Audio.DereverbDenoiserIntensity, 'f', 3, 64))
		cmd.Env = helperEnv(cfg)
		out, err := cmd.CombinedOutput()
		result.HelperOutput += strings.TrimSpace(string(out)) + "\n"
		if err != nil {
			result.Message = fmt.Sprintf("%s doctor failed: %v", mode, err)
			return result
		}
	}
	result.HelperOK = true
	result.Message = "AFX dereverb/denoiser and Studio Voice Low Latency initialized"
	return result
}

type Supervisor struct {
	cfg  config.Config
	logf func(string, ...any)
	mu   sync.Mutex
	snap Snapshot
}

func NewSupervisor(cfg config.Config, logf func(string, ...any)) *Supervisor {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Supervisor{cfg: cfg, logf: logf, snap: Snapshot{State: StateDisabled, Mode: cfg.Audio.Mode, InputNode: cfg.Audio.InputNode, OutputNode: cfg.Audio.OutputNodeName}}
}

func (s *Supervisor) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}

func (s *Supervisor) set(state State, message string, restarts int) {
	s.mu.Lock()
	s.snap.State, s.snap.Message, s.snap.Restarts = state, message, restarts
	s.snap.UpdatedAt = time.Now().Format(time.RFC3339)
	snap := s.snap
	s.mu.Unlock()
	writeState(snap)
}

func (s *Supervisor) Run(ctx context.Context) error {
	if s.cfg.Audio.Mode == "off" {
		s.set(StateDisabled, "audio effects are off", 0)
		<-ctx.Done()
		return nil
	}
	if err := config.ValidateAudioMode(s.cfg.Audio.Mode); err != nil {
		return err
	}
	helper := findHelper()
	if helper == "" {
		s.set(StateError, "audio helper binary not found", 0)
		<-ctx.Done()
		return nil
	}
	model, err := modelPath(s.cfg, s.cfg.Audio.Mode)
	if err != nil {
		s.set(StateError, err.Error(), 0)
		<-ctx.Done()
		return nil
	}
	resolved := s.cfg.Audio.InputNode
	if resolved == "" {
		if sources, listErr := ListSources(ctx, s.cfg.Audio.OutputNodeName); listErr == nil {
			for _, source := range sources {
				if source.Default {
					resolved = source.NodeName
					break
				}
			}
		}
	}
	s.mu.Lock()
	s.snap.ResolvedInput = resolved
	s.mu.Unlock()

	restarts := 0
	for ctx.Err() == nil {
		s.set(StateStarting, "starting NVIDIA audio effect", restarts)
		args := []string{"run", "--sdk-path", s.cfg.Audio.SDKPath, "--mode", s.cfg.Audio.Mode,
			"--model", model, "--intensity", strconv.FormatFloat(s.cfg.Audio.DereverbDenoiserIntensity, 'f', 3, 64),
			"--output-node", s.cfg.Audio.OutputNodeName, "--output-description", s.cfg.Audio.OutputDescription}
		if s.cfg.Audio.InputNode != "" {
			args = append(args, "--input-node", s.cfg.Audio.InputNode)
		}
		if s.cfg.Audio.MonitorEnabled {
			args = append(args, "--monitor", "true")
			if s.cfg.Audio.MonitorOutputNode != "" {
				args = append(args, "--monitor-output-node", s.cfg.Audio.MonitorOutputNode)
			}
		}
		cmd := exec.CommandContext(ctx, helper, args...)
		cmd.Env = helperEnv(s.cfg)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			s.set(StateError, err.Error(), restarts)
		} else {
			s.set(StateActive, "NV-X Microphone is active", restarts)
			err = cmd.Wait()
		}
		if ctx.Err() != nil {
			return nil
		}
		restarts++
		s.set(StateDegraded, fmt.Sprintf("audio helper stopped: %v; retrying", err), restarts)
		s.logf("audio helper stopped: %v", err)
		delay := time.Duration(min(restarts, 5)) * time.Second
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
	}
	return nil
}

func findHelper() string {
	if path := os.Getenv("NV_X_AUDIO_HELPER"); path != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	if exe, err := os.Executable(); err == nil {
		if path := existing(filepath.Join(filepath.Dir(exe), "nv-x-audio")); path != "" {
			return path
		}
	}
	if path, err := exec.LookPath("nv-x-audio"); err == nil {
		return path
	}
	return existing("bin/nv-x-audio")
}

func existing(path string) string {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

func modelPath(cfg config.Config, mode string) (string, error) {
	name := "dereverb_denoiser_48k_*.trtpkg"
	feature := "dereverb_denoiser"
	if mode == "studio_voice_low_latency" {
		name, feature = "studio_voice_low_latency_48k_*.trtpkg", "studio_voice"
	}
	root, _ := config.ExpandPath(cfg.Audio.SDKPath)
	matches, _ := filepath.Glob(filepath.Join(root, "features", feature, "models", "sm_*", name))
	if len(matches) == 0 {
		return "", fmt.Errorf("model for %s not found below %s", mode, root)
	}
	if len(matches) > 1 {
		return "", errors.New("multiple GPU architecture models installed; keep only the model matching this GPU")
	}
	return matches[0], nil
}

func helperEnv(cfg config.Config) []string {
	root, _ := config.ExpandPath(cfg.Audio.SDKPath)
	paths := []string{
		filepath.Join(root, "nvafx", "lib"),
		filepath.Join(root, "external", "cuda", "lib"),
		filepath.Join(root, "features", "dereverb_denoiser", "lib"),
		filepath.Join(root, "features", "studio_voice", "lib"),
	}
	env := os.Environ()
	value := strings.Join(paths, string(os.PathListSeparator))
	for i, entry := range env {
		if strings.HasPrefix(entry, "LD_LIBRARY_PATH=") {
			env[i] = entry + string(os.PathListSeparator) + value
			return env
		}
	}
	return append(env, "LD_LIBRARY_PATH="+value)
}
