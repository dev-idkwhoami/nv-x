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
- Maxine still-image validation commands.

## Dependencies

Runtime:

- Linux with systemd user services.
- `v4l2loopback-dkms` and matching kernel headers for the running kernel.
- Cam Link or another V4L2 input that can provide `NV12 1920x1080 @ 50fps`.
- NVIDIA Maxine Video Effects SDK Core installed under `/usr/local/VideoFX`.
- Maxine feature packages installed under `/usr/local/VideoFX/features`:
  - `nvvfxgreenscreen` for segmentation.
  - `nvvfxbackgroundblur` for blur mode.
  - `nvvfxdenoising` is optional/experimental and off by default.
- Maxine TensorRT model packages under `/usr/local/VideoFX/lib/models`.
- `pkexec`/polkit for GUI loopback write/reload elevation.
- `fuser` from `psmisc` is optional but useful for troubleshooting busy devices.

Build:

- Go 1.24 or newer.
- Wails v2 CLI.
- Bun.
- WebKitGTK/GTK development packages required by Wails. On this system the Wails build uses the `webkit2_41` tag, configured in `app/wails.json`.

Arch/CachyOS package names are typically:

```bash
sudo pacman -S --needed go bun v4l2loopback-dkms psmisc polkit gcc
```

Install the kernel headers that match `uname -r`; on CachyOS this may be a CachyOS-specific headers package rather than plain `linux-headers`.

Install the Wails v2 CLI with Go:

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Install NVIDIA Maxine from NGC into `/usr/local/VideoFX` before using FX commands. The app checks for Maxine headers/libraries and model files with `nv-vcam fx doctor`.

## Build And Install

```bash
make check
make build
make install
```

`make install` installs the CLI/service binary to `~/.local/bin/nv-vcam`, the Maxine helper to `~/.local/bin/nv-vcam-maxine-helper`, and the CachyOS/Arch compatibility shim to `~/.local/lib/nv-vcam/nv-vcam-os-release-shim.so`.

```bash
nv-vcam config write
sudo nv-vcam loopback write
nv-vcam loopback reload
nv-vcam service install --enable --start
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
denoise_enabled = false
denoise_strength = 0
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

The normal service path runs the same native helper on demand. `nv-vcam run` watches `/dev/video10`; when an external app opens the virtual camera, it starts `nv-vcam-maxine-helper native-stream`. When no consumer remains, it stops the helper.

On CachyOS/Arch, the Maxine SDK can reject the host OS during `NvVFX_Load()`. `nv-vcam` enables a narrow `LD_PRELOAD` shim by default for helper processes only; it redirects Maxine's `/etc/os-release` read to an Ubuntu-shaped temporary file and does not change the system file.

## Manual Validation

```bash
v4l2-ctl -d /dev/video0 --list-formats-ext
nv-vcam loopback write --dry-run
sudo nv-vcam loopback write
nv-vcam loopback reload
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

The GUI is for management/status. It intentionally does not preview camera feeds because Wails/WebKitGTK did not expose the loopback camera reliably on the target setup.
