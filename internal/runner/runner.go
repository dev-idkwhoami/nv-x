package runner

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"nv-vcam/internal/config"
	"nv-vcam/internal/fx"
)

func Run(ctx context.Context, cfg config.Config) error {
	fmt.Println("nv-vcam native ingest supervisor starting")
	fmt.Printf("camera input: %s (%s %dx%d @ %dfps)\n", cfg.Camera.InputDevice, cfg.Camera.InputFormat, cfg.Camera.Width, cfg.Camera.Height, cfg.Camera.FPS)
	fmt.Printf("virtual output: %s (%s, %s)\n", cfg.Output.Device, cfg.Output.Label, cfg.Output.OutputFormat)
	fmt.Printf("loopback config: %s\n", cfg.Loopback.ConfigPath)
	fmt.Printf("fx enabled: %t\n", cfg.FX.Enabled)
	fmt.Printf("fx mode: %s\n", cfg.FX.Mode)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	logf := func(format string, args ...any) {
		log.Printf(format, args...)
	}
	fxSupervisor := fx.NewSupervisor(cfg, logf)
	if err := fxSupervisor.Run(ctx); err != nil {
		log.Printf("fx supervisor stopped with error: %v", err)
	}
	fmt.Println("nv-vcam run stopped")
	return nil
}
