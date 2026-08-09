# Contributing to Concord

Concord is a peer-to-peer, end-to-end-encrypted chat app: a Go core (identity,
MLS, libp2p, storage) with a Svelte UI that the same core serves either into a
browser or into a native desktop window.

Before changing anything substantial, read [docs/DESIGN.md](docs/DESIGN.md). It
is long on purpose. It explains why the design is the way it is, and most of the
questions a newcomer has ("why is there a server at all?", "why can't two people
commit at once?") are answered there rather than in the code.

Security issues do not belong in an issue or a pull request. See
[SECURITY.md](SECURITY.md).

## Prerequisites

- Go: the version in [`go.mod`](go.mod) (currently 1.26.3) or newer.
- Node 22 or newer. That is what CI uses; older majors are untested.
- `webkit2gtk-4.1`, for the native desktop build (`make gui`) only. The
  browser-served build needs nothing beyond Go.

The MLS implementation is vendored under `third_party/mls-go` and wired up with
a `replace` in `go.mod`, so there is no extra fetch step.

Mobile (`make android-core`, `make ios-core`) additionally needs gomobile plus
an Android NDK or Xcode. You need none of it to work on the core or the UI.

## One-time setup

```sh
git config core.hooksPath .githooks
```

Commit authors here are GitHub noreply addresses. That line turns on two hooks
in [`.githooks/`](.githooks) which check it, one before the commit is written
and one before it is pushed. Yours is shown on
<https://github.com/settings/emails> and looks like
`ID+username@users.noreply.github.com`.

Git does not clone hooks, so this is per-clone.

## Building

`make help` lists every target. The two that matter:

```sh
make web     # browser-served binary  -> bin/concord-web, then open :8787
make gui     # native desktop window  -> bin/concord-gui
```

Both rebuild the frontend first. That ordering is not optional: the UI is
embedded into the Go binary, so a `go build` without a fresh `frontend/dist`
silently ships the previous UI.

`make release` cross-compiles the zero-dependency web binaries into
`dist-release/`. It publishes nothing; it is a local smoke test.

## Tests

```sh
make test                      # go test ./internal/... .  (includes a multi-node E2EE integration test)
make race                      # the same under the race detector
make fmt                       # gofmt the Go tree
cd frontend && npm run lint    # the no-undef gate: catches references to things that do not exist
cd frontend && npm test        # markdown, render and other unit tests
cd frontend && npm run build   # must succeed; a broken build is a broken release
```

On every pull request (and every push to `main`), CI runs `go build ./...`,
`go vet ./...`, `go test -race -short ./...`, and the three frontend steps. See
[`.github/workflows/ci.yml`](.github/workflows/ci.yml). It has to be green
before anything merges. `gofmt` is part of that gate, so run `make fmt` before
you push rather than discovering it in a red build.

`-short` is deliberate: it skips the tests that stand up real libp2p nodes and
wait on message propagation, which are too timing-sensitive to gate a pull
request on a shared runner. Those run nightly in the `integration` job, and
`make test` runs them locally — so a change to the networking layer wants a
local `make race` before you push, because the gate will not catch it.

Run the rest locally first as well. The Go suite starts real libp2p hosts and is
occasionally timing-flaky under load, so re-run a single failure on its own
before you go hunting.

## The multi-peer dev loop

Almost every interesting bug in Concord is a bug *between* two peers, so the
normal way to work is to run several of them side by side. There is no server to
point at: each peer is a whole app with its own identity and database.

```sh
make peers          # 2 isolated peers on :8801 and :8802 (make peers N=3 for more)
make dev-clean      # wipe the local test data in .dev/ afterwards
```

Open each printed URL in a separate browser window, unlock each with any
passphrase (the first unlock creates that peer's identity), then create a guild
in peer 1, copy its invite code, and join from peer 2. They find each other over
mDNS on loopback within a couple of seconds. Logs land in `.dev/peer*.log`. To
start one peer from scratch, delete just its directory: `rm -rf .dev/peer2`.

The `?t=` on each printed URL is the local API token — the page stores it and
strips it from the address bar. Every request to the core needs it, so open the
URL as printed rather than retyping the bare host, or you get `unauthorized`.

You can also drive a peer without a browser. The UI talks to the core over a
small JSON-RPC endpoint, and so can you. `make peers` pins one token for the
run and prints it; a standalone binary mints a random one and prints it on the
URL at startup:

```sh
export CONCORD_API_TOKEN=<the token make peers printed>
curl -s -X POST http://127.0.0.1:8801/rpc \
  -H "Authorization: Bearer $CONCORD_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"method":"Login","args":["any-passphrase"]}'
```

The available methods are the `Dispatch` switch in `internal/bridge/bridge.go`
and nothing else. `curl -N "http://127.0.0.1:8801/events?token=$CONCORD_API_TOKEN"`
streams the same server-sent events the UI reacts to, which is the fastest way
to see whether a change reached the other side — SSE cannot set headers, so it
takes the token as a query parameter. A CSRF guard additionally rejects requests
carrying a foreign `Origin` header; curl sends none, so it passes.

One shell gotcha: zsh reserves `GID`, so it is the one variable name not to
reach for when scripting guild IDs.

### Driving the real UI without a browser window

For a change you want to *see* rather than assert on, `chromium --headless
--screenshot` does not work — the SSE stream never lets the page reach a settled
state, so the capture hangs. Playwright against the system Chromium does, with
no browser download:

```sh
npm i playwright-core
```

```js
import { chromium } from "playwright-core";
const browser = await chromium.launch({ executablePath: "/usr/bin/chromium" });
```

Then: load `127.0.0.1:8801`, fill `input[placeholder*="assphrase"]` and press
Enter (the backend session survives a reload, but the UI always asks), then
press Escape to dismiss anything already open. Useful handles: member names are
`.mname`, the profile popover is `[role="dialog"][aria-label*="profile"]`, and
the composer's `input[type="file"]` takes `setInputFiles` to send an attachment.
Clicking the guild-name header opens Guild settings, which is usually not what
you wanted. `scripts/playwright.mjs` and the `smoke-*` scripts are worked
examples.

To exercise internet-style discovery without deploying anything, `make
rendezvous-run` starts a local rendezvous node with a stable identity and prints
a multiaddr to point peers at via `CONCORD_BOOTSTRAP`.

One level up from that loop are the end-to-end harnesses in `scripts/`, which
drive real browsers against real peers: guest joins, calls, screen share, and
`interop.sh`, which runs a previous release as one peer and a build of your
branch as the other. Each script opens with a comment saying what it covers and
why. They need a `playwright-core` install (`npm i playwright-core` anywhere the
script can find it) and are not wired into `npm test`, because pulling a browser
toolchain onto every contributor's machine to run a harness most will never
touch is not a trade worth making.

## Code style

Go is `gofmt`-formatted (`make fmt`) and passes `go vet`. Frontend code passes
`npm run lint`. Beyond that, the thing this codebase is strict about is
comments.

### Comments explain why, never what

A comment that restates the code is worse than no comment: it goes stale, and it
teaches readers to skip comments. Write down the constraint, the failure that
forced the shape, the thing the next person will otherwise "simplify" back into
a bug. Real examples from the tree: why `-skipbindings` is required for the
Wails build, why the TURN relay block in `fly.rendezvous.toml` is commented out,
why the libc fork exists at all, why the governance backdating guard runs on the
live path but not on sync backfill. Each of those exists because someone lost
time to it.

The corollary is that a comment is a claim, and a claim that stops being true is
a bug. If you change the shape a comment describes, change the comment in the
same diff.

### Prose

Comments, docs, commit messages and UI copy are plain. No marketing. If
something is a known gap, say so; the threat model in
[docs/DESIGN.md](docs/DESIGN.md#13-threat-model-what-concord-defends-against) is the tone to match.

Commit messages describe the change from the user's side ("A guild survives its
owner vanishing: name an heir who can take the crown"), not the diff.

## Pull requests

Open an issue before starting anything large. Two maintainers cannot absorb a
rewrite that arrives unannounced, and hearing "we don't want this" before you
build it is better than after.

Then:

- One concern per pull request. A refactor bundled with a behaviour change is
  hard to review and impossible to revert cleanly.
- Keep it small. A 200-line diff gets read closely. A 2,000-line one gets
  skimmed, and two people reviewing in their spare time will skim.
- Tests for anything with logic in it. If a bug was reachable, a test should
  reach it.
- Say how you verified it. "Two peers, invited, messaged both ways, restarted
  peer 2 and the history came back" is worth more than a green checkmark.
- Changes to the wire protocol, the storage schema, or governance rules need a
  note on what happens when an older peer meets a newer one. There is no server
  to migrate everybody at once, so both versions are live at the same time.
  `scripts/interop.sh` runs that pairing for you.
- If your change alters what an attacker can do, update the threat model in
  [docs/DESIGN.md](docs/DESIGN.md#13-threat-model-what-concord-defends-against) in the same pull request.
- CI green before review, not after.

Who reviews what, and which changes need both maintainers to sign off, is in
[MAINTAINERS.md](MAINTAINERS.md).

## Sign your commits off

Commit with `git commit -s`. That appends a `Signed-off-by:` line carrying your
name and email.

The line is the [Developer Certificate of Origin](https://developercertificate.org/):
by adding it you state that you wrote the patch, or otherwise have the right to
submit it under this project's license. It is a claim about where the code came
from, not a transfer of anything, and it costs one flag.

## Licensing

Concord is licensed under the GNU Affero General Public License v3.0 or later
(see [LICENSE](LICENSE)). By contributing, you agree that your contribution is
licensed under AGPL-3.0-or-later.

There is no CLA and no copyright assignment. Contributors keep their own
copyright, held collectively as "Concord contributors".
