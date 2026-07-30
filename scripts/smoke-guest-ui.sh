#!/usr/bin/env bash
# On-demand end-to-end check of the GUEST PAGE's call experience — the half of
# the guest path that scripts/smoke-guest.sh proves nothing about, because it
# only asks "did bytes flow". This one asks "is the guest a real client":
#
#   • a shared screen takes the stage (theater), and you can switch and get out
#   • the guest sees their OWN screen while sharing it (real frames, not a black
#     rectangle) — the thing that was simply missing
#   • per-stream sound isolation: the share you're watching plays at full gain
#     while the other one ducks, and VOICE is never touched
#   • device pickers actually enumerate, and switching the mic mid-call neither
#     drops the call nor stops the audio
#   • the knock at a locked door renders as a state, not an eternal "Connecting…"
#   • the same on a 390x844 phone
#
# Requirements: node, npm, chromium (CHROMIUM=... to override), and a
# playwright-core install (PLAYWRIGHT_CORE=<path>, see smoke-guest.mjs).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# The shared 8961-8990 block, scanned for whatever is free: the other guest
# smoke scripts live in here too and may be running.
port_free() { ! (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }
PORTS=()
for p in $(seq 8961 8990); do
  port_free "$p" && PORTS+=("$p")
  [ "${#PORTS[@]}" -ge 3 ] && break
done
[ "${#PORTS[@]}" -ge 3 ] || { echo "FAIL: no free ports in 8961-8990" >&2; exit 1; }
RDV_P2P="${PORTS[0]}" GATEWAY="${PORTS[1]}" WEB="${PORTS[2]}"

TMP="$(mktemp -d /tmp/concord-guest-ui.XXXXXX)"
PIDS=()
STATUS=1
cleanup() {
  for pid in "${PIDS[@]:-}"; do kill "$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
  if [ "$STATUS" -eq 0 ] && [ -z "${CONCORD_SMOKE_KEEP:-}" ]; then
    rm -rf "$TMP"
  else
    echo "logs + screenshots kept in $TMP" >&2
  fi
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

rpc() { curl -sf -X POST "http://127.0.0.1:$WEB/rpc" -H 'Content-Type: application/json' -d "$1"; }
jget() { node -e 'const r=JSON.parse(process.argv[1]);if(r.error){console.error(r.error);process.exit(1)}process.stdout.write(String(eval("r.result"+process.argv[2])??""))' "$1" "$2"; }

PASSPHRASE="guest-ui-pass"
echo "== onboard member + start meeting"
LOGIN="$(rpc "{\"method\":\"Login\",\"args\":[\"$PASSPHRASE\"]}")"
jget "$LOGIN" '' >/dev/null || { echo "FAIL: Login: $LOGIN" >&2; exit 1; }
MEETING="$(rpc '{"method":"StartMeeting","args":[]}')"
GUILD_ID="$(jget "$MEETING" '.guild.id')" || { echo "FAIL: StartMeeting: $MEETING" >&2; exit 1; }
CHANNEL_ID="$(jget "$MEETING" '.guild.channels[0].id')"
LINK_RESP="$(rpc "{\"method\":\"CreateGuestLink\",\"args\":[\"$GUILD_ID\",24]}")"
GUEST_LINK="$(jget "$LINK_RESP" '')" || { echo "FAIL: CreateGuestLink: $LINK_RESP" >&2; exit 1; }
echo "   guest link: $GUEST_LINK"

echo "== drive the browsers"
MEMBER_URL="http://127.0.0.1:$WEB" \
  GUEST_LINK="$GUEST_LINK" \
  CHANNEL_ID="$CHANNEL_ID" \
  PASSPHRASE="$PASSPHRASE" \
  OUT_DIR="$TMP" \
  node "$ROOT/scripts/smoke-guest-ui.mjs"

STATUS=0
