# nv-vcam

`nv-vcam` is a native Linux "NVIDIA Broadcast-lite" virtual camera manager and effects service.

The current milestone is v0.1.0 RAW-first plus early Maxine FX validation: a Go CLI and Wails app that manage config files, inspect camera devices, write safe `v4l2loopback` configuration, control a systemd user service, supervise a Sony RAW capture pipeline, and validate NVIDIA Maxine still-image effects before realtime video integration.

## Current Milestone

- Linux only, with CachyOS / Arch Linux as the first target.
- Go service packages and CLI under the root module.
- Wails desktop app under `app/`.
- RAW Sony capture supervision through `gphoto2` and `ffmpeg`.
- No realtime FX streaming yet.
- Maxine FX validation is available for still images only.

## Intended Topology

```text
Sony DSLR
  -> nv-vcam.service
  -> /dev/video10 "Sony Camera RAW"
  -> future nv-vcam effects service
  -> /dev/video20 "Sony Camera FX"
  -> Teams/Zoom/Discord/etc.
```

The current working flow targets `/dev/video10` as the RAW camera. The service keeps a lightweight idle stream attached so camera apps can list the RAW device, then starts the real `gphoto2 -> ffmpeg -> /dev/video10` capture when an external app opens it. `/dev/video20` remains reserved for the later FX pipeline.

## Non-goals For This Milestone

- No ONNX, OpenCV, or RNNoise integration.
- No realtime background blur or replacement service yet.
- No GUI camera preview yet; validate feeds in a real browser or video app.
- No Docker.
- No Python.

## Dependencies

Runtime:

- Linux with systemd user services.
- `v4l2loopback-dkms` and matching kernel headers for the running kernel.
- `gphoto2` for Sony camera capture.
- `ffmpeg` with V4L2 output support.
- NVIDIA Maxine Video Effects SDK Core installed under `/usr/local/VideoFX` for FX validation.
- Maxine features: GreenScreen, BackgroundBlur, and Denoising packages. The current CLI uses GreenScreen and BackgroundBlur; Denoising is installed for later video work.
- `pkexec`/polkit for GUI loopback write/reload elevation.
- `fuser` from `psmisc` is optional but useful for troubleshooting busy devices.

Build:

- Go 1.24 or newer.
- Wails v2 CLI.
- Bun.
- WebKitGTK/GTK development packages required by Wails. On this system the Wails build uses the `webkit2_41` tag, configured in `app/wails.json`.

Arch/CachyOS package names are typically:

```bash
sudo pacman -S --needed go bun ffmpeg gphoto2 v4l2loopback-dkms psmisc polkit gcc
```

Install the kernel headers that match `uname -r`; on CachyOS this may be a CachyOS-specific headers package rather than plain `linux-headers`.

Install the Wails v2 CLI with Go:

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Make sure `$(go env GOPATH)/bin` is in `PATH` so `wails` can be found.

Install NVIDIA Maxine from NGC into `/usr/local/VideoFX` before using `nv-vcam fx doctor` or `nv-vcam fx test-image`. The NGC API key is only needed to download the SDK/model artifacts; the effects run locally.

## Build And Install

```bash
make check
make build
make install
```

`make install` installs the CLI/service binary to `~/.local/bin/nv-vcam`, the Maxine helper to `~/.local/bin/nv-vcam-maxine-helper`, and the CachyOS/Arch compatibility shim to `~/.local/lib/nv-vcam/nv-vcam-os-release-shim.so`. Make sure `~/.local/bin` is in your shell `PATH` if you want to run `nv-vcam` directly.

```bash
nv-vcam config write
sudo nv-vcam loopback write
nv-vcam loopback reload
nv-vcam service install --enable --start
```

If `~/.local/bin` is not in `PATH`, use `~/.local/bin/nv-vcam`.

## Example Commands

```bash
nv-vcam list
nv-vcam status
nv-vcam config show
nv-vcam config write
nv-vcam loopback show
nv-vcam loopback write --dry-run
nv-vcam loopback reload
nv-vcam service install
nv-vcam service start
nv-vcam service status
```

`nv-vcam loopback write` writes `/etc/modprobe.d/nv-vcam-v4l2loopback.conf`, so it requires root. If run without root, it prints the exact `sudo` command to run.

## FX Development

The `features/camera-fx` branch starts FX work with a still-image command before realtime video integration:

```bash
nv-vcam fx doctor
nv-vcam fx test-image --input ./input.jpg --blur-output ./blur.png --removed-output ./removed.png --mask ./mask.png
```

`nv-vcam fx doctor` validates the Maxine SDK layout, shared library dependencies, helper binary, CachyOS/Arch compatibility shim, TensorRT model files, and a synthetic GreenScreen/BackgroundBlur smoke test.

`nv-vcam fx test-image` runs Maxine GreenScreen and BackgroundBlur on a still image. It writes:

- `--blur-output`: a PNG/JPEG with the original foreground and Maxine-blurred background.
- `--removed-output`: a PNG/JPEG where the background is transparent.
- `--mask`: optional grayscale segmentation mask.

On CachyOS/Arch, the Maxine SDK can reject the host OS during `NvVFX_Load()`. `nv-vcam` enables a narrow `LD_PRELOAD` shim by default for helper processes only; it redirects Maxine's `/etc/os-release` read to an Ubuntu-shaped temporary file and does not change the system file.

Realtime FX streaming is intentionally not wired yet. The still-image commands are the validation step before connecting Maxine to `/dev/video10 -> /dev/video20`.

## RAW Capture Service

`nv-vcam run` is the systemd service entrypoint. It checks for `gphoto2` and `ffmpeg`, writes an idle stream to `/dev/video10`, watches for external consumers, and starts the Sony RAW capture pipeline on demand. When no consumer remains for the configured timeout, it stops the expensive capture process and returns to the idle stream.

The default capture config is rendered by `nv-vcam config show`:

```toml
[capture]
enabled = true
input_command = "gphoto2 --stdout --capture-movie"
device = "/dev/video10"
fps = 25
width = 2560
height = 1440
use_cuda_scale = true
idle_timeout_seconds = 15
idle_label = "nv-vcam idling ..."
```

The idle stream intentionally uses the same resolution and frame rate as the real capture stream. Keeping the negotiated V4L2 format stable avoids corrupted frames when an app stays attached across the idle-to-capture handoff.

## Existing Sony Setup

If `/etc/modprobe.d/sony-camera-v4l2loopback.conf` already manages `/dev/video10`, `nv-vcam loopback write` will refuse to write a competing config unless `--force` is provided. This is intentional; multiple active `v4l2loopback` option files can make reload behavior unclear.

## Loopback Reload Troubleshooting

If unload fails because devices are busy, stop camera consumers first. Useful checks:

```bash
fuser -v /dev/video10 /dev/video20
systemctl --user stop nv-vcam.service
```

Then reload:

```bash
sudo modprobe -r v4l2loopback
sudo modprobe v4l2loopback
```

## v4l2loopback On Arch / CachyOS

If `modprobe v4l2loopback` fails, install matching kernel headers and `v4l2loopback-dkms`, then rebuild DKMS modules. On CachyOS, make sure the headers match the running kernel.

Teams, Zoom, Discord, and browsers may cache camera names. Restart them after changing virtual camera labels.

If the RAW camera is visible but the handoff from idle to real capture is corrupt, restart the consuming app after restarting `nv-vcam.service`. Consumers can keep an old V4L2 format across process changes.

## Desktop App

The Wails app lives in `app/`. It uses Svelte 5, Tailwind CSS 4, and shadcn-svelte.

```bash
make dev
```

The GUI is for management/status. It intentionally does not preview camera feeds because Wails/WebKitGTK did not expose the loopback camera reliably on the target setup.
