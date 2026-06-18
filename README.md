# nv-vcam

`nv-vcam` is a native Linux "NVIDIA Broadcast-lite" virtual camera manager and effects service.

> [!WARNING]
> FX mode is currently expected to add about 1 second of end-to-end latency on the target setup. The RAW `/dev/video10` feed remains the low-latency path. Use `/dev/video20` only when you want Maxine effects.

The current milestone is v0.1.0 RAW-first plus early Maxine FX: a Go CLI and Wails app that manage config files, inspect camera devices, write safe `v4l2loopback` configuration, control a systemd user service, supervise a Sony RAW capture pipeline, validate NVIDIA Maxine still-image effects, and run an on-demand background effects stream.

## Current Milestone

- Linux only, with CachyOS / Arch Linux as the first target.
- Go service packages and CLI under the root module.
- Wails desktop app under `app/`.
- RAW Sony capture supervision through `gphoto2` and `ffmpeg`.
- On-demand realtime Maxine background blur, mask output, chroma background, or image replacement from `/dev/video10` to `/dev/video20`.
- Maxine still-image validation commands.

## Intended Topology

```text
Sony DSLR
  -> nv-vcam.service
  -> /dev/video10 "Sony Camera RAW"
  -> nv-vcam Maxine effects pipeline
  -> /dev/video20 "Sony Camera FX"
  -> Teams/Zoom/Discord/etc.
```

The current working flow targets `/dev/video10` as the RAW camera. The service keeps a lightweight idle stream attached so camera apps can list the RAW device, then starts the real `gphoto2 -> ffmpeg -> /dev/video10` capture when an external app opens it. `/dev/video20` is the FX camera. It idles until an app opens it, then starts a Maxine pipeline fed by `/dev/video10`.

## Non-goals For This Milestone

- No ONNX, OpenCV, or RNNoise integration.
- No GUI controls for FX tuning yet.
- No GUI camera preview yet; validate feeds in a real browser or video app.
- No Docker.
- No Python.

## Dependencies

Runtime:

- Linux with systemd user services.
- `v4l2loopback-dkms` and matching kernel headers for the running kernel.
- `gphoto2` for Sony camera capture.
- `ffmpeg` with V4L2 output support.
- NVIDIA Maxine Video Effects SDK Core installed under `/usr/local/VideoFX`.
- Maxine feature packages installed under `/usr/local/VideoFX/features`:
  - `nvvfxgreenscreen` for segmentation.
  - `nvvfxbackgroundblur` for blur mode.
  - `nvvfxdenoising` is optional/experimental and not used by default.
- Maxine TensorRT model packages under `/usr/local/VideoFX/lib/models`:
  - `AIGS_288x512_89_*.engine.trtpkg` for AI Green Screen.
  - `Denoise-*-89.engine.trtpkg` only if trying optional denoise.
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

Install NVIDIA Maxine from NGC into `/usr/local/VideoFX` before using FX commands. The NGC API key is only needed to download the SDK/model artifacts; the effects run locally.

The currently tested NGC artifact set is:

- SDK core: `VFXSDK_linux_1.2.0.0.tgz` / Maxine VFX SDK Core.
- GreenScreen: `nvidia/maxine/nvvfxgreenscreen:1.2.0.0_lib_linux`.
- BackgroundBlur: `nvidia/maxine/nvvfxbackgroundblur:1.2.0.0_lib_linux`.
- Webcam Denoise: `nvidia/maxine/nvvfxdenoising:1.2.0.0_lib_linux` if you want to experiment with `--denoise`.

The app checks for Maxine headers/libraries and model files with `nv-vcam fx doctor`.

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

## FX Modes

FX is enabled with `[fx].enabled = true`. Disable it entirely with `[fx].enabled = false` and use the RAW `/dev/video10` feed for the lowest latency path.

```bash
nv-vcam fx doctor
nv-vcam fx test-image --input ./input.jpg --blur-output ./blur.png --removed-output ./removed.png --mask ./mask.png
```

`nv-vcam fx doctor` validates the Maxine SDK layout, shared library dependencies, helper binary, CachyOS/Arch compatibility shim, TensorRT model files, and a synthetic GreenScreen/BackgroundBlur smoke test.

`nv-vcam fx test-image` runs Maxine effects on a still image. It writes:

- `--blur-output`: a PNG/JPEG with the original foreground and Maxine-blurred background.
- `--removed-output`: a PNG/JPEG where the background is transparent.
- `--mask`: optional grayscale segmentation mask.
- `--final-output`: optional selected output for `--background blur|mask|replace|chroma`.

The final FX output mode is controlled with `--background blur|mask|replace|chroma` or `[fx].background_mode` in the config:

- `blur`: runs GreenScreen and BackgroundBlur.
- `replace`: runs GreenScreen only, then composites the foreground over `[fx].background_image`.
- `chroma`: runs GreenScreen only, then composites the foreground over `[fx].chroma_color`.
- `mask`: runs GreenScreen only and outputs the grayscale segmentation mask as a debug view.

For still images, `replace` without `--background-image` or `[fx].background_image` writes a transparent foreground to `--final-output`. For live V4L2 output, `replace` requires a background image because camera apps do not receive an alpha channel.

Webcam Denoise can be tried with `--denoise --denoise-strength 0|1`, but it is intentionally off by default. It has stricter SDK constraints than the background effects and is not part of the normal service path.

The helper is mode-aware: it only loads/runs BackgroundBlur for `blur`; `replace`, `chroma`, and `mask` skip BackgroundBlur.

On CachyOS/Arch, the Maxine SDK can reject the host OS during `NvVFX_Load()`. `nv-vcam` enables a narrow `LD_PRELOAD` shim by default for helper processes only; it redirects Maxine's `/etc/os-release` read to an Ubuntu-shaped temporary file and does not change the system file.

Realtime FX streaming is available as the first background effects path:

```bash
nv-vcam fx stream --input /dev/video10 --output /dev/video20 --background blur
nv-vcam fx stream --input /dev/video10 --output /dev/video20 --background replace --background-image ~/Pictures/background.png
nv-vcam fx stream --input /dev/video10 --output /dev/video20 --background chroma --chroma-color '#00ff00'
```

The normal service path runs the same pipeline on demand. `nv-vcam run` keeps an idle stream attached to `/dev/video20` so applications can discover "Sony Camera FX". When an external app opens `/dev/video20`, the FX supervisor starts:

```text
ffmpeg /dev/video10 -> raw bgr24
  -> nv-vcam-maxine-helper stream
  -> ffmpeg raw bgr24 -> yuv420p -> /dev/video20
```

When `background_mode = "replace"`, `nv-vcam` loads `[fx].background_image`, scales/crops it once to the configured FX frame size, and passes that prepared frame to the helper for realtime compositing. When `background_mode = "chroma"`, it uses `[fx].chroma_color` directly and does not load a background image.

The current realtime FX pipeline is:

```text
/dev/video10 -> ffmpeg raw bgr24
  -> nv-vcam-maxine-helper
  -> ffmpeg yuv420p -> /dev/video20
```

This pipeline keeps the implementation simple and reliable, but it adds conversion and pipe buffering overhead. Expect roughly 1 second of latency with FX enabled on the current setup.

The FX pipeline opening `/dev/video10` acts as a RAW consumer, so the existing RAW capture supervisor starts the Sony camera feed automatically. Closing the FX consumer stops Maxine after the configured idle timeout and returns `/dev/video20` to the lightweight idle stream.

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
