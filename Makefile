SHELL := /usr/bin/env bash

GOCACHE ?= /tmp/nv-x-go-cache
BUN_TMPDIR ?= /tmp/nv-x-bun-tmp
BUN_INSTALL ?= /tmp/nv-x-bun-install
MAXINE_SDK ?= /usr/local/VideoFX
AFX_SDK ?= /usr/local/AudioFX
CC ?= gcc
CXX ?= g++
GO_PACKAGES := ./app ./cmd/nv-x ./internal/...

MAXINE_RPATH := -Wl,-rpath,$(MAXINE_SDK)/lib -Wl,-rpath,$(MAXINE_SDK)/external/cuda/lib -Wl,-rpath,$(MAXINE_SDK)/external/tensorrt/lib -Wl,-rpath,$(MAXINE_SDK)/features/nvvfxgreenscreen/lib -Wl,-rpath,$(MAXINE_SDK)/features/nvvfxbackgroundblur/lib
AFX_RPATH := -Wl,-rpath,$(AFX_SDK)/nvafx/lib -Wl,-rpath,$(AFX_SDK)/external/cuda/lib -Wl,-rpath,$(AFX_SDK)/features/dereverb_denoiser/lib -Wl,-rpath,$(AFX_SDK)/features/studio_voice/lib

.PHONY: build dev package desktop desktop-clean-local install install-desktop-files uninstall test check clean

build: bin/nv-x-video bin/nv-x-audio bin/nv-x-os-release-shim.so
	rm -f bin/nv-x app/build/bin/nv-x app/build/bin/nv-x-gui
	GOCACHE="$(GOCACHE)" go build -buildvcs=false -o bin/nv-x ./cmd/nv-x
	cd app/frontend && BUN_TMPDIR="$(BUN_TMPDIR)" BUN_INSTALL="$(BUN_INSTALL)" bun run build
	cd app && GOCACHE="$(GOCACHE)" BUN_TMPDIR="$(BUN_TMPDIR)" BUN_INSTALL="$(BUN_INSTALL)" wails build

bin/nv-x-video: cmd/nv-x-video/main.cpp
	install -d bin
	$(CXX) -std=c++17 -O2 \
		-I"$(MAXINE_SDK)/include" \
		-I"$(MAXINE_SDK)/features/nvvfxgreenscreen/include" \
		-I"$(MAXINE_SDK)/features/nvvfxbackgroundblur/include" \
		-L"$(MAXINE_SDK)/lib" \
		$(MAXINE_RPATH) \
		-o $@ $< -lVideoFX -lNVCVImage

bin/nv-x-audio: cmd/nv-x-audio/main.cpp
	install -d bin
	$(CXX) -std=c++20 -O2 -pthread \
		$$(pkg-config --cflags libpipewire-0.3) \
		-I"$(AFX_SDK)/nvafx/include" \
		-I"$(AFX_SDK)/features/dereverb_denoiser/include" \
		-I"$(AFX_SDK)/features/studio_voice/include" \
		-L"$(AFX_SDK)/nvafx/lib" \
		-L"$(AFX_SDK)/features/dereverb_denoiser/lib" \
		-L"$(AFX_SDK)/features/studio_voice/lib" \
		-Wl,--disable-new-dtags $(AFX_RPATH) -o $@ $< $$(pkg-config --libs libpipewire-0.3) \
		-lnv_audiofx

bin/nv-x-os-release-shim.so: internal/fx/maxine/os_release_shim.c
	install -d bin
	$(CC) -shared -fPIC -O2 -o $@ $< -ldl

dev:
	cd app && GOCACHE="$(GOCACHE)" BUN_TMPDIR="$(BUN_TMPDIR)" BUN_INSTALL="$(BUN_INSTALL)" wails dev

package:
	chmod -R u+rwX packaging/arch/pkg packaging/arch/src 2>/dev/null || true
	rm -rf packaging/arch/pkg packaging/arch/src
	cd packaging/arch && BUILDDIR=/tmp/nv-x-makepkg/build PKGDEST="$(CURDIR)/packaging/arch" SRCDEST=/tmp/nv-x-makepkg/src makepkg -f --nodeps

desktop-clean-local:
	-systemctl --user stop nv-vcam.service
	-systemctl --user disable nv-vcam.service
	rm -f "$(HOME)/.config/systemd/user/nv-vcam.service"
	rm -f "$(HOME)/.local/bin/nv-vcam" "$(HOME)/.local/bin/nv-vcam-gui" "$(HOME)/.local/bin/nv-vcam-maxine-helper"
	rm -f "$(HOME)/.local/share/applications/nv-vcam-gui.desktop"
	rm -f "$(HOME)/.local/bin/nv-x-gui"
	rm -f "$(HOME)/.local/bin/nv-x-gui-launcher"
	rm -f "$(HOME)/.local/share/applications/nv-x-gui.desktop"
	rm -f "$(HOME)/.local/share/icons/hicolor/256x256/apps/nv-x-gui.png"
	-systemctl --user daemon-reload

desktop: desktop-clean-local package
	pkg="$$(ls -t packaging/arch/nv-x-*.pkg.tar.zst | head -n 1)"; \
		sudo pacman -U "$$pkg"
	-command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$(HOME)/.local/share/applications"
	-command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -f -t "$(HOME)/.local/share/icons/hicolor"
	-command -v kbuildsycoca6 >/dev/null 2>&1 && kbuildsycoca6

install-desktop-files:
	install -d "$(HOME)/.local/bin" "$(HOME)/.local/share/applications" "$(HOME)/.local/share/icons/hicolor/256x256/apps"
	install -m 0755 app/build/bin/nv-x-gui "$(HOME)/.local/bin/nv-x-gui"
	rm -f "$(HOME)/.local/bin/nv-x-gui-launcher"
	sed -e 's|^Exec=.*|Exec=$(HOME)/.local/bin/nv-x-gui|' -e 's|^TryExec=.*|TryExec=$(HOME)/.local/bin/nv-x-gui|' app/build/linux/nv-x-gui.desktop > "$(HOME)/.local/share/applications/nv-x-gui.desktop"
	chmod 0644 "$(HOME)/.local/share/applications/nv-x-gui.desktop"
	install -m 0644 app/build/linux-icon.png "$(HOME)/.local/share/icons/hicolor/256x256/apps/nv-x-gui.png"
	@echo "installed GUI to $(HOME)/.local/bin/nv-x-gui"
	@echo "installed desktop launcher to $(HOME)/.local/share/applications/nv-x-gui.desktop"

install:
	$(MAKE) build
	install -d "$(HOME)/.local/bin" "$(HOME)/.local/lib/nv-x"
	install -m 0755 bin/nv-x "$(HOME)/.local/bin/nv-x"
	install -m 0755 bin/nv-x-video "$(HOME)/.local/bin/nv-x-video"
	install -m 0755 bin/nv-x-audio "$(HOME)/.local/bin/nv-x-audio"
	install -m 0755 bin/nv-x-os-release-shim.so "$(HOME)/.local/lib/nv-x/nv-x-os-release-shim.so"
	$(MAKE) install-desktop-files
	@echo "installed CLI to $(HOME)/.local/bin/nv-x"
	@echo "installed Maxine video and audio helpers to $(HOME)/.local/bin"
	@echo "installed Maxine OS shim to $(HOME)/.local/lib/nv-x/nv-x-os-release-shim.so"
	@echo "make sure $(HOME)/.local/bin is on PATH, then run 'nv-x setup'"

uninstall:
	-systemctl --user stop nv-x.service
	-systemctl --user disable nv-x.service
	rm -rf "$(HOME)/.config/nv-x"
	rm -f "$(HOME)/.config/systemd/user/nv-x.service"
	rm -f "$(HOME)/.local/bin/nv-x"
	rm -f "$(HOME)/.local/bin/nv-x-gui"
	rm -f "$(HOME)/.local/bin/nv-x-gui-launcher"
	rm -f "$(HOME)/.local/bin/nv-x-video"
	rm -f "$(HOME)/.local/bin/nv-x-audio"
	rm -f "$(HOME)/.local/lib/nv-x/nv-x-os-release-shim.so"
	rm -f "$(HOME)/.local/share/applications/nv-x.desktop"
	rm -f "$(HOME)/.local/share/icons/hicolor/256x256/apps/nv-x.png"
	rm -f "$(HOME)/.local/share/applications/nv-x-gui.desktop"
	rm -f "$(HOME)/.local/share/icons/hicolor/256x256/apps/nv-x-gui.png"
	-sudo rm -f /etc/modprobe.d/nv-x-v4l2loopback.conf
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
