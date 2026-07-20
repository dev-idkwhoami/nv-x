# nv-x Desktop App

This directory contains the Wails desktop app.

The frontend lives in `frontend/` and has already been initialized with Svelte 5, Tailwind CSS 4, and shadcn-svelte.

`app/wails.json` must contain:

```json
"build:tags": "webkit2_41"
```

This is required for the current Linux WebKit package version on this machine.

The current frontend baseline is Svelte 5, Tailwind CSS 4, and Vite. It has a `$lib` alias pointing at `frontend/src/lib`, which is the alias shadcn-svelte should use.

## UI Direction

- Wails backend in Go.
- Svelte 5 frontend.
- shadcn-svelte components by default.
- Do not hand-make components when a shadcn-svelte equivalent exists.

Current GUI surface:

- detected input and output devices
- service status and start/stop/restart controls
- loopback config status and reload/write controls
- config display and edit controls
- mutually exclusive NVIDIA video effects and camera selection
- mutually exclusive NVIDIA audio effects and PipeWire source selection
- background image replacement setting
- service logs/status view
