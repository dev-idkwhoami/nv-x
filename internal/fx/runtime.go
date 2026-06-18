package fx

import (
	"fmt"
	"os"
	"strings"

	ort "github.com/yalue/onnxruntime_go"

	"nv-vcam/internal/config"
)

type DoctorResult struct {
	RuntimeLibraryPath string
	ModelPath          string
	Provider           string
	DeviceID           int
	RuntimeOK          bool
	CUDAProviderOK     bool
	ModelExists        bool
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
	if _, err := os.Stat(libraryPath); err != nil {
		result.Message = fmt.Sprintf("ONNX Runtime library is not available at %s: %v", libraryPath, err)
		return result
	}

	ort.SetSharedLibraryPath(libraryPath)
	if err := ort.InitializeEnvironment(ort.WithLogLevelWarning()); err != nil {
		result.Message = fmt.Sprintf("initialize ONNX Runtime: %v", err)
		return result
	}
	result.RuntimeOK = true
	defer ort.DestroyEnvironment()

	if strings.EqualFold(cfg.FX.Provider, "cuda") {
		if err := checkCUDAProvider(cfg.FX.DeviceID); err != nil {
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
