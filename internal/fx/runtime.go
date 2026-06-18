package fx

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	ort "github.com/yalue/onnxruntime_go"

	"nv-vcam/internal/config"
)

type DoctorResult struct {
	RuntimeLibraryPath string
	CUDAProviderPath   string
	ModelPath          string
	Provider           string
	DeviceID           int
	RuntimeOK          bool
	CUDAProviderOK     bool
	ModelExists        bool
	MissingLibraries   []string
	Message            string
}

func Doctor(cfg config.Config) DoctorResult {
	result := DoctorResult{
		RuntimeLibraryPath: cfg.FX.ONNXRuntimeLibraryPath,
		ModelPath:          cfg.FX.ModelPath,
		Provider:           cfg.FX.Provider,
		DeviceID:           cfg.FX.DeviceID,
	}
	libraryPath, err := config.ExpandPath(cfg.FX.ONNXRuntimeLibraryPath)
	if err != nil {
		result.Message = fmt.Sprintf("expand ONNX Runtime path: %v", err)
		return result
	}
	modelPath, err := config.ExpandPath(cfg.FX.ModelPath)
	if err != nil {
		result.Message = fmt.Sprintf("expand model path: %v", err)
		return result
	}
	result.RuntimeLibraryPath = libraryPath
	result.ModelPath = modelPath
	if _, err := os.Stat(modelPath); err == nil {
		result.ModelExists = true
	}
	resolvedLibraryPath, err := ResolveRuntimeLibrary(libraryPath)
	if err != nil {
		result.Message = err.Error()
		return result
	}
	result.RuntimeLibraryPath = resolvedLibraryPath

	if strings.EqualFold(cfg.FX.Provider, "cuda") {
		providerPath := filepath.Join(filepath.Dir(resolvedLibraryPath), "libonnxruntime_providers_cuda.so")
		result.CUDAProviderPath = providerPath
		result.MissingLibraries = MissingSharedLibraries(providerPath)
	}

	ort.SetSharedLibraryPath(resolvedLibraryPath)
	if err := ort.InitializeEnvironment(ort.WithLogLevelWarning()); err != nil {
		result.Message = fmt.Sprintf("initialize ONNX Runtime: %v", err)
		return result
	}
	result.RuntimeOK = true
	defer ort.DestroyEnvironment()

	if strings.EqualFold(cfg.FX.Provider, "cuda") {
		if err := checkCUDAProvider(cfg.FX.DeviceID); err != nil {
			if len(result.MissingLibraries) > 0 {
				result.Message = fmt.Sprintf("initialize CUDA execution provider: %v; missing shared libraries: %s", err, strings.Join(result.MissingLibraries, ", "))
				return result
			}
			result.Message = fmt.Sprintf("initialize CUDA execution provider: %v", err)
			return result
		}
		result.CUDAProviderOK = true
		result.Message = "ONNX Runtime and CUDA execution provider initialized"
		return result
	}

	result.Message = "ONNX Runtime initialized; CUDA provider not requested"
	return result
}

func ResolveRuntimeLibrary(configured string) (string, error) {
	if configured != "" {
		if _, err := os.Stat(configured); err == nil {
			return configured, nil
		}
	}
	for _, name := range []string{"libonnxruntime.so", "libonnxruntime.so.1"} {
		if path, ok := ldconfigPath(name); ok {
			return path, nil
		}
	}
	if configured == "" {
		configured = "libonnxruntime.so"
	}
	return "", fmt.Errorf("ONNX Runtime library is not available at %s and was not found in ldconfig; install onnxruntime-cuda or set fx.onnxruntime_library_path", configured)
}

func MissingSharedLibraries(path string) []string {
	if path == "" {
		return nil
	}
	cmd := exec.Command("ldd", path)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return nil
	}
	var missing []string
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "=> not found") {
			continue
		}
		name, _, _ := strings.Cut(line, "=>")
		name = strings.TrimSpace(name)
		if name != "" {
			missing = append(missing, name)
		}
	}
	return missing
}

func ldconfigPath(name string) (string, bool) {
	cmd := exec.Command("ldconfig", "-p")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "", false
	}
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, name+" ") && !strings.Contains(line, " "+name+" ") {
			continue
		}
		_, path, ok := strings.Cut(line, "=>")
		if !ok {
			continue
		}
		path = strings.TrimSpace(path)
		if path != "" {
			return path, true
		}
	}
	return "", false
}

func checkCUDAProvider(deviceID int) error {
	sessionOptions, err := ort.NewSessionOptions()
	if err != nil {
		return err
	}
	defer sessionOptions.Destroy()

	cudaOptions, err := ort.NewCUDAProviderOptions()
	if err != nil {
		return err
	}
	defer cudaOptions.Destroy()

	if err := cudaOptions.Update(map[string]string{
		"device_id": fmt.Sprintf("%d", deviceID),
	}); err != nil {
		return err
	}
	return sessionOptions.AppendExecutionProviderCUDA(cudaOptions)
}
