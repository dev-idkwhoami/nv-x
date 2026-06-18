package runner

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"nv-vcam/internal/capture"
	"nv-vcam/internal/config"
)

func Run(ctx context.Context, cfg config.Config) error {
	fmt.Println("nv-vcam RAW capture supervisor starting")
	fmt.Printf("raw device: %s (%s)\n", cfg.Input.Device, cfg.Input.Label)
	fmt.Printf("future fx output: %s (%s)\n", cfg.Output.Device, cfg.Output.Label)
	fmt.Printf("loopback config: %s\n", cfg.Loopback.ConfigPath)
	fmt.Printf("capture device: %s\n", cfg.Capture.Device)
	fmt.Printf("capture input: %s\n", cfg.Capture.InputCommand)

	// TODO: read frames from the configured RAW V4L2 device.
	// TODO: read frames from the configured input V4L2 device.
	// TODO: run a segmentation model against each frame.
	// TODO: blur or replace the frame background.
	// TODO: write processed frames to the output v4l2loopback device.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	supervisor := capture.NewSupervisor(cfg, func(format string, args ...any) {
		log.Printf(format, args...)
	})
	if err := supervisor.Run(ctx); err != nil {
		return err
	}
	fmt.Println("nv-vcam run stopped")
	return nil
}
