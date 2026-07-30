#!/usr/bin/env bash
# On-demand end-to-end smoke test of the guest-meeting path: real rendezvous
# gateway, real member app, real Chromium on both sides of the meeting. This
# exists because the gateway↔guest-page framing once drifted and nothing
# noticed for weeks; the fast half of the guard runs in `go test ./...`
# (cmd/rendezvous/guest_smoke_test.go) — this script is the full-fidelity half
# that proves chat AND WebRTC media (audio + screen share, both directions)
# actually work through a browser.
#
# Requirements: node, npm, chromium (CHROMIUM=... to override), and a
# playwright-core install (PLAYWRIGHT_CORE=<path>, see smoke-guest.mjs).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# A dedicated port block (8961-8980) so this never collides with dev servers
# on the usual 8787/880x ports.
port_free() { ! (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }
PORTS=()
for p in $(seq 8961 8980); do
  port_free "$p" && PORTS+=("$p")
  [ "${#PORTS[@]}" -ge 3 ] && break
done
[ "${#PORTS[@]}" -ge 3 ] || { echo "FAIL: no free ports in 8961-8980" >&2; exit 1; }
RDV_P2P="${PORTS[0]}" GATEWAY="${PORTS[1]}" WEB="${PORTS[2]}"

TMP="$(mktemp -d /tmp/concord-guest-smoke.XXXXXX)"
PIDS=()
STATUS=1
cleanup() {
  for pid in "${PIDS[@]:-}"; do kill "$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
  # Keep the evidence (logs, screenshots) when something failed.
  if [ "$STATUS" -eq 0 ]; then rm -rf "$TMP"; else echo "logs kept in $TMP" >&2; fi
}
trap cleanup EXIT

echo "== build (UI + web binary + rendezvous)"
npm --prefix frontend run build >"$TMP/npm-build.log" 2>&1 || { echo "FAIL: frontend build (see $TMP/npm-build.log)" >&2; exit 1; }
go build -o "$TMP/concord-web" . || { echo "FAIL: go build ." >&2; exit 1; }
go build -o "$TMP/rendezvous" ./cmd/rendezvous || { echo "FAIL: go build ./cmd/rendezvous" >&2; exit 1; }

echo "== start rendezvous (p2p :$RDV_P2P, guest gateway :$GATEWAY)"
PORT="$RDV_P2P" CONCORD_GUEST_PORT="$GATEWAY" "$TMP/rendezvous" >"$TMP/rdv.log" 2>&1 &
PIDS+=($!)
PEER=""
for _ in $(seq 1 50); do
  PEER="$(sed -n 's/^PeerID: //p' "$TMP/rdv.log" | head -1)"
  [ -n "$PEER" ] && break
  sleep 0.2
done
[ -n "$PEER" ] || { echo "FAIL: rendezvous never printed its PeerID (see $TMP/rdv.log)" >&2; exit 1; }
BOOT="/ip4/127.0.0.1/tcp/$RDV_P2P/p2p/$PEER"

echo "== start member app (:$WEB, home $TMP/member-home)"
CONCORD_HOME="$TMP/member-home" \
  CONCORD_WEB_ADDR="127.0.0.1:$WEB" \
  CONCORD_NO_OPEN=1 \
  CONCORD_DISABLE_MDNS=1 \
  CONCORD_BOOTSTRAP="$BOOT" \
  CONCORD_GUEST_BASE="http://127.0.0.1:$GATEWAY" \
  "$TMP/concord-web" >"$TMP/app.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 1 50); do
  curl -sf "http://127.0.0.1:$WEB/" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -sf "http://127.0.0.1:$WEB/" >/dev/null || { echo "FAIL: member app never came up (see $TMP/app.log)" >&2; exit 1; }

# Onboard + mint the meeting over the same RPC the UI speaks. jq-free: node is
# already required for the browser driver.
rpc() { curl -sf -X POST "http://127.0.0.1:$WEB/rpc" -H 'Content-Type: application/json' -d "$1"; }
jget() { node -e 'const r=JSON.parse(process.argv[1]);if(r.error){console.error(r.error);process.exit(1)}process.stdout.write(String(eval("r.result"+process.argv[2])??""))' "$1" "$2"; }

PASSPHRASE="guest-smoke-pass"
echo "== onboard member + start meeting"
LOGIN="$(rpc "{\"method\":\"Login\",\"args\":[\"$PASSPHRASE\"]}")"
jget "$LOGIN" '' >/dev/null || { echo "FAIL: Login: $LOGIN" >&2; exit 1; }
MEETING="$(rpc '{"method":"StartMeeting","args":[]}')"
GUILD_ID="$(jget "$MEETING" '.guild.id')" || { echo "FAIL: StartMeeting: $MEETING" >&2; exit 1; }
CHANNEL_ID="$(jget "$MEETING" '.guild.channels[0].id')"
LINK_RESP="$(rpc "{\"method\":\"CreateGuestLink\",\"args\":[\"$GUILD_ID\"]}")"
GUEST_LINK="$(jget "$LINK_RESP" '')" || { echo "FAIL: CreateGuestLink: $LINK_RESP" >&2; exit 1; }
echo "   guest link: $GUEST_LINK"

echo "== drive both browsers"
MEMBER_URL="http://127.0.0.1:$WEB" \
  GUEST_LINK="$GUEST_LINK" \
  CHANNEL_ID="$CHANNEL_ID" \
  PASSPHRASE="$PASSPHRASE" \
  OUT_DIR="$TMP" \
  node "$ROOT/scripts/smoke-guest.mjs"

STATUS=0
