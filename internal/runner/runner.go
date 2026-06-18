package runner

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"nv-vcam/internal/capture"
	"nv-vcam/internal/config"
	"nv-vcam/internal/fx"
)

func Run(ctx context.Context, cfg config.Config) error {
	fmt.Println("nv-vcam RAW capture supervisor starting")
	fmt.Printf("raw device: %s (%s)\n", cfg.Input.Device, cfg.Input.Label)
	fmt.Printf("future fx output: %s (%s)\n", cfg.Output.Device, cfg.Output.Label)
	fmt.Printf("loopback config: %s\n", cfg.Loopback.ConfigPath)
	fmt.Printf("capture device: %s\n", cfg.Capture.Device)
	fmt.Printf("capture input: %s\n", cfg.Capture.InputCommand)
	fmt.Printf("fx enabled: %t\n", cfg.FX.Enabled)
	fmt.Printf("fx pipeline: %s -> %s\n", cfg.FX.InputDevice, cfg.FX.OutputDevice)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	logf := func(format string, args ...any) {
		log.Printf(format, args...)
	}
	rawSupervisor := capture.NewSupervisor(cfg, logf)
	fxSupervisor := fx.NewSupervisor(cfg, logf)
	fxSupervisor.SetInputIgnorePIDsFunc(rawSupervisor.OwnedPIDs)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := rawSupervisor.Run(ctx); err != nil {
			log.Printf("raw supervisor stopped with error: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := fxSupervisor.Run(ctx); err != nil {
			log.Printf("fx supervisor stopped with error: %v", err)
		}
	}()
	wg.Wait()
	fmt.Println("nv-vcam run stopped")
	return nil
}
