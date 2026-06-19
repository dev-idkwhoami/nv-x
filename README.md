# nv-vcam

`nv-vcam` is a native Linux "NVIDIA Broadcast-lite" virtual camera manager and effects service.

The current native-ingest path is Cam Link first:

```text
/dev/video0 Cam Link
  -> nv-vcam-maxine-helper native-stream
       V4L2 input
       CPU NV12 -> BGR
       NVIDIA Maxine effects
       CPU BGR -> YU12
       V4L2 output
  -> /dev/video10 "NV-vCam"
  -> Teams/Zoom/Discord/browser/etc.
```

Default mode is `/dev/video0` `NV12` `1920x1080 @ 50fps` into `/dev/video10` `YU12/yuv420p` `1920x1080 @ 50fps`.

## Current Milestone

- Linux only, with CachyOS / Arch Linux as the first target.
- Go CLI/service packages under the root module.
- Wails desktop app under `app/`.
- One `v4l2loopback` output camera: `/dev/video10 "NV-vCam"`.
- On-demand native Maxine background blur, mask output, chroma background, or image replacement.
- Optional Elgato ring light auto-control when the virtual camera has consumers.
- Maxine still-image validation commands.

## Dependencies

Runtime:

- Linux with systemd user services.
- `v4l2loopback-dkms` and matching kernel headers for the running kernel.
- Cam Link or another V4L2 input that can provide `NV12 1920x1080 @ 50fps`.
- NVIDIA GPU/driver stack compatible with NVIDIA Maxine.
- NVIDIA NGC CLI installed and authenticated with `ngc config set` for SDK Core download.
- NVIDIA NGC API key exported as `NGC_CLI_API_KEY` for Maxine feature installation.
- NVIDIA Maxine Video Effects SDK Core installed under `/usr/local/VideoFX`.
- Maxine feature packages installed under `/usr/local/VideoFX/features`:
  - `nvvfxgreenscreen` for segmentation.
  - `nvvfxbackgroundblur` for blur mode.
- Maxine TensorRT model packages under `/usr/local/VideoFX/lib/models`.
- `pkexec`/polkit for GUI loopback write/reload elevation.
- `fuser` from `psmisc` is optional but useful for troubleshooting busy devices.
- Wails GUI runtime dependencies: GTK 3 and WebKitGTK 4.1.

Build:

- Go 1.24 or newer.
- Wails v2 CLI.
- Bun.
- WebKitGTK/GTK development packages required by Wails. On this system the Wails build uses the `webkit2_41` tag, configured in `app/wails.json`.
- C/C++ build tools for the native Maxine helper.
- `makepkg`/`pacman` for the CachyOS/Arch package install flow.

Arch/CachyOS package names are typically:

```bash
sudo pacman -S --needed go gcc make wails webkit2gtk-4.1 gtk3 v4l2loopback-dkms psmisc polkit
```

Install the kernel headers that match `uname -r`; on CachyOS this may be a CachyOS-specific headers package rather than plain `linux-headers`.

`bun` may be installed either through pacman or your existing user install. The local package target runs `makepkg --nodeps`, so it can use `bun` from your current `PATH` instead of requiring the pacman `bun` package. For a strict redistributable PKGBUILD, install `bun` through pacman and remove `--nodeps`.

If `wails` is not available from pacman on the target machine, install the Wails v2 CLI with Go:

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Set up the NVIDIA NGC CLI before first setup:

```bash
ngc config set
export NGC_CLI_API_KEY=<your-ngc-api-key>
```

`nv-vcam setup` downloads `nvidia/maxine/vfx_sdk_core:1.2.0.0_linux` from NGC, extracts it into `/usr/local/VideoFX`, then runs NVIDIA's feature installer for the required GreenScreen and BackgroundBlur packages.

## Build And Install

Recommended CachyOS/Arch desktop install:

```bash
make check
make desktop
```

`make desktop` builds a local Arch package from `packaging/arch/PKGBUILD`, removes stale user-local GUI files that can shadow the package, installs the newest `nv-vcam-*.pkg.tar.zst` with `sudo pacman -U`, and refreshes desktop caches when the cache tools are available.

The package installs:

```text
/usr/bin/nv-vcam
/usr/bin/nv-vcam-gui
/usr/bin/nv-vcam-maxine-helper
/usr/lib/nv-vcam/nv-vcam-os-release-shim.so
/usr/share/applications/nv-vcam-gui.desktop
/usr/share/icons/hicolor/256x256/apps/nv-vcam-gui.png
```

The GUI binary is intentionally named `nv-vcam-gui`; the CLI remains `nv-vcam`.

Manual package steps, equivalent to the package part of `make desktop`:

```bash
make package
sudo pacman -U packaging/arch/nv-vcam-*.pkg.tar.zst
```

Developer-local install is still available:

```bash
make install
```

`make install` installs into `~/.local/bin` and `~/.local/lib/nv-vcam`. It is useful for development, but the package path is preferred for a normal CachyOS/Arch desktop because Plasma/Gtk launchers resolve `/usr/bin` and `/usr/share/applications` more reliably than ad hoc user-local desktop entries.

```bash
export NGC_CLI_API_KEY=<your-ngc-api-key>
nv-vcam setup
```

Run `nv-vcam setup` as your normal desktop user, not with `sudo`. Setup validates sudo once up front, then invokes `sudo` only for the root-scoped parts: extracting SDK Core into `/usr/local`, writing `/etc/modprobe.d/nv-vcam-v4l2loopback.conf`, and reloading `v4l2loopback`. The service is a systemd user service and must be installed for your desktop account.

`nv-vcam setup` creates the user config if missing, downloads and extracts the Maxine SDK Core tarball if `/usr/local/VideoFX` is not present, installs `nvvfxgreenscreen,nvvfxbackgroundblur`, writes and reloads the v4l2loopback config with sudo subcommands, installs/enables/starts the user service, and finishes with `fx doctor`.

Useful partial setup flags:

```bash
nv-vcam setup --dry-run
nv-vcam setup --skip-sdk
nv-vcam setup --skip-maxine
nv-vcam setup --skip-loopback
nv-vcam setup --skip-service
nv-vcam setup --force
```

## Config

The default config rendered by `nv-vcam config show` is:

```toml
[camera]
input_device = "/dev/video0"
input_format = "nv12"
width = 1920
height = 1080
fps = 50

[output]
device = "/dev/video10"
video_nr = 10
label = "NV-vCam"
output_format = "yuv420p"

[loopback]
config_path = "/etc/modprobe.d/nv-vcam-v4l2loopback.conf"
exclusive_caps = true
max_buffers = 8

[fx]
enabled = true
mode = "blur"
background_image = ""
chroma_color = "#00ff00"
sdk_path = "/usr/local/VideoFX"
model_dir = "/usr/local/VideoFX/lib/models"
enable_os_release_shim = true
blur_strength = 0.75

[light]
enabled = false
address = ""
brightness = 20
temperature = 206
timeout_ms = 1500
```

`nv-vcam loopback write` renders:

```conf
options v4l2loopback devices=1 video_nr=10 card_label="NV-vCam" exclusive_caps=1 max_buffers=8
```

## FX Modes

FX is enabled with `[fx].enabled = true`. The selected live output mode is `[fx].mode` or `--background blur|mask|replace|chroma` for CLI commands:

- `blur`: runs GreenScreen and BackgroundBlur.
- `replace`: runs GreenScreen, then composites the foreground over `[fx].background_image`.
- `chroma`: runs GreenScreen, then composites the foreground over `[fx].chroma_color`.
- `mask`: runs GreenScreen and outputs the grayscale segmentation mask as a debug view.

Still-image validation:

```bash
nv-vcam fx doctor
nv-vcam fx test-image --input ./input.jpg --blur-output ./blur.png --removed-output ./removed.png --mask ./mask.png
```

Live native stream:

```bash
nv-vcam fx stream --input /dev/video0 --output /dev/video10 --background blur
nv-vcam fx stream --input /dev/video0 --output /dev/video10 --background replace --background-image ~/Pictures/background.png
nv-vcam fx stream --input /dev/video0 --output /dev/video10 --background chroma --chroma-color '#00ff00'
```

Transfer-only diagnostic path:

```bash
nv-vcam fx transfer --input /dev/video0 --output /dev/video10 --width 1920 --height 1080 --fps 50
```

This sends NV12 through `NvCVImage_Transfer()` into a GPU BGR buffer and back to CPU BGR, then writes YU12 with the existing output converter. It does not run GreenScreen, BackgroundBlur, chroma, or replacement.

The normal service path runs the same native helper on demand. `nv-vcam run` watches `/dev/video10`; when an external app opens the virtual camera, it starts `nv-vcam-maxine-helper native-stream`. When no consumer remains, it stops the helper.

On CachyOS/Arch, the Maxine SDK can reject the host OS during `NvVFX_Load()`. `nv-vcam` enables a narrow `LD_PRELOAD` shim by default for helper processes only; it redirects Maxine's `/etc/os-release` read to an Ubuntu-shaped temporary file and does not change the system file.

## Light Auto-Control

`[light].enabled = true` lets the service turn an Elgato light on when an external app starts consuming `/dev/video10`, and turn it off when the stream returns to idle.

If `[light].address` is empty, `nv-vcam` tries to reuse the active IP from `~/.config/elgato-light-toggle/config.json`. If no light is configured or reachable, camera setup and streaming continue; the service logs the skipped light update and does not fail the stream.

`brightness` is `0-100`. `temperature` uses Elgato's API range, currently validated as `143-344`.

## Planned Features

- Animated backgrounds: support a preprocessed frame-folder asset format, such as `manifest.json` plus JPG/PNG frames, that loops during the live camera pipeline without requiring ffmpeg at runtime. Importing arbitrary video containers like WebM/MP4 may be added as an optional conversion step that can use ffmpeg when available, but the live camera service should remain native and ffmpeg-free.

## Manual Validation

```bash
v4l2-ctl -d /dev/video0 --list-formats-ext
nv-vcam loopback write --dry-run
nv-vcam setup --dry-run
nv-vcam run
```

Then open `/dev/video10` in a browser or video-call app and verify there are no purple/green artifacts.

To confirm the normal FX path is native and not using the old ffmpeg bridge:

```bash
pgrep -a ffmpeg
```

## Loopback Reload Troubleshooting

If unload fails because devices are busy, stop camera consumers first. Useful checks:

```bash
fuser -v /dev/video10
systemctl --user stop nv-vcam.service
```

Then reload:

```bash
sudo modprobe -r v4l2loopback
sudo modprobe v4l2loopback
```

Teams, Zoom, Discord, and browsers may cache camera names. Restart them after changing virtual camera labels.

## Desktop App

The Wails app lives in `app/`. It uses Svelte 5, Tailwind CSS 4, and shadcn-svelte.

```bash
make dev
```

`make dev` / `wails dev` starts the developer dashboard with detailed device, service, loopback, config, and FX diagnostics.

Production builds use the slim user UI:

```bash
make build
app/build/bin/nv-vcam-gui
```

The installed desktop app from `make desktop` is launched as `nv-vcam-gui`. The user UI provides direct background mode selection, inline mode settings, a Settings page for theme and Elgato light auto-control, and debounced automatic config save/service restart.
