#!/usr/bin/env bash
# On-demand end-to-end smoke test of the LOCKED guest meeting: office hours.
# Same shape as scripts/smoke-guest.sh (real rendezvous gateway, real member
# app, real Chromium on both sides) but aimed at the two things that script
# cannot see: a guest held at the door of a locked meeting, and a guest link
# whose lifetime the host chose.
#
# What it proves, in one run:
#   • CreateGuestLink(guild, 168) sets a 7-day expiry on link AND room
#   • locking the call closes the guest door (the lock button exists in a
#     meeting at all — it used to be hidden there)
#   • a knocking guest receives ONLY a "waiting" frame: no welcome, no roster,
#     no chat, no signalling, no RTCPeerConnection
#   • the host sees who is knocking, by the name the guest typed
#   • Admit → welcome → chat + audio actually flow
#   • Refuse and kick both end with a reason the guest can read
#
# Requirements: node, npm, chromium (CHROMIUM=... to override), and a
# playwright-core install (PLAYWRIGHT_CORE=<path>, see smoke-guest.mjs).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# A dedicated port block (8981-8990) so this never collides with the plain guest
# smoke test (8961-8980) or a dev server.
port_free() { ! (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }
PORTS=()
for p in $(seq 8981 8990); do
  port_free "$p" && PORTS+=("$p")
  [ "${#PORTS[@]}" -ge 3 ] && break
done
[ "${#PORTS[@]}" -ge 3 ] || { echo "FAIL: no free ports in 8981-8990" >&2; exit 1; }
RDV_P2P="${PORTS[0]}" GATEWAY="${PORTS[1]}" WEB="${PORTS[2]}"

TMP="$(mktemp -d /tmp/concord-guest-lock.XXXXXX)"
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

PASSPHRASE="guest-lock-pass"
echo "== onboard member + start meeting"
LOGIN="$(rpc "{\"method\":\"Login\",\"args\":[\"$PASSPHRASE\"]}")"
jget "$LOGIN" '' >/dev/null || { echo "FAIL: Login: $LOGIN" >&2; exit 1; }
MEETING="$(rpc '{"method":"StartMeeting","args":[]}')"
GUILD_ID="$(jget "$MEETING" '.guild.id')" || { echo "FAIL: StartMeeting: $MEETING" >&2; exit 1; }
CHANNEL_ID="$(jget "$MEETING" '.guild.channels[0].id')"

# A 7-day link: the lifetime the user asked for ("wonder if I can make it so
# that link can be used by people for days").
LINK_RESP="$(rpc "{\"method\":\"CreateGuestLink\",\"args\":[\"$GUILD_ID\",168]}")"
GUEST_LINK="$(jget "$LINK_RESP" '')" || { echo "FAIL: CreateGuestLink: $LINK_RESP" >&2; exit 1; }
EXP_RESP="$(rpc "{\"method\":\"MeetingExpiry\",\"args\":[\"$GUILD_ID\"]}")"
EXPIRES="$(jget "$EXP_RESP" '')" || { echo "FAIL: MeetingExpiry: $EXP_RESP" >&2; exit 1; }
echo "   guest link: $GUEST_LINK"
echo "   expires:    $(node -e 'process.stdout.write(new Date(Number(process.argv[1])).toISOString())' "$EXPIRES")"
node -e '
const ms = Number(process.argv[1]) - Date.now();
const days = ms / 86400000;
if (days < 6.9 || days > 7.1) { console.error(`FAIL: 168h link expires in ${days.toFixed(2)} days`); process.exit(1); }
console.log(`   PASS lifetime: link + room live for ${days.toFixed(2)} days`);
' "$EXPIRES"

# Re-minting with a different lifetime must hand back the SAME url (a link
# already pasted into an invite must not die because you changed your mind).
AGAIN="$(jget "$(rpc "{\"method\":\"CreateGuestLink\",\"args\":[\"$GUILD_ID\",720]}")" '')"
[ "$AGAIN" = "$GUEST_LINK" ] || { echo "FAIL: re-minting changed the link ($AGAIN)" >&2; exit 1; }
echo "   PASS re-minting at 30 days kept the same url"
# …and back to 7 days for the rest of the run.
rpc "{\"method\":\"CreateGuestLink\",\"args\":[\"$GUILD_ID\",168]}" >/dev/null

# A lifetime that is not on the menu must be refused, not silently rounded.
BAD="$(rpc "{\"method\":\"CreateGuestLink\",\"args\":[\"$GUILD_ID\",5]}")"
case "$BAD" in
  *error*) echo "   PASS an off-menu lifetime (5h) is refused" ;;
  *) echo "FAIL: CreateGuestLink accepted 5h: $BAD" >&2; exit 1 ;;
esac

echo "== drive both browsers"
MEMBER_URL="http://127.0.0.1:$WEB" \
  GUEST_LINK="$GUEST_LINK" \
  CHANNEL_ID="$CHANNEL_ID" \
  PASSPHRASE="$PASSPHRASE" \
  OUT_DIR="$TMP" \
  node "$ROOT/scripts/smoke-guest-lock.mjs"

STATUS=0
