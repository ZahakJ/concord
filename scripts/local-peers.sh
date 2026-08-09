#!/usr/bin/env bash
#
# Spin up N isolated Concord peers (browser-served) for local multi-peer
# testing. Each peer gets its own data dir and port, so they behave like
# separate people on the same machine. They discover each other over mDNS.
#
# Usage:
#   ./scripts/local-peers.sh [N]      # default 2
#   BASE_PORT=9000 ./scripts/local-peers.sh 3
#
# Open the printed URLs in separate browser windows/profiles. Ctrl-C stops all.
set -euo pipefail
cd "$(dirname "$0")/.."

N="${1:-2}"
BASE_PORT="${BASE_PORT:-8801}"
mkdir -p .dev

# One pinned API token for the whole run. The server otherwise mints a random
# one per process and only reveals it on the URL it prints to its own log, which
# left `make peers` printing URLs that answered "unauthorized" and a curl loop
# that had to scrape .dev/peerN.log first. Pinning is what CONCORD_API_TOKEN is
# for (see main_web.go); sharing one across peers is what makes a helper like
#   rpc() { curl -s -X POST "http://127.0.0.1:$1/rpc" -H "Authorization: Bearer $CONCORD_API_TOKEN" ...; }
# work against any of them. Loopback-only dev tooling — do not reuse the idea
# anywhere a real identity lives.
export CONCORD_API_TOKEN="${CONCORD_API_TOKEN:-$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')}"

echo "Building frontend + web server..."
( cd frontend && npm install >/dev/null 2>&1 && npm run build >/dev/null 2>&1 )
go build -o .dev/concord-web .

pids=()
cleanup() {
  echo
  echo "Stopping peers..."
  for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
}
trap cleanup EXIT INT TERM

echo
echo "Starting $N peer(s):"
for i in $(seq 1 "$N"); do
  port=$((BASE_PORT + i - 1))
  home=".dev/peer$i"
  mkdir -p "$home"
  CONCORD_HOME="$home" CONCORD_WEB_ADDR="127.0.0.1:$port" \
    .dev/concord-web > ".dev/peer$i.log" 2>&1 &
  pids+=($!)
  printf "  peer %d  →  http://127.0.0.1:%d/?t=%s   (data: %s)\n" \
    "$i" "$port" "$CONCORD_API_TOKEN" "$home"
done

cat <<EOF

Open each URL in a separate browser window (or private windows). The ?t= is the
API token — the page stores it and strips it from the address bar.
On first unlock, pick any passphrase — each peer has its own identity.
Try: create a guild in peer 1 → Invite → paste the code into peer 2's "Join".

To drive a peer from the shell:
  export CONCORD_API_TOKEN=$CONCORD_API_TOKEN
  curl -s -X POST http://127.0.0.1:$BASE_PORT/rpc \\
    -H "Authorization: Bearer \$CONCORD_API_TOKEN" \\
    -H 'Content-Type: application/json' \\
    -d '{"method":"Guilds","args":[]}'

Logs:  .dev/peer*.log      Stop all:  Ctrl-C
EOF

wait
