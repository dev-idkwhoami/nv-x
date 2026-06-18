SHELL := /usr/bin/env bash

GOCACHE ?= /tmp/nv-vcam-go-cache
BUN_TMPDIR ?= /tmp/nv-vcam-bun-tmp
BUN_INSTALL ?= /tmp/nv-vcam-bun-install

.PHONY: build dev install uninstall test check clean

build:
	GOCACHE="$(GOCACHE)" go build -buildvcs=false -o bin/nv-vcam ./cmd/nv-vcam
	cd app/frontend && BUN_TMPDIR="$(BUN_TMPDIR)" BUN_INSTALL="$(BUN_INSTALL)" bun run build
	cd app && GOCACHE="$(GOCACHE)" BUN_TMPDIR="$(BUN_TMPDIR)" BUN_INSTALL="$(BUN_INSTALL)" wails build

dev:
	cd app && GOCACHE="$(GOCACHE)" BUN_TMPDIR="$(BUN_TMPDIR)" BUN_INSTALL="$(BUN_INSTALL)" wails dev

install:
	GOCACHE="$(GOCACHE)" go build -buildvcs=false -o bin/nv-vcam ./cmd/nv-vcam
	install -d "$(HOME)/.local/bin"
	install -m 0755 bin/nv-vcam "$(HOME)/.local/bin/nv-vcam"
	@echo "installed CLI to $(HOME)/.local/bin/nv-vcam"
	@echo "run 'nv-vcam service install --enable --start' or use the app Service tab to install/start the user service"

uninstall:
	-systemctl --user stop nv-vcam.service
	-systemctl --user disable nv-vcam.service
	rm -rf "$(HOME)/.config/nv-vcam"
	rm -f "$(HOME)/.config/systemd/user/nv-vcam.service"
	rm -f "$(HOME)/.local/bin/nv-vcam"
	-sudo rm -f /etc/modprobe.d/nv-vcam-v4l2loopback.conf
	-systemctl --user daemon-reload

test:
	GOCACHE="$(GOCACHE)" go test ./...

check:
	GOCACHE="$(GOCACHE)" go test ./...
	cd app/frontend && BUN_TMPDIR="$(BUN_TMPDIR)" BUN_INSTALL="$(BUN_INSTALL)" bun run check

clean:
	rm -rf bin
