# Concord build targets.
#
# Two front ends share one backend:
#   - `make gui`  native desktop window (needs webkit2gtk-4.1)
#   - `make web`  browser-served app (no system dependencies)

GUI_TAGS := wails desktop production webkit2_41
N ?= 2

# Stamped into the binary as main.version (drives the in-app update check).
# CI passes the git tag (e.g. VERSION=v0.4.9); local builds stay "dev" (no nag).
VERSION ?= dev

.PHONY: gui gui-dev web cli rendezvous frontend test race fmt clean \
        peers rendezvous-run dev-clean help release icons native

frontend:
	cd frontend && npm install && npm run build

# Regenerate app-icon assets from build/appicon.svg (needs ImageMagick).
# Produces the PNG (Wails/Linux), the multi-size Windows .ico, and macOS .icns.
icons:
	magick -background none -density 300 build/appicon.svg -resize 1024x1024 build/appicon.png
	mkdir -p build/windows build/darwin
	magick build/appicon.png -define icon:auto-resize=256,128,64,48,32,16 build/windows/icon.ico
	magick build/appicon.png -resize 512x512 build/darwin/icon.icns
	@echo "icons regenerated under build/"

# Native desktop app: a real branded window (Concorde logo), not a browser.
# Uses a direct tagged build (verified) rather than the wails CLI so the
# webkit2gtk-4.1 tag is always applied. Needs cgo + the system WebView.
gui: frontend
	go build -tags "$(GUI_TAGS)" -o bin/concord-gui .
	@echo "built bin/concord-gui — run it to open the desktop app"

# native is an alias for gui, kept distinct from `release` (the zero-dependency
# web binary). CI builds `native` per-OS to produce branded installers, while
# `release` stays the single-file cross-compiled download.
native: gui

# Hot-reloading dev window (requires the wails CLI on PATH).
gui-dev:
	wails dev -tags "wails webkit2_41"

# Browser-served app: go run . then open http://127.0.0.1:8787
web: frontend
	go build -o bin/concord-web .
	@echo "built bin/concord-web — run it, then open http://127.0.0.1:8787"

# Self-contained WEB release binaries (UI embedded, pure Go, no dependencies).
# Friends download ONE file for their OS, run it, and the browser opens.
#
# This builds the web track ONLY, into dist-release/ — it does NOT build the
# desktop apps or publish anything. Real releases are tag-driven: pushing a
# `v*` tag runs .github/workflows/release.yml, which builds this AND the native
# desktop apps (per-OS, via Wails) and attaches everything to the GitHub
# Release. See README §16 "Cutting a release". Use this target only for a quick
# local smoke test of the web binaries.
release: frontend
	rm -rf dist-release && mkdir -p dist-release
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o dist-release/concord-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o dist-release/concord-linux-arm64 .
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o dist-release/concord-macos-arm64 .
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o dist-release/concord-macos-intel .
	# Windows: embed version info + manifest (goversioninfo) and DON'T strip
	# symbols — both markedly reduce Defender false positives on unsigned exes.
	go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.7.0 -64 -o resource_windows_amd64.syso build/versioninfo.json
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-X main.version=$(VERSION)" -o dist-release/concord-windows.exe .
	@rm -f resource_windows_amd64.syso
	@echo && echo "Release binaries in dist-release/:" && ls -lh dist-release/

cli:
	go build -o bin/concord ./cmd/concord

rendezvous:
	go build -o bin/rendezvous ./cmd/rendezvous

# --- Local multi-peer testing ---------------------------------------------

# Launch N isolated browser peers (default 2) that discover each other on the
# LAN. Open the printed URLs in separate windows. Override count with N=3.
#   make peers          make peers N=3
peers:
	./scripts/local-peers.sh $(N)

# Run a local rendezvous/relay node with a stable identity, then point peers at
# the printed multiaddr via CONCORD_BOOTSTRAP to test internet-style discovery.
rendezvous-run:
	CONCORD_RELAY_SEED=$${CONCORD_RELAY_SEED:-00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff} \
		go run ./cmd/rendezvous

# Remove all local test data / binaries created by `make peers`.
dev-clean:
	rm -rf .dev

test:
	go test ./internal/... . -count=1

race:
	go test -race ./internal/... . -count=1

fmt:
	gofmt -w *.go internal cmd

clean:
	rm -rf bin frontend/dist

help:
	@echo "Concord make targets:"
	@echo "  make peers [N=3]   run N local browser peers (multi-peer testing)"
	@echo "  make gui           build the native desktop app (bin/concord-gui)"
	@echo "  make gui-dev       hot-reloading native dev window"
	@echo "  make web           build the browser-served app (bin/concord-web)"
	@echo "  make rendezvous-run run a local rendezvous/relay node"
	@echo "  make release       build web binaries locally (smoke test only)"
	@echo "  make test | race   run tests (optionally with the race detector)"
	@echo "  make dev-clean      delete local test data (.dev/)"
	@echo
	@echo "Releases are tag-driven: 'git push origin vX.Y.Z' builds the web"
	@echo "binaries + desktop apps and publishes them all to the GitHub Release."
	@echo "See README section 16 'Cutting a release'."
