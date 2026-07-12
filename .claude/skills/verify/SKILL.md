---
name: verify
description: Build, launch, and drive Concord to verify changes end-to-end (multi-peer, RPC, and headless-browser flows).
---

# Verifying Concord changes

## Build + launch isolated peers

```bash
npm --prefix frontend run build        # UI must be rebuilt before go build (embedded)
go build -o .dev/concord-web .
CONCORD_HOME=.dev/peer1 CONCORD_WEB_ADDR=127.0.0.1:8801 CONCORD_NO_OPEN=1 .dev/concord-web &
CONCORD_HOME=.dev/peer2 CONCORD_WEB_ADDR=127.0.0.1:8802 CONCORD_NO_OPEN=1 .dev/concord-web &
```

Peers discover each other over mDNS on loopback within ~2s. `scripts/local-peers.sh N` does the same. Clean slate: `rm -rf .dev/peerN`.

## Drive the RPC surface (same API the UI speaks)

```bash
rpc() { curl -s -X POST "http://127.0.0.1:$1/rpc" -H 'Content-Type: application/json' -d "$2"; }
rpc 8801 '{"method":"Login","args":["any-passphrase"]}'   # first call creates the identity
rpc 8801 '{"method":"Guilds","args":[]}'
```

Method names = `Dispatch` switch in `internal/bridge/bridge.go`. No Origin header needed (curl sends none; the CSRF guard only rejects foreign origins). Events: `curl -N http://127.0.0.1:8801/events` streams SSE (`message`, `guild-updated`, `read-state`, …).

Typical two-peer wiring: peer1 `CreateGuild` → `InviteCode` → peer2 `JoinViaInvite`; wait ~2s; fingerprints come from `Identity`/`Members`.

Gotcha: zsh reserves `GID` — don't use it as a shell var for guild IDs.

## Drive the real UI (pixels)

Plain `chromium --headless --screenshot` hangs: the SSE stream never lets the page settle. Use playwright-core against system Chromium instead (no browser download):

```bash
cd "$SCRATCHPAD" && mkdir drive && cd drive && npm init -y && npm i playwright-core
```

```js
import { chromium } from "playwright-core";
const browser = await chromium.launch({ executablePath: "/usr/bin/chromium", args: ["--no-sandbox"] });
```

Flow: goto 127.0.0.1:8801 → fill `input[placeholder*="assphrase"]` + Enter (backend session survives, but the UI always asks) → press Escape to clear stray modals. Member panel names are `.mname`; the profile popover is `[role="dialog"][aria-label*="profile"]`; clicking the guild-name header opens Guild settings (avoid). Composer file input: `input[type="file"]` + `setInputFiles` sends an image attachment.
