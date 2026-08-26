# Concord build targets.
#
# Two front ends share one backend:
#   - `make gui`  native desktop window (needs webkit2gtk-4.1)
#   - `make web`  browser-served app (no system dependencies)

GUI_TAGS := wails desktop production webkit2_41
N ?= 2

# Stamped into the binary as internal/version.Version (drives the in-app update check).
# CI passes the git tag (e.g. VERSION=v0.4.9); local builds stay "dev" (no nag).
VERSION ?= dev

.PHONY: gui gui-dev web cli rendezvous frontend test race fmt clean release-keygen \
        peers rendezvous-run dev-clean help release icons banner native \
        android-core ios-core android-app ios-app

# gomobile bind flags shared by both mobile cores.
#   -checklinkname=0: github.com/wlynxg/anet (libp2p's Android net shim) uses
#    go:linkname into net internals that Go 1.23+ rejects by default.
#   -extldflags max-page-size=16384: Android 15 ships 16KB memory pages on new
#    64-bit devices and Play refuses uploads at targetSdk 35+ whose native
#    libraries are laid out for 4KB pages. The external linker (cgo/c-shared
#    always uses one) decides the ELF LOAD alignment, so the flag has to reach
#    the NDK's lld rather than the Go linker. Verify with
#    `llvm-readelf -lW libgojni.so`: every LOAD must read 0x4000, not 0x1000.
#    Single quotes because this expands inside a double-quoted -ldflags string.
MOBILE_PAGE_ALIGN := -extldflags '-Wl,-z,max-page-size=16384,-z,common-page-size=16384'
MOBILE_LDFLAGS := -checklinkname=0 -s -w $(MOBILE_PAGE_ALIGN) -X github.com/ZahakJ/concord/internal/version.Version=$(VERSION)
ANDROID_API    := 26
IOS_VERSION    := 15.0

# Every shippable artifact goes through here: the desktop window, the web
# binaries, the .aab and the .apk all depend on `frontend`, and all of them
# embed or copy frontend/dist wholesale.
#
# Which is why the meme pack is removed from dist right after the build. Those
# 101 images are third-party photographs and film stills with identifiable
# rightsholders (frontend/public/memes/README.md sets out the position); they
# are gitignored so the repository does not redistribute them, but a gitignore
# says nothing about a binary built on a machine where prep-memes.mjs has been
# run — which is every machine that has ever cut a release. The README claimed
# they were "in neither the repository nor any release" and only the first half
# was true.
#
# CONCORD_MEMES=1 keeps them, for a build going nowhere. `make peers` and the
# vite dev server are unaffected: neither goes through this target, and the dev
# server reads public/ directly.
frontend:
	cd frontend && npm install && npm run build
	@if [ "$(CONCORD_MEMES)" = "1" ]; then \
		echo "frontend: keeping the bundled meme pack (CONCORD_MEMES=1) — do not ship this build"; \
	elif [ -d frontend/dist/memes ]; then \
		rm -rf frontend/dist/memes; \
		echo "frontend: pruned the bundled meme pack from dist/"; \
	fi

# Create the release signing keypair. Run ONCE, on the machine that publishes
# releases; the private half stays there (back it up offline), the public half
# gets committed to internal/bridge/release-pubkey.txt so every build carries it.
release-keygen:
	go run ./cmd/releasekey gen

# README banner and social-preview card, rendered from docs/media/*.svg rather
# than hand-exported, so a wording change is a text edit and not a trip through
# an image editor.
#
# The wordmark is set in Inter, which the app already bundles — but as woff2,
# which neither fontconfig nor rsvg will read. So it is decompressed into a
# throwaway font directory that only this command can see, keeping the banner in
# the app's own typeface without installing anything on the machine. Needs
# rsvg-convert and woff2_decompress.
banner:
	@tmp=$$(mktemp -d); mkdir -p "$$tmp/fonts"; \
	cp frontend/public/fonts/inter-latin-300700.woff2 "$$tmp/fonts/"; \
	( cd "$$tmp/fonts" && woff2_decompress inter-latin-300700.woff2 >/dev/null ); \
	XDG_DATA_HOME="$$tmp" fc-cache -f "$$tmp/fonts" >/dev/null 2>&1; \
	for v in banner banner-light; do \
		XDG_DATA_HOME="$$tmp" rsvg-convert -w 2000 -o docs/media/$$v.png docs/media/$$v.svg || exit 1; \
	done; \
	XDG_DATA_HOME="$$tmp" rsvg-convert -w 1280 -o docs/media/social-preview.png docs/media/social-preview.svg || exit 1; \
	rm -rf "$$tmp"
	@echo "banner + social preview regenerated under docs/media/"

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

# Mobile core: the whole Go backend (identity, MLS, libp2p, store, bridge)
# bound for the native shells under apps/mobile/. Needs gomobile on PATH
# (go install golang.org/x/mobile/cmd/gomobile@latest) and, for Android,
# ANDROID_HOME + an NDK. iOS requires building on macOS with Xcode.
# The x86 ABIs need a one-file patch on modernc.org/libc: Android's seccomp
# filter denies the legacy x86_64 path syscalls it uses, so the process dies
# with SIGSYS on emulators the moment sqlite opens. The patched file lives in
# third_party/_libc-overlay/ (committed); the target materializes a local module
# fork under build/libc-fork/ (gitignored) and builds against it via an
# alternate -modfile, leaving go.mod and all desktop builds untouched. arm64
# (real devices) never compiles the patched file.
LIBC_VERSION = $(shell go list -m -f '{{.Version}}' modernc.org/libc)

android-core:
	mkdir -p apps/mobile/android/app/libs build/libc-fork
	go mod download modernc.org/libc
	rsync -a --delete --chmod=u+w "$$(go env GOMODCACHE)/modernc.org/libc@$(LIBC_VERSION)/" build/libc-fork/
	cp third_party/_libc-overlay/syscall_musl.go build/libc-fork/syscall_musl.go
	# The replace lives in go.mod only while the bind runs (gomobile spawns its
	# own temp work modules, so a GOFLAGS=-modfile override would poison them);
	# it is dropped again even when the bind fails.
	go mod edit -replace modernc.org/libc=$(CURDIR)/build/libc-fork
	gomobile bind -target=android -androidapi $(ANDROID_API) -trimpath \
		-ldflags "$(MOBILE_LDFLAGS)" \
		-o apps/mobile/android/app/libs/concord.aar ./mobile; \
	status=$$?; go mod edit -dropreplace modernc.org/libc; exit $$status
	@echo "built apps/mobile/android/app/libs/concord.aar"

ios-core:
	mkdir -p apps/mobile/ios/Frameworks
	gomobile bind -target=ios,iossimulator -iosversion $(IOS_VERSION) -trimpath \
		-ldflags "$(MOBILE_LDFLAGS)" \
		-o apps/mobile/ios/Frameworks/Concord.xcframework ./mobile
	@echo "built apps/mobile/ios/Frameworks/Concord.xcframework"

# Installable mobile apps. Each rebuilds the web UI, the gomobile core, syncs
# Capacitor, then invokes the platform build. Android needs a JDK 17–21 on
# JAVA_HOME (see apps/mobile/android/gradle.properties); iOS needs macOS + Xcode.
# Release signing is supplied out-of-band (keystore / provisioning profile); an
# unconfigured build still produces an unsigned artifact for testing.
# MOBILE_VERSION_NAME strips a leading "v" from VERSION (v0.6.0 -> 0.6.0) for
# the store-required numeric versionName; MOBILE_VERSION_CODE is a monotonic int
# the caller must supply. It has no useful default: Play rejects any upload
# whose code is not strictly greater than the last one, and the old default of 1
# meant a release built without the variable produced a bundle that could only
# ever be uploaded once — silently, with nothing in the build log to say so.
# The convention is minor*100 + patch (v0.57.0 -> 5700).
MOBILE_VERSION_NAME := $(patsubst v%,%,$(VERSION))
MOBILE_VERSION_CODE ?= 0

# Windows version resource, stamped from the same tag (see the `release` target).
# WIN_PATCH is empty unless VERSION splits into three dot-separated parts, so an
# unstamped local build (VERSION=dev) passes no flags and goversioninfo falls
# back to whatever build/versioninfo.json carries.
WIN_VERSION := $(patsubst v%,%,$(VERSION))
WIN_MAJOR   := $(word 1,$(subst ., ,$(WIN_VERSION)))
WIN_MINOR   := $(word 2,$(subst ., ,$(WIN_VERSION)))
WIN_PATCH   := $(word 3,$(subst ., ,$(WIN_VERSION)))
WIN_STAMP    = $(if $(WIN_PATCH),\
	-ver-major $(WIN_MAJOR) -ver-minor $(WIN_MINOR) -ver-patch $(WIN_PATCH) \
	-product-ver-major $(WIN_MAJOR) -product-ver-minor $(WIN_MINOR) -product-ver-patch $(WIN_PATCH) \
	-file-version "$(WIN_VERSION).0" -product-version "$(WIN_VERSION)")

android-app: frontend android-core
	@case "$(MOBILE_VERSION_CODE)" in \
		''|*[!0-9]*) ok=no ;; \
		*) [ "$(MOBILE_VERSION_CODE)" -gt 1 ] && ok=yes || ok=no ;; \
	esac; \
	if [ "$$ok" != yes ]; then \
		echo "make: MOBILE_VERSION_CODE='$(MOBILE_VERSION_CODE)' is not a usable release code." >&2; \
		echo "      Pass a monotonic integer, by convention minor*100+patch:" >&2; \
		echo "      make android-app VERSION=$(VERSION) MOBILE_VERSION_CODE=5700" >&2; \
		exit 1; \
	fi
	cd apps/mobile && npm ci && npx cap sync android
	cd apps/mobile/android && ./gradlew bundleRelease \
		-PconcordVersionName=$(MOBILE_VERSION_NAME) \
		-PconcordVersionCode=$(MOBILE_VERSION_CODE)
	@echo "built apps/mobile/android/app/build/outputs/bundle/release/app-release.aab"
	# Sideloadable APK for direct distribution (GitHub Release download).
	# arm64-only: every phone since ~2017; halves the size vs universal.
	cd apps/mobile/android && ./gradlew assembleRelease \
		-PconcordAbi=arm64-v8a \
		-PconcordVersionName=$(MOBILE_VERSION_NAME) \
		-PconcordVersionCode=$(MOBILE_VERSION_CODE)
	cp apps/mobile/android/app/build/outputs/apk/release/app-release.apk \
		apps/mobile/android/app/build/outputs/apk/release/concord-$(MOBILE_VERSION_NAME)-android.apk
	@echo "built apps/mobile/android/app/build/outputs/apk/release/concord-$(MOBILE_VERSION_NAME)-android.apk"

ios-app: frontend ios-core
	cd apps/mobile && npm ci && npx cap sync ios
	cd apps/mobile/ios/App && xcodebuild -project App.xcodeproj -scheme App \
		-configuration Release -archivePath build/App.xcarchive archive
	@echo "archived apps/mobile/ios/App/build/App.xcarchive"

# Self-contained WEB release binaries (UI embedded, pure Go, no dependencies).
# Friends download ONE file for their OS, run it, and the browser opens.
#
# This builds the web track ONLY, into dist-release/ — it does NOT build the
# desktop apps or publish anything. Real releases go through
# scripts/publish-release.sh, which calls this target and then adds the native
# desktop apps, SHA256SUMS and the GitHub Release. See README "Cutting a
# release". Use this target only for a quick local smoke test of the binaries.
release: frontend
	rm -rf dist-release && mkdir -p dist-release
	# The version is stamped into each filename so downloaded builds are visibly
	# distinct across releases; the updater matches assets by OS keyword.
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "-s -w -X github.com/ZahakJ/concord/internal/version.Version=$(VERSION)" -o dist-release/concord-linux-amd64-$(VERSION) .
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags "-s -w -X github.com/ZahakJ/concord/internal/version.Version=$(VERSION)" -o dist-release/concord-linux-arm64-$(VERSION) .
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "-s -w -X github.com/ZahakJ/concord/internal/version.Version=$(VERSION)" -o dist-release/concord-macos-arm64-$(VERSION) .
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags "-s -w -X github.com/ZahakJ/concord/internal/version.Version=$(VERSION)" -o dist-release/concord-macos-intel-$(VERSION) .
	# Windows: embed version info + manifest (goversioninfo) and DON'T strip
	# symbols — both markedly reduce Defender false positives on unsigned exes.
	# The version is stamped from the tag rather than read out of the JSON: the
	# committed numbers rot silently otherwise, and once did — v0.55.1 shipped an
	# exe whose Properties pane read 0.31.0. See WIN_* above for the dev case.
	go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.7.0 -64 $(WIN_STAMP) -o resource_windows_amd64.syso build/versioninfo.json
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-X github.com/ZahakJ/concord/internal/version.Version=$(VERSION)" -o dist-release/concord-windows-$(VERSION).exe .
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
	@echo "  make icons|banner  regenerate the app icons / README artwork"
	@echo
	@echo "Releases are built locally: 'scripts/publish-release.sh vX.Y.Z' builds"
	@echo "the web binaries + desktop apps and publishes the GitHub Release."
	@echo "See docs/RELEASING.md."
