# Testing

The checks a change has to pass, exactly as CI runs them, and the slower suites and harnesses for what the gate does not cover: the network integration tests, local multi-peer runs, and the browser end-to-end scripts.

CI is defined in `.github/workflows/ci.yml`. It runs on every push to `main`, on every pull request, nightly, and on manual dispatch. It has three jobs: `frontend` and `backend` run every time, and `integration` runs only on the nightly schedule or a manual dispatch.

## The frontend job

```sh
cd frontend && npm ci
cd frontend && npm run lint     # Lint (no-undef gate)
cd frontend && npm run build    # Build
cd frontend && npm test         # Markdown/render tests
node scripts/build-docs.mjs --check   # Documentation site is current
```

- **Lint** runs ESLint over `frontend/src/**/*.{js,mjs,svelte}`. The rule that matters is `no-undef`: a reference to something that does not exist compiles cleanly and fails only at runtime, in the user's window.
- **Build** is the production Vite build. It has to succeed, because every shipped artifact embeds or copies its output.
- **Test** runs each `frontend/src/lib/*.test.mjs` file with plain Node. There is no test framework; each file is a script that exits non-zero when a case fails. The files are listed one by one in the `test` script in `frontend/package.json`, so a new test file runs only once it is added there.
- **Documentation site is current** runs from the repository root. The manual is published from the committed `docs/manual/`, so this fails when a Markdown source under `docs/manual/` or `docs/dev/` (or one of the repository documents rendered as a page) was edited without rerunning `make docs`, and when a link points at a file or heading that does not exist.

## The backend job

The Go binary embeds `frontend/dist`, which is not tracked, so this job builds the frontend before anything else:

```sh
cd frontend && npm ci && npm run build
go build ./...
go vet ./...
gofmt -l . | grep -v '^third_party/'   # must print nothing
go test -race -short ./...
```

- **gofmt** fails the job if any Go file outside `third_party/` is not formatted. `third_party/` is vendored and keeps upstream's formatting; never reformat it. `make fmt` runs `gofmt -w` on the root `.go` files, `internal/` and `cmd/`. It does not touch `mobile/` or `scripts/`, which CI still checks, so run `gofmt -w` on those directly if you change them.
- **Tests** run under the race detector with `-short`, which skips every test that starts real libp2p nodes and waits on message propagation. Those tests pass quickly on an idle machine but miss their deadlines under `-race` on a shared runner, often enough that a red build would stop meaning anything.

### Writing a network test

A test that starts real peers and waits for something to arrive belongs in the integration suite. Guard it at the top:

```go
if testing.Short() {
	t.Skip("network integration test")
}
```

Tests of pure logic run in the gate and need no guard. Most of `internal/app` is integration tests; `TestThreeNodeGuildE2EE` in `internal/app/integration_test.go`, three services forming a guild and exchanging encrypted messages, is the one to copy from.

## The integration job

```sh
cd frontend && npm ci && npm run build
go test -race -p 1 -timeout 30m ./...
```

The full Go suite, including every `testing.Short()`-guarded test, under the race detector. `-p 1` runs one package at a time so packages do not compete for the CPU. It runs nightly and on manual dispatch, and does not block a pull request, so a change to networking, sync or MLS handling should run it locally before pushing.

## Running the checks locally

The same steps, in an order that fails fast. Build the frontend before Go, since Go embeds it:

```sh
npm --prefix frontend run build
npm --prefix frontend run lint
npm --prefix frontend test
node scripts/build-docs.mjs --check
go build ./... && go vet ./...
gofmt -l . | grep -v '^third_party/'
go test -race -short ./...
```

`npm --prefix frontend` runs scripts with the repository root as the working directory, not `frontend/`.

The `Makefile` has two shortcuts for the Go suite. Both run the integration tests, but over `./internal/...` and the root package only, so they skip `cmd/` (including the rendezvous guest-framing test and the bot integration test) and `mobile/`:

```sh
make test   # go test ./internal/... . -count=1
make race   # the same under -race
```

The integration tests start real libp2p hosts and can fail on timing under load. Re-run a single failure on its own (`go test -race -run TestName ./internal/app`) before investigating it.

## Local peers

Most bugs worth finding are between two peers. `make peers` builds the frontend and the browser-served binary into `.dev/`, then starts isolated peers, each with its own data directory (`CONCORD_HOME=.dev/peerN`) and port:

```sh
make peers          # peers on 127.0.0.1:8801 and :8802
make peers N=3      # more peers
BASE_PORT=9000 make peers   # a different port block
make dev-clean      # delete .dev/
```

It pins one API token for the whole run and prints each peer's URL with it as `?t=`. Open each URL in a separate browser window. Peers find each other over mDNS within a few seconds; logs are in `.dev/peerN.log`. The script also prints a `curl` command for calling a peer's `/rpc` with that token, and [Contributing](../../CONTRIBUTING.md#the-multi-peer-dev-loop) walks through driving peers from the shell.

To test internet-style discovery, `make rendezvous-run` starts a local rendezvous and prints a multiaddr; point peers at it with `CONCORD_BOOTSTRAP`.

## End-to-end scripts

`scripts/` holds harnesses that drive real browsers against real peers. They are not part of CI or `npm test`. Each opens with a comment describing what it proves. They need Chromium and a `playwright-core` install, which is deliberately not a project dependency: `scripts/playwright.mjs` finds one through `PLAYWRIGHT_CORE`, or in a `node_modules/` in the working directory or the repository root.

| Script | Covers |
|---|---|
| `smoke-guest.sh` | A guest meeting through a real local rendezvous and member app: chat both ways, plus WebRTC audio and screen share in both directions |
| `smoke-guest-ui.sh` | The guest page's call interface: screen-share stage, self-view while sharing |
| `smoke-guest-lock.sh` | A locked meeting: a guest held at the door, and guest links with a chosen lifetime |
| `verify-guest.sh` | The locked-meeting path again, inspecting what an unadmitted guest's WebSocket actually receives |
| `verify-member.sh` | Two members in one guild: an ordinary call with audio and screen share, and the meeting-creation UI |
| `verify-compat.sh` | An old rendezvous gateway with a new member app, and the reverse |
| `interop.sh` | A previous release binary as one peer and a build of your branch as the other, both on a local rendezvous: discovery, joins, messages, attachments and sync |

Run `interop.sh` for any change to the wire protocol, the storage schema or governance rules. Its first argument is the old binary; by default it looks for a past release in `dist-release/`.

The fast half of the guest-framing check also runs in the Go suite: `cmd/rendezvous/guest_smoke_test.go` drives the real gateway handler and parses its output with a Go copy of the guest page's line-splitting loop.

## Checking UI changes

A string in the bundle is not proof that it renders. For a change to the UI, run `make peers`, drive the change in a browser, and look at the result. `scripts/smoke-guest.mjs` and the other `.mjs` drivers show how to launch Chromium through `playwright-core`, unlock a peer and act on the page.
