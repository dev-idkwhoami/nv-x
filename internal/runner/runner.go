package runner

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"nv-x/internal/audio"
	"nv-x/internal/config"
	"nv-x/internal/fx"
)

func Run(ctx context.Context, cfg config.Config) error {
	fmt.Println("nv-x effects service starting")
	fmt.Printf("camera input: %s (%s %dx%d @ %dfps)\n", cfg.Camera.InputDevice, cfg.Camera.InputFormat, cfg.Camera.Width, cfg.Camera.Height, cfg.Camera.FPS)
	fmt.Printf("virtual output: %s (%s, %s)\n", cfg.Output.Device, cfg.Output.Label, cfg.Output.OutputFormat)
	fmt.Printf("loopback config: %s\n", cfg.Loopback.ConfigPath)
	fmt.Printf("fx enabled: %t\n", cfg.FX.Enabled)
	fmt.Printf("fx mode: %s\n", cfg.FX.Mode)
	fmt.Printf("audio mode: %s\n", cfg.Audio.Mode)
	if cfg.Audio.InputNode == "" {
		fmt.Println("audio input: system default")
	} else {
		fmt.Printf("audio input: %s\n", cfg.Audio.InputNode)
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	logf := func(format string, args ...any) {
		log.Printf(format, args...)
	}
	fxSupervisor := fx.NewSupervisor(cfg, logf)
	audioSupervisor := audio.NewSupervisor(cfg, logf)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := fxSupervisor.Run(ctx); err != nil {
			log.Printf("fx supervisor stopped with error: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := audioSupervisor.Run(ctx); err != nil {
			log.Printf("audio supervisor stopped with error: %v", err)
		}
	}()
	wg.Wait()
	fmt.Println("nv-x run stopped")
	return nil
}
