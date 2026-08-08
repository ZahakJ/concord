# Contributing to Concord

Thanks for looking. Concord is a peer-to-peer, end-to-end-encrypted chat app: a
Go core (identity, MLS, libp2p, storage) with a Svelte UI that the same core
serves either into a browser or into a native desktop window.

Before changing anything substantial, read the README. It is long on purpose —
it explains *why* the design is the way it is, and most of the questions a
newcomer has ("why is there a server at all?", "why can't two people commit at
once?") are answered there rather than in the code.

Security issues do not belong in an issue or a pull request. See
[SECURITY.md](SECURITY.md).

## Prerequisites

- **Go** — the version in [`go.mod`](go.mod) (currently 1.26.3) or newer.
- **Node** — 22 or newer. That is what CI uses; older majors are untested.
- **webkit2gtk-4.1** — only for the native desktop build (`make gui`). The
  browser-served build needs nothing beyond Go.

The MLS implementation is vendored under `third_party/mls-go` and wired up with
a `replace` in `go.mod`, so there is no extra fetch step.

Mobile (`make android-core` / `ios-core`) additionally needs gomobile plus an
Android NDK or Xcode. You do not need any of it to work on the core or the UI.

## Building

`make help` lists everything. The two that matter:

```sh
make web     # browser-served binary  -> bin/concord-web, then open :8787
make gui     # native desktop window  -> bin/concord-gui
```

Both rebuild the frontend first. That ordering is not optional: the UI is
embedded into the Go binary, so a `go build` without a fresh `frontend/dist`
silently ships the previous UI.

`make release` cross-compiles the zero-dependency web binaries into
`dist-release/`. It publishes nothing — it is a local smoke test.

## Tests

```sh
make test                      # go test ./internal/... .  (incl. a multi-node E2EE integration test)
make race                      # the same under the race detector
cd frontend && npm run lint    # the no-undef gate; catches references to things that don't exist
cd frontend && npm test        # markdown/render unit tests
cd frontend && npm run build   # must succeed — a broken build is a broken release
```

CI runs all of these on every push and pull request. Run them locally first
anyway; the Go suite spins up real libp2p hosts and is occasionally
timing-flaky under load, so a single failure is worth re-running alone before
you go hunting.

## The multi-peer dev loop

Almost every interesting bug in Concord is a bug *between* two peers, so the
normal way to work is to run several of them side by side. There is no server to
point at — each peer is a whole app with its own identity and database.

```sh
make peers          # 2 isolated peers on :8801 and :8802 (make peers N=3 for more)
make dev-clean      # wipe the local test data in .dev/ afterwards
```

Open each printed URL in a separate browser window, unlock each with any
passphrase (the first unlock creates that peer's identity), then create a guild
in peer 1, copy its invite code, and join from peer 2. They find each other over
mDNS on loopback within a couple of seconds. To start one peer from scratch,
delete just its directory: `rm -rf .dev/peer2`.

You can also drive a peer without a browser. The UI talks to the core over a
small JSON-RPC endpoint, and so can you:

```sh
curl -s -X POST http://127.0.0.1:8801/rpc -H 'Content-Type: application/json' \
  -d '{"method":"Login","args":["any-passphrase"]}'
```

The available methods are exactly the `Dispatch` switch in
`internal/bridge/bridge.go`. `curl -N http://127.0.0.1:8801/events` streams the
same server-sent events the UI reacts to, which is the fastest way to see
whether a change actually reached the other side.

To exercise internet-style discovery without deploying anything, `make
rendezvous-run` starts a local rendezvous node with a stable identity and prints
a multiaddr to point peers at via `CONCORD_BOOTSTRAP`.

`.claude/skills/verify/SKILL.md` has more of this loop written down, including
how to drive the real UI headlessly. The browser drivers in `scripts/` need a
`playwright-core` install; they are not wired into `npm test` because pulling a
browser toolchain onto every contributor's machine to run a harness most never
touch is not a trade worth making.

## Code style

Go code is `gofmt`-formatted (`make fmt`) and passes `go vet`. Frontend code
passes `npm run lint`. Beyond that, the thing this codebase is actually strict
about is comments.

**Comments explain why, never what.** A comment that restates the code is worse
than no comment: it goes stale, and it trains readers to skip comments. Write
down the constraint, the failure that forced the shape, the thing the next
person will otherwise "simplify" back into a bug. Real examples from the tree:
why `-skipbindings` is required for the Wails build, why the TURN relay block in
`fly.rendezvous.toml` is commented out, why the libc fork exists at all. Each
one exists because someone lost time to it.

Prose — comments, docs, commit messages, UI copy — is plain and honest. No
marketing. If something is a known gap, say so plainly; the README's threat
model is the tone to match.

Commit messages describe the change from the user's side ("A guild survives its
owner vanishing: name an heir who can take the crown"), not the diff.

## Pull requests

- One concern per PR. A refactor bundled with a behaviour change is very hard to
  review and impossible to revert cleanly.
- Say how you verified it. "Ran two peers, invited, messaged both ways" is worth
  more than a green checkmark.
- Changes to the protocol, the storage schema, or governance rules need a note
  on what happens when an older peer meets a newer one. There is no server to
  migrate everybody at once.
- If your change alters what an attacker can do, update README §13 in the same
  PR.

## Licensing

Concord is licensed under the **GNU Affero General Public License v3.0 or
later** (see [LICENSE](LICENSE)). By contributing, you agree that your
contribution is licensed under AGPL-3.0-or-later. There is no CLA and no
copyright assignment; contributors keep their copyright, held collectively as
"Concord contributors".
