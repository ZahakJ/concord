# Maintainer runbook

Everything a maintainer needs that a user does not: building each front end from
source, running a handful of peers side by side, standing up your own rendezvous
node, and cutting a signed release.

Day-to-day contribution flow, code style and pull-request expectations live in
[CONTRIBUTING.md](../CONTRIBUTING.md). How any of this works lives in
[DESIGN.md](DESIGN.md).

## Prerequisites

| For | You need |
|---|---|
| The browser-served build, tests, releases | Go (the version in `go.mod`) and Node 22+ |
| The native desktop window (`make gui`) | `webkit2gtk-4.1` and cgo |
| The Windows installer in a release | `wine` (NSIS is fetched and cached on first use) |
| Publishing a release | the `gh` CLI, authenticated, with write access to this repo |
| App icons (`make icons`) | ImageMagick |
| Android | gomobile, `ANDROID_HOME` with an NDK, a JDK 17–21 on `JAVA_HOME` |
| iOS | macOS with Xcode |

Nothing else is fetched at build time. The MLS library is vendored under
`third_party/mls-go` with a `replace` in `go.mod`.

## Building

`make help` lists every target. The ones that matter:

```sh
make web     # browser-served binary -> bin/concord-web, then open :8787
make gui     # native desktop window -> bin/concord-gui
make cli     # headless client       -> bin/concord
make rendezvous   # the untrusted node -> bin/rendezvous
```

Both app targets rebuild the frontend first, and that ordering is not optional:
the UI is embedded in the Go binary, so a bare `go build` without a fresh
`frontend/dist` silently ships the previous UI.

`make gui` applies the build tags that select the native variant
(`wails desktop production webkit2_41`). A plain `go build` or `go run .` builds
the browser variant.

`make release VERSION=vX.Y.Z` cross-compiles the zero-dependency web binaries
into `dist-release/`. It publishes nothing and builds no desktop app; it is a
local smoke test. Without `VERSION` the binaries are stamped `dev`, which
disables the in-app update check.

## Running several peers

Almost every interesting bug is a bug *between* two peers, so the normal way to
work is to run several at once. Each peer is a whole app with its own identity
and database; there is no server to point them at.

```sh
make peers          # 2 isolated peers on :8801 and :8802
make peers N=3      # or more
make dev-clean      # delete the local test data under .dev/
```

They find each other over mDNS on loopback within a couple of seconds. To reset
one peer, delete its directory: `rm -rf .dev/peer2`. `BASE_PORT=9000 make peers`
moves the block if 8801 is taken.

To exercise internet-style discovery without deploying anything:

```sh
make rendezvous-run   # stable identity; prints a multiaddr
```

Point peers at the printed address with `CONCORD_BOOTSTRAP`, or paste it on the
login screen.

## Tests

```sh
make test    # go test ./internal/... .   (includes a multi-node E2EE integration test)
make race    # the same under the race detector
```

The Go suite spins up real libp2p hosts and is occasionally timing-flaky under
load, so re-run a single failure alone before hunting it.

**Guest-meeting smoke tests.** The browser-guest path crosses a process
boundary: the rendezvous gateway relays bytes to a guest page that buffers and
parses newline-terminated JSON lines. Each half once looked correct alone while
the pair was broken for weeks (the gateway forwarded lines without the trailing
`\n`, and every guest join hung at "Connecting…"). Two guards cover it now:

- `cmd/rendezvous/guest_smoke_test.go` runs in the normal suite. It drives the
  real gateway handler over a real libp2p stream and a real WebSocket, and
  parses the output with a literal Go transcription of the guest page's
  `ws.onmessage` buffer-and-split loop, so the test *is* the other half of the
  framing contract.
- `scripts/smoke-guest.sh` is the full browser end-to-end: it builds and
  launches a local rendezvous plus a member app, mints a real guest link, and
  drives member and guest in two Chromium contexts, asserting chat both ways
  plus WebRTC audio packets and screen-share frames in both directions. Needs
  `chromium` and a `playwright-core` install (`PLAYWRIGHT_CORE=<path>`); see the
  script header.

`scripts/interop.sh` runs a previous release binary against a fresh build of
HEAD, both bootstrapping off a local rendezvous, to catch the regressions that
only appear against a peer that has not updated. Run it before a release that
touches the wire format, the storage schema, or governance rules.

## Hosting a rendezvous node

The node is untrusted infrastructure: it relays ciphertext, serves the DHT, and
holds an offline mailbox in memory. What it can and cannot see is
[§6 of DESIGN.md](DESIGN.md#6-the-rendezvous-node); what breaks when it dies is
[§6.5](DESIGN.md#65-the-day-it-dies).

`fly.rendezvous.toml` is a working deployment on a free fly.io account. Pick your
own app name first, since fly app names are globally unique.

```sh
fly launch --no-deploy -c fly.rendezvous.toml
fly secrets set CONCORD_RELAY_SEED=$(openssl rand -hex 32) \
                CONCORD_PUBLIC_HOST=<your-app-name>.fly.dev
fly deploy -c fly.rendezvous.toml
fly logs        # copy the ">>> SHARE THIS ADDRESS <<<" line
```

Paste that address on your own login screen under "Connect with friends". From
then on your invite codes carry it, so a friend who pastes one configures their
app from the code alone and never sees a setting.

`CONCORD_RELAY_SEED` is the node's identity. Keep it stable forever: every invite
code ever issued embeds the PeerID derived from it, and changing it makes all of
them worthless.

Two optional services, both off until configured:

- **GIF search.** Set `CONCORD_GIF_KEY` as a fly secret (Giphy by default; see
  the comments in `fly.rendezvous.toml` for the provider variables). With no key
  the node reports the feature unavailable and the picker says so.
- **Push wakes.** Only exists if platform credentials are configured. The
  mailbox works the same without it.

**Do not enable the TURN relay on fly.io.** The deployment file ships with that
service commented out and the measurement recorded beside it: fly's edge does not
hairpin UDP back to the advertised public IP, so a relay there authenticates,
allocates, reports success and carries nothing. Re-enabling it without re-testing
end-to-end media resurrects a silent failure where calls connect, report success,
and carry no sound. If IP-private calls matter, host the relay on a machine that
owns its public IP (any small VPS) and point `CONCORD_TURN_*` at it. The full
account is [§9.2 of DESIGN.md](DESIGN.md#92-ip-privacy-the-state-of-it).

## Cutting a release

Releases are built on your machine and uploaded with `gh`. No GitHub Actions
compute is involved: CI-built releases burn macOS runner minutes at a 10×
multiplier, so the `release` workflow is `workflow_dispatch`-only and exists for
the rare case where you want CI-built Windows and macOS desktop apps.

```sh
git tag v0.9.0 && git push origin main v0.9.0
scripts/publish-release.sh v0.9.0 notes.md      # notes file is optional
```

The tag has to be plain semver `vX.Y.Z`. The update check recognises no other
shape, so a `-rc` tag reads as "no update", and the script rejects it outright.

### What the script builds and uploads

1. `make release VERSION=…`: the zero-dependency web binaries for Linux
   amd64/arm64, macOS arm64/intel, and Windows. One file per OS, "download, run,
   browser opens".
2. The native Linux desktop app, if `webkit2gtk-4.1` is installed. Skipped with a
   note otherwise.
3. The native Windows desktop app, cross-compiled from Linux. This is the one
   Wails target that cross-compiles, because its WebView2 backend is pure Go.
4. `Concord-Setup-vX.Y.Z.exe`, the one-click Windows installer, compiled by NSIS
   under wine. Its name carries no OS keyword, on purpose, so the in-app
   updater can never mistake the installer for the app binary.
5. The Android APK, if one has been built for this exact version (below).
   A missing APK is a loud warning rather than a failure.
6. `WINDOWS.md`, then `SHA256SUMS` over every asset, then a signature over
   `SHA256SUMS`.

Assets are uploaded one at a time with retries, because `gh release create` with
assets attached rolls the whole release back if a single large upload hits a
transient TLS reset.

### Android

Build the APK before running the publish script, for the same tag:

```sh
make android-app VERSION=v0.9.0 MOBILE_VERSION_CODE=900
```

This produces both the Play bundle (`app-release.aab`, all ABIs) and an
arm64-only sideload APK named `concord-0.9.0-android.apk`, which is what the
publish script picks up. arm64-only covers every phone since roughly 2017 and
halves the download.

`MOBILE_VERSION_CODE` is a plain integer that must never decrease across
releases; Play rejects an upload whose code is not higher than the last one.
Deriving it from the tag keeps that automatic: minor × 100 + patch, so v0.55.0
is 5500 and v0.55.1 is 5501.

The x86 ABIs need a patched `modernc.org/libc`, because Android's seccomp filter
denies the legacy x86_64 path syscalls it uses and the process dies with SIGSYS
the moment SQLite opens on an emulator. `make android-core` materializes a local
module fork under `build/libc-fork/` from the committed overlay in
`third_party/_libc-overlay/` and drops the `replace` again afterwards, even on
failure, so `go.mod` and every desktop build stay untouched. Real arm64 devices
never compile the patched file.

### What still needs a Mac

The macOS `.app` bundle. Everything else in a release, including the Windows
desktop app and installer, cross-compiles from Linux. Two options: build on a
Mac and `gh release upload` the extra asset afterwards, or run the `release`
workflow manually from the tag when there is billing headroom.

`make ios-app` likewise needs macOS with Xcode. iOS has no turnkey distribution
yet.

### Signing, and why the host stops mattering

Release trust is a signature, not a source. `SHA256SUMS` lists every asset's
hash, so signing that one file covers all of them. Releases are signed with an
offline Ed25519 key whose public half is compiled into every binary
(`internal/bridge/release-pubkey.txt`); a client verifies the signature over
`SHA256SUMS` first, then checks the download against the now-trusted hash, never
the other way round.

Because a signature is what is trusted, where the bytes came from stops
mattering, which is what makes peer-to-peer updates safe: a peer already running
the newer build can hand you the same bytes, as a courier rather than an
authority. The worst it can do is refuse, which is why GitHub stays a source
instead of being replaced. A build with no embedded key refuses peer updates
outright.

Generate the keypair once, on the machine that publishes:

```sh
make release-keygen     # private half stays here, back it up offline
```

Commit the printed public line to `internal/bridge/release-pubkey.txt`. Publishing
without a signature fails loudly rather than shipping a release that
signature-enforcing builds would refuse to install.

### Where releases go

Binaries are published to this repository's own Releases, which is also what the
in-app update check polls (`internal/bridge/update.go`). That poll is
unauthenticated, so no token is ever embedded in a shipped binary, and the
workflow signs in with the run's built-in `GITHUB_TOKEN` under
`permissions: contents: write`. There is no personal access token to create,
scope or rotate, and a fork can cut its own releases with no secrets configured.

### Windows false-positive submission

Unsigned executables lose SmartScreen reputation on every new build. After each
release, submit `concord-desktop-windows-vX.Y.Z.exe` to
<https://www.microsoft.com/en-us/wdsi/filesubmission> as an incorrectly detected
file, which whitelists that build's hash in Defender. It has to be redone every
release, because every release is a new hash. The durable fix is code signing,
deferred on cost. [WINDOWS.md](../WINDOWS.md) has the user-facing bypass and the
web-binary fallback.

### Rules that are easy to break once

- **Never move a published tag.** Bump to a new `vX.Y.Z` instead. The desktop
  matrix and the web track both key off the tag.
- **Tags are `vX.Y.Z` and nothing else.** Pre-release suffixes read as "no
  update" to every installed client.
- **`make release` alone publishes nothing.** It builds the web binaries only.
- **The rendezvous is deployed separately and rarely** (`fly deploy -c
  fly.rendezvous.toml`), and its seed never changes.
