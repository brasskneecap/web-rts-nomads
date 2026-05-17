# Nomads desktop build pipeline.
#
# Steps (per §18 task 18.1):
#   1. Build the Vue SPA (npm run build → client/src/game-portal/dist/)
#   2. Stage dist/ into server/cmd/api/dist/ (Go //go:embed cannot reach
#      files outside the server module)
#   3. Build the Go server with the embed_spa tag, output sidecar binary
#      into desktop/src-tauri/binaries/nomads-server-<target-triple>.<ext>
#   4. Run cargo tauri build to produce the platform installer
#
# Cross-platform notes:
#   - Make is available on Windows via Git Bash; PowerShell users can also
#     run the steps individually as documented in desktop/README.md.
#   - Sidecar binary naming follows Tauri's <name>-<target-triple>.<ext>
#     convention; this Makefile auto-detects the current rustc host triple.
#   - `make package` is the end-to-end target; use individual sub-targets
#     for partial rebuilds.

SHELL := /usr/bin/env bash
RUSTC_HOST := $(shell rustc -vV | sed -n 's/host: //p')
ifeq ($(OS),Windows_NT)
EXE := .exe
else
EXE :=
endif

SPA_DIR  := client/src/game-portal
SPA_DIST := $(SPA_DIR)/dist
EMBED_DIST := server/cmd/api/dist
SIDECAR_DIR := desktop/src-tauri/binaries
SIDECAR_BIN := $(SIDECAR_DIR)/nomads-server-$(RUSTC_HOST)$(EXE)

# Version is injected into both the SPA (via Vite define, §15.8) and the Go
# binary (via -ldflags -X main.version). Set NOMADS_VERSION in CI / release
# tags to override the git-SHA default.
ifdef NOMADS_VERSION
VERSION := $(NOMADS_VERSION)
else
VERSION := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
endif

.PHONY: all package spa stage-dist server sidecar shell test test-go test-rust clean help

all: help

help:
	@echo "Targets:"
	@echo "  make spa         — npm run build (Vue SPA)"
	@echo "  make stage-dist  — copy SPA dist into server module"
	@echo "  make server      — build Go server (no embed)"
	@echo "  make sidecar     — go build -tags embed_spa into desktop sidecar slot"
	@echo "  make shell       — cargo tauri build (requires sidecar staged)"
	@echo "  make package     — full pipeline: spa → stage-dist → sidecar → shell"
	@echo "  make test        — run all Go + Rust tests"
	@echo "  make clean       — remove built artifacts"
	@echo ""
	@echo "Version: $(VERSION) (override with NOMADS_VERSION=...)"
	@echo "Host triple: $(RUSTC_HOST)"

# --- Phase 1 pipeline --------------------------------------------------------

spa:
	cd $(SPA_DIR) && NOMADS_VERSION=$(VERSION) npm run build

stage-dist: spa
	rm -rf $(EMBED_DIST)
	cp -r $(SPA_DIST) $(EMBED_DIST)

server:
	cd server && go build -ldflags "-X main.version=$(VERSION)" -o ../bin/api$(EXE) ./cmd/api

sidecar: stage-dist
	mkdir -p $(SIDECAR_DIR)
	cd server && go build -tags embed_spa -ldflags "-X main.version=$(VERSION)" -o "../$(SIDECAR_BIN)" ./cmd/api

shell: sidecar
	cd desktop/src-tauri && cargo tauri build

package: shell
	@echo ""
	@echo "Packaging complete. Artefacts:"
	@find desktop/src-tauri/target -maxdepth 5 -type f \( -name "*.msi" -o -name "*.dmg" -o -name "*.AppImage" -o -name "*.deb" \) 2>/dev/null

# --- Tests -------------------------------------------------------------------

test: test-go test-rust

test-go:
	cd server && go test -count=1 ./...

test-rust:
	cd desktop/src-tauri && cargo test --lib

# --- Cleanup -----------------------------------------------------------------

clean:
	rm -rf bin
	rm -rf $(EMBED_DIST)
	rm -rf $(SIDECAR_DIR)
	rm -rf desktop/src-tauri/target
	rm -rf $(SPA_DIST)
