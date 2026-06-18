SHELL := /usr/bin/env bash

GOCACHE ?= /tmp/nv-vcam-go-cache
BUN_TMPDIR ?= /tmp/nv-vcam-bun-tmp
BUN_INSTALL ?= /tmp/nv-vcam-bun-install
MAXINE_SDK ?= /usr/local/VideoFX
CC ?= gcc
CXX ?= g++
GO_PACKAGES := ./app ./cmd/nv-vcam ./internal/...

MAXINE_RPATH := -Wl,-rpath,$(MAXINE_SDK)/lib -Wl,-rpath,$(MAXINE_SDK)/external/cuda/lib -Wl,-rpath,$(MAXINE_SDK)/external/tensorrt/lib -Wl,-rpath,$(MAXINE_SDK)/features/nvvfxgreenscreen/lib -Wl,-rpath,$(MAXINE_SDK)/features/nvvfxbackgroundblur/lib -Wl,-rpath,$(MAXINE_SDK)/features/nvvfxdenoising/lib

.PHONY: build dev install uninstall test check clean

build: bin/nv-vcam-maxine-helper bin/nv-vcam-os-release-shim.so
	GOCACHE="$(GOCACHE)" go build -buildvcs=false -o bin/nv-vcam ./cmd/nv-vcam
	cd app/frontend && BUN_TMPDIR="$(BUN_TMPDIR)" BUN_INSTALL="$(BUN_INSTALL)" bun run build
	cd app && GOCACHE="$(GOCACHE)" BUN_TMPDIR="$(BUN_TMPDIR)" BUN_INSTALL="$(BUN_INSTALL)" wails build

bin/nv-vcam-maxine-helper: cmd/nv-vcam-maxine-helper/main.cpp
	install -d bin
	$(CXX) -std=c++17 -O2 \
		-I"$(MAXINE_SDK)/include" \
		-I"$(MAXINE_SDK)/features/nvvfxgreenscreen/include" \
		-I"$(MAXINE_SDK)/features/nvvfxbackgroundblur/include" \
		-L"$(MAXINE_SDK)/lib" \
		$(MAXINE_RPATH) \
		-o $@ $< -lVideoFX -lNVCVImage

bin/nv-vcam-os-release-shim.so: internal/fx/maxine/os_release_shim.c
	install -d bin
	$(CC) -shared -fPIC -O2 -o $@ $< -ldl

dev:
	cd app && GOCACHE="$(GOCACHE)" BUN_TMPDIR="$(BUN_TMPDIR)" BUN_INSTALL="$(BUN_INSTALL)" wails dev

install:
	$(MAKE) bin/nv-vcam-maxine-helper bin/nv-vcam-os-release-shim.so
	GOCACHE="$(GOCACHE)" go build -buildvcs=false -o bin/nv-vcam ./cmd/nv-vcam
	install -d "$(HOME)/.local/bin" "$(HOME)/.local/lib/nv-vcam"
	install -m 0755 bin/nv-vcam "$(HOME)/.local/bin/nv-vcam"
	install -m 0755 bin/nv-vcam-maxine-helper "$(HOME)/.local/bin/nv-vcam-maxine-helper"
	install -m 0755 bin/nv-vcam-os-release-shim.so "$(HOME)/.local/lib/nv-vcam/nv-vcam-os-release-shim.so"
	@echo "installed CLI to $(HOME)/.local/bin/nv-vcam"
	@echo "installed Maxine helper to $(HOME)/.local/bin/nv-vcam-maxine-helper"
	@echo "run 'nv-vcam service install --enable --start' or use the app Service tab to install/start the user service"

uninstall:
	-systemctl --user stop nv-vcam.service
	-systemctl --user disable nv-vcam.service
	rm -rf "$(HOME)/.config/nv-vcam"
	rm -f "$(HOME)/.config/systemd/user/nv-vcam.service"
	rm -f "$(HOME)/.local/bin/nv-vcam"
	rm -f "$(HOME)/.local/bin/nv-vcam-maxine-helper"
	rm -f "$(HOME)/.local/lib/nv-vcam/nv-vcam-os-release-shim.so"
	-sudo rm -f /etc/modprobe.d/nv-vcam-v4l2loopback.conf
	-systemctl --user daemon-reload

test:
	GOCACHE="$(GOCACHE)" go test $(GO_PACKAGES)

check:
	GOCACHE="$(GOCACHE)" go test $(GO_PACKAGES)
	cd app/frontend && BUN_TMPDIR="$(BUN_TMPDIR)" BUN_INSTALL="$(BUN_INSTALL)" bun run check

clean:
	rm -rf bin
