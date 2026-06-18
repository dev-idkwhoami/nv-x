# UI Plan

The desktop UI will live in the top-level `app/` Wails project.

Planned stack:

- Wails
- Svelte 5
- shadcn-svelte

The UI uses shadcn-svelte components whenever an equivalent exists. Avoid hand-made controls unless there is no suitable shadcn-svelte component.

Current and planned capabilities:

- manage virtual camera names and numbers
- start, stop, and reload loopback devices
- choose input and output devices
- show service logs and status
- configure future blur strength
- configure future background image

The first desktop implementation is Wails with a local web UI rendered through WebKit. A Fyne GUI is not planned for this repo unless the Wails path becomes blocked.
