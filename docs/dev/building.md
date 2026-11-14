# Building from source

How to build each Concord binary from a checkout: the browser-served build, the desktop window, the headless client, the rendezvous node, the Android and iOS apps, and the Windows cross-build.

Everything here is driven by the `Makefile`; `make help` prints the common targets. For the day-to-day development loop see [Contributing](../../CONTRIBUTING.md), and for cutting a release see [Releasing](../RELEASING.md).

## Prerequisites

| To build | You need |
|---|---|
| Anything | Go, at the version in `go.mod` or newer, and Node 22 or newer (what CI uses) |
| The desktop window (`make gui`) | cgo and `webkit2gtk-4.1` on Linux |
| The hot-reloading desktop window (`make gui-dev`) | The `wails` CLI on `PATH` |
| The Android core (`make android-core`) | `gomobile` (`go install golang.org/x/mobile/cmd/gomobile@latest`, then `gomobile init`), `ANDROID_HOME` with an NDK, and `rsync` |
| The Android app (`make android-app`) | The above, plus a JDK 17 to 21 on `JAVA_HOME` |
| iOS (`make ios-core`, `make ios-app`) | macOS with Xcode, plus `gomobile` |

The MLS library is vendored under `third_party/mls-go` and wired in with a `replace` in `go.mod`, so there is no separate fetch step. The browser-served build, the CLI and the rendezvous are pure Go and need no C toolchain.

## Why the frontend is built first

The Svelte UI is compiled into `frontend/dist` and embedded into the Go binary with `//go:embed all:frontend/dist` (in both `main_web.go` and `main_wails.go`). Two things follow:

- On a fresh checkout, `frontend/dist` does not exist, and `go build .` fails because the embed pattern matches nothing.
- Once it exists, `go build` embeds whatever is there. A stale `frontend/dist` builds without complaint and ships the previous UI.

So every app target in the `Makefile` depends on the `frontend` target, which runs `npm install` and `npm run build` in `frontend/`. If you run `go build` yourself, run `npm --prefix frontend run build` first. The Capacitor shells copy `frontend/dist` rather than embedding it, but the mobile app targets depend on `frontend` for the same reason.

The `frontend` target also deletes `frontend/dist/memes` after building. Those images are not redistributable and are kept out of every shipped artifact; set `CONCORD_MEMES=1` to keep them in a build that will not leave your machine.

## Desktop and web

```sh
make web          # browser-served build      -> bin/concord-web
make gui          # native desktop window     -> bin/concord-gui
make gui-dev      # hot-reloading desktop window through the wails CLI
make cli          # headless client and bots  -> bin/concord
make rendezvous   # the rendezvous node       -> bin/rendezvous
make clean        # remove bin/ and frontend/dist
```

`bin/concord-web` serves the UI on `127.0.0.1:8787` and opens the default browser; `CONCORD_WEB_ADDR` changes the address. It has no system dependencies, so it runs anywhere Go targets.

`make gui` is a plain `go build` with the tags `wails desktop production webkit2_41`, not the `wails` CLI, so the WebKitGTK 4.1 tag is always applied. Without the `wails` tag, a build produces the browser-served variant.

`make cli` and `make rendezvous` do not embed the UI and do not depend on `frontend`.

### Version stamping

Binaries report `dev` unless built with `VERSION`, which is stamped into `internal/version.Version` and drives the in-app update check:

```sh
make release VERSION=v0.9.0
```

A `dev` build never offers an update.

### A built-in rendezvous

A copy of Concord can carry the rendezvous it starts from, so a fresh install,
and an account restored from its recovery phrase, reach other people with
nothing typed. Set `RENDEZVOUS` to the address your node prints at startup (one,
or several comma-separated), in its `/dns4/<host>/tcp/<port>/p2p/<peer id>` form:

```sh
make web RENDEZVOUS=/dns4/rdv.example.com/tcp/4001/p2p/12D3KooW...
make release VERSION=v0.9.0 RENDEZVOUS=/dns4/rdv.example.com/tcp/4001/p2p/12D3KooW...
```

Every target honours it (`web`, `gui`, `release` including the Windows binary,
`android-core`/`android-app`, `ios-core`); `scripts/publish-release.sh` reads it
from the environment and prints it in its summary, and the CI desktop build takes
it from the repository variable `CONCORD_RENDEZVOUS`. It is stamped into
`internal/version.Rendezvous`, which is empty in the source: a plain clone ships
with none and starts on the local network only. The binary names its built-in
node in `--version`, `--help` and on Settings' About card, and Settings ›
Connection marks the address "Built in to this copy of Concord" with a way to
replace or clear it; whatever a user sets there, on the restore screen or
through an invite code is saved and wins. The privacy cost, in one sentence: the
node you build in sees each new user's peer id and network address, and when
they are online (DESIGN §6.3).

### Cross-compiling, including Windows

`make release` cross-compiles the browser-served build for Linux (amd64, arm64), macOS (arm64, Intel) and Windows (amd64) into `dist-release/`, with `CGO_ENABLED=0`. The Windows binary gets a version resource and manifest from `build/versioninfo.json` through `goversioninfo`, with the version numbers taken from `VERSION`, and keeps its symbols, since both reduce false positives from Windows Defender on an unsigned executable. Do not edit the numbers in `build/versioninfo.json` by hand; they are overridden from the tag.

The Windows desktop window also cross-compiles from Linux, because the Wails WebView2 backend on Windows needs no cgo. `scripts/publish-release.sh` builds it with:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -tags "wails desktop production" -ldflags "-s -w -H windowsgui" \
  -o concord-desktop-windows.exe .
```

The macOS `.app` bundle needs a Mac; the `release` workflow in `.github/workflows/release.yml` builds it with `wails build` on a macOS runner when run by hand.

## Android

The Android app is a Capacitor shell (`apps/mobile`) around the Go core, which `gomobile` compiles into an `.aar`.

```sh
make android-core     # Go core -> apps/mobile/android/app/libs/concord.aar
```

`android-core` targets API 26 (the app's `minSdkVersion`) and links with 16 KB page alignment, which Android 15 devices and Play require. It also builds the x86 ABIs against a patched `modernc.org/libc`: Android's seccomp filter kills the process with `SIGSYS` on the legacy syscalls the stock version uses on x86_64 emulators. The target copies the module into `build/libc-fork/`, applies the committed overlay from `third_party/_libc-overlay/`, adds a `replace` to `go.mod` for the duration of the bind, and removes it again even if the bind fails. Real arm64 devices never compile the patched file. `ANDROID_TARGET=android/amd64` binds a single ABI, which is what CI does to keep the build short.

For a debug build to run on an emulator or a device:

```sh
make frontend android-core
cd apps/mobile && npm ci && npx cap sync android
cd android && ./gradlew assembleDebug
```

For a release build:

```sh
make android-app VERSION=v0.9.0 MOBILE_VERSION_CODE=900
```

This produces the Play bundle (`app-release.aab`, all ABIs) and an arm64-only sideload APK named `concord-0.9.0-android.apk`. `MOBILE_VERSION_CODE` is required and must be an integer greater than 1 that increases with every release; by convention it is minor × 100 + patch. Release signing reads `apps/mobile/android/keystore.properties` if present (it is gitignored); without it, the release build is unsigned.

Gomobile also writes `concord-sources.jar` beside the `.aar`. Both are build output and both are gitignored.

## iOS

```sh
make ios-core   # Go core -> apps/mobile/ios/Frameworks/Concord.xcframework
make ios-app    # frontend, core, cap sync, then an Xcode archive
```

Both need macOS and Xcode. `ios-core` targets iOS 15.0 for device and simulator. `ios-app` archives to `apps/mobile/ios/App/build/App.xcarchive`; signing and distribution are configured in Xcode.

## Other targets

| Target | Does |
|---|---|
| `make peers [N=3]` | Builds the browser-served binary into `.dev/` and starts N isolated local peers (see [Testing](testing.md#local-peers)) |
| `make rendezvous-run` | Runs a local rendezvous with a fixed development seed |
| `make dev-clean` | Deletes `.dev/` |
| `make docs` | Rebuilds the documentation site in `docs/manual/` from the Markdown sources |
| `make icons` | Regenerates the app icons from `build/appicon.svg` (needs ImageMagick) |
| `make banner` | Regenerates the README banner and social preview from `docs/media/*.svg` (needs `rsvg-convert` and `woff2_decompress`) |
| `make release-keygen` | Creates the release signing keypair, once, on the publishing machine (see [Releasing](../RELEASING.md)) |
