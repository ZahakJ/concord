# Concord build targets.
#
# Two front ends share one backend:
#   - `make gui`  native desktop window (needs webkit2gtk-4.1)
#   - `make web`  browser-served app (no system dependencies)

GUI_TAGS := wails desktop production webkit2_41
N ?= 2

.PHONY: gui gui-dev web cli rendezvous frontend test race fmt clean \
        peers rendezvous-run dev-clean help release

frontend:
	cd frontend && npm install && npm run build

# Native desktop app. Uses a direct tagged build (verified) rather than the
# wails CLI so the webkit2gtk-4.1 tag is always applied.
gui: frontend
	go build -tags "$(GUI_TAGS)" -o bin/concord-gui .
	@echo "built bin/concord-gui — run it to open the desktop app"

# Hot-reloading dev window (requires the wails CLI on PATH).
gui-dev:
	wails dev -tags "wails webkit2_41"

# Browser-served app: go run . then open http://127.0.0.1:8787
web: frontend
	go build -o bin/concord-web .
	@echo "built bin/concord-web — run it, then open http://127.0.0.1:8787"

# Self-contained release binaries (UI embedded, pure Go, no dependencies).
# Friends download ONE file for their OS, run it, and the browser opens.
# Upload the contents of dist-release/ to a GitHub Release.
release: frontend
	rm -rf dist-release && mkdir -p dist-release
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist-release/concord-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist-release/concord-linux-arm64 .
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist-release/concord-macos-arm64 .
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist-release/concord-macos-intel .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist-release/concord-windows.exe .
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
	@echo "  make test | race   run tests (optionally with the race detector)"
	@echo "  make dev-clean      delete local test data (.dev/)"
