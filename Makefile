SHELL := /usr/bin/env bash

GOCACHE ?= /tmp/nv-vcam-go-cache
BUN_TMPDIR ?= /tmp/nv-vcam-bun-tmp
BUN_INSTALL ?= /tmp/nv-vcam-bun-install
MAXINE_SDK ?= /usr/local/VideoFX
CC ?= gcc
CXX ?= g++
GO_PACKAGES := ./app ./cmd/nv-vcam ./internal/...

MAXINE_RPATH := -Wl,-rpath,$(MAXINE_SDK)/lib -Wl,-rpath,$(MAXINE_SDK)/external/cuda/lib -Wl,-rpath,$(MAXINE_SDK)/external/tensorrt/lib -Wl,-rpath,$(MAXINE_SDK)/features/nvvfxgreenscreen/lib -Wl,-rpath,$(MAXINE_SDK)/features/nvvfxbackgroundblur/lib

.PHONY: build dev package desktop desktop-clean-local install install-desktop-files uninstall test check clean

build: bin/nv-vcam-maxine-helper bin/nv-vcam-os-release-shim.so
	rm -f bin/nv-vcam app/build/bin/nv-vcam app/build/bin/nv-vcam-gui
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

package:
	chmod -R u+rwX packaging/arch/pkg packaging/arch/src 2>/dev/null || true
	rm -rf packaging/arch/pkg packaging/arch/src
	cd packaging/arch && BUILDDIR=/tmp/nv-vcam-makepkg/build PKGDEST="$(CURDIR)/packaging/arch" SRCDEST=/tmp/nv-vcam-makepkg/src makepkg -f --nodeps

desktop-clean-local:
	rm -f "$(HOME)/.local/bin/nv-vcam-gui"
	rm -f "$(HOME)/.local/bin/nv-vcam-gui-launcher"
	rm -f "$(HOME)/.local/share/applications/nv-vcam-gui.desktop"
	rm -f "$(HOME)/.local/share/icons/hicolor/256x256/apps/nv-vcam-gui.png"

desktop: desktop-clean-local package
	pkg="$$(ls -t packaging/arch/nv-vcam-*.pkg.tar.zst | head -n 1)"; \
		sudo pacman -U "$$pkg"
	-command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$(HOME)/.local/share/applications"
	-command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -f -t "$(HOME)/.local/share/icons/hicolor"
	-command -v kbuildsycoca6 >/dev/null 2>&1 && kbuildsycoca6

install-desktop-files:
	install -d "$(HOME)/.local/bin" "$(HOME)/.local/share/applications" "$(HOME)/.local/share/icons/hicolor/256x256/apps"
	install -m 0755 app/build/bin/nv-vcam-gui "$(HOME)/.local/bin/nv-vcam-gui"
	rm -f "$(HOME)/.local/bin/nv-vcam-gui-launcher"
	sed -e 's|^Exec=.*|Exec=$(HOME)/.local/bin/nv-vcam-gui|' -e 's|^TryExec=.*|TryExec=$(HOME)/.local/bin/nv-vcam-gui|' app/build/linux/nv-vcam-gui.desktop > "$(HOME)/.local/share/applications/nv-vcam-gui.desktop"
	chmod 0644 "$(HOME)/.local/share/applications/nv-vcam-gui.desktop"
	install -m 0644 app/build/linux-icon.png "$(HOME)/.local/share/icons/hicolor/256x256/apps/nv-vcam-gui.png"
	@echo "installed GUI to $(HOME)/.local/bin/nv-vcam-gui"
	@echo "installed desktop launcher to $(HOME)/.local/share/applications/nv-vcam-gui.desktop"

install:
	$(MAKE) build
	install -d "$(HOME)/.local/bin" "$(HOME)/.local/lib/nv-vcam"
	install -m 0755 bin/nv-vcam "$(HOME)/.local/bin/nv-vcam"
	install -m 0755 bin/nv-vcam-maxine-helper "$(HOME)/.local/bin/nv-vcam-maxine-helper"
	install -m 0755 bin/nv-vcam-os-release-shim.so "$(HOME)/.local/lib/nv-vcam/nv-vcam-os-release-shim.so"
	$(MAKE) install-desktop-files
	@echo "installed CLI to $(HOME)/.local/bin/nv-vcam"
	@echo "installed Maxine helper to $(HOME)/.local/bin/nv-vcam-maxine-helper"
	@echo "installed Maxine OS shim to $(HOME)/.local/lib/nv-vcam/nv-vcam-os-release-shim.so"
	@echo "make sure $(HOME)/.local/bin is on PATH, then run 'nv-vcam setup'"

uninstall:
	-systemctl --user stop nv-vcam.service
	-systemctl --user disable nv-vcam.service
	rm -rf "$(HOME)/.config/nv-vcam"
	rm -f "$(HOME)/.config/systemd/user/nv-vcam.service"
	rm -f "$(HOME)/.local/bin/nv-vcam"
	rm -f "$(HOME)/.local/bin/nv-vcam-gui"
	rm -f "$(HOME)/.local/bin/nv-vcam-gui-launcher"
	rm -f "$(HOME)/.local/bin/nv-vcam-maxine-helper"
	rm -f "$(HOME)/.local/lib/nv-vcam/nv-vcam-os-release-shim.so"
	rm -f "$(HOME)/.local/share/applications/nv-vcam.desktop"
	rm -f "$(HOME)/.local/share/icons/hicolor/256x256/apps/nv-vcam.png"
	rm -f "$(HOME)/.local/share/applications/nv-vcam-gui.desktop"
	rm -f "$(HOME)/.local/share/icons/hicolor/256x256/apps/nv-vcam-gui.png"
	-sudo rm -f /etc/modprobe.d/nv-vcam-v4l2loopback.conf
	-systemctl --user daemon-reload
	@if [ "$(REMOVE_MAXINE)" = "1" ]; then \
		echo "removing Maxine SDK at $(MAXINE_SDK)"; \
		sudo rm -rf "$(MAXINE_SDK)"; \
	else \
		echo "left Maxine SDK untouched at $(MAXINE_SDK)"; \
		echo "run 'make uninstall REMOVE_MAXINE=1' to remove it too"; \
	fi

test:
	GOCACHE="$(GOCACHE)" go test $(GO_PACKAGES)

check:
	GOCACHE="$(GOCACHE)" go test $(GO_PACKAGES)
	cd app/frontend && BUN_TMPDIR="$(BUN_TMPDIR)" BUN_INSTALL="$(BUN_INSTALL)" bun run check

clean:
	rm -rf bin
