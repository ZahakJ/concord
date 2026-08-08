#!/usr/bin/env bash
#
# Local release publisher — the ZERO-GitHub-compute path.
#
# GitHub Actions on a private repo bills macOS runners at a 10x minute
# multiplier, so CI-building each release was burning real money. This script
# replaces that for the common case: it builds everything THIS machine can
# build (web binaries for all OSes via cross-compilation, plus the native
# Linux desktop app when webkit2gtk is installed), checksums it, and uploads
# straight to the public dist repo with `gh` — GitHub only stores the files.
#
# Windows/macOS native desktop apps still need their own OS: run the `release`
# workflow manually (workflow_dispatch) when billing headroom exists, or build
# on that machine and `gh release upload` the extra assets afterward.
#
# Usage:
#   scripts/publish-release.sh vX.Y.Z [notes-file.md]
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${1:?usage: publish-release.sh vX.Y.Z [notes-file.md]}"
NOTES_FILE="${2:-}"
RELEASE_REPO="ZahakJ/concord"

[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "error: version must look like v0.9.0" >&2; exit 1; }
command -v gh >/dev/null || { echo "error: needs the gh CLI, authenticated" >&2; exit 1; }

echo "==> building web release binaries ($VERSION)"
make release VERSION="$VERSION"

# Native Linux desktop app — buildable right here when the WebView dev package
# is present; skipped (with a note) otherwise.
if pkg-config --exists webkit2gtk-4.1 2>/dev/null; then
  echo "==> building native Linux desktop app"
  go build -tags "wails desktop production webkit2_41" -trimpath \
    -ldflags "-X github.com/ZahakJ/concord/internal/version.Version=$VERSION" \
    -o "dist-release/concord-desktop-linux-$VERSION" .
else
  echo "==> webkit2gtk-4.1 not found; skipping the native Linux desktop build"
fi

# Native WINDOWS desktop app — the one Wails target that cross-compiles from
# Linux (its WebView2 backend is pure Go, no cgo). -H windowsgui hides the
# console; the goversioninfo .syso stamps icon + version resource. macOS's
# .app still needs a Mac (or the manual CI workflow).
echo "==> cross-building native Windows desktop app"
go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.7.0 -64 \
  -o resource_windows_amd64.syso build/versioninfo.json
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -tags "wails desktop production" \
  -ldflags "-s -w -H windowsgui -X github.com/ZahakJ/concord/internal/version.Version=$VERSION" \
  -o "dist-release/concord-desktop-windows-$VERSION.exe" .
rm -f resource_windows_amd64.syso

# Windows one-click INSTALLER (Concord-Setup-*.exe): NSIS compiled under wine
# (fully user-space; the portable NSIS zip is cached on first use). The name
# deliberately carries no OS keyword so the in-app updater can never mistake
# the installer for the app binary.
NSIS_DIR="$HOME/.cache/concord-nsis/nsis-3.11"
if command -v wine >/dev/null; then
  if [[ ! -x "$NSIS_DIR/makensis.exe" && ! -f "$NSIS_DIR/makensis.exe" ]]; then
    echo "==> fetching portable NSIS"
    mkdir -p "$HOME/.cache/concord-nsis"
    curl -sL --max-time 180 -o "$HOME/.cache/concord-nsis/nsis.zip" \
      "https://downloads.sourceforge.net/project/nsis/NSIS%203/3.11/nsis-3.11.zip" &&
      python3 -c "import zipfile;zipfile.ZipFile('$HOME/.cache/concord-nsis/nsis.zip').extractall('$HOME/.cache/concord-nsis')" || true
  fi
  if [[ -f "$NSIS_DIR/makensis.exe" ]]; then
    echo "==> building Windows installer (Concord-Setup-$VERSION.exe)"
    WINEDEBUG=-all wine "$NSIS_DIR/makensis.exe" \
      "/DVERSION=$VERSION" \
      "/DEXE=$(winepath -w "dist-release/concord-desktop-windows-$VERSION.exe")" \
      "/DICON=$(winepath -w build/windows/icon.ico)" \
      "/DOUT=$(winepath -w "dist-release/Concord-Setup-$VERSION.exe")" \
      "$(winepath -w build/windows/installer.nsi)" >/dev/null
    ls -lh "dist-release/Concord-Setup-$VERSION.exe"
  else
    echo "!!! NSIS unavailable — release will lack the Windows installer" >&2
  fi
else
  echo "!!! wine unavailable — release will lack the Windows installer" >&2
fi

# Android: ship the sideload APK when it's been built for THIS version
# (`make android-app VERSION=vX.Y.Z MOBILE_VERSION_CODE=<monotonic int>`).
# Missing APK is a loud warning, not a failure — but don't let Android
# silently fall out of releases again.
APK="apps/mobile/android/app/build/outputs/apk/release/concord-${VERSION#v}-android.apk"
if [[ -f "$APK" ]]; then
  cp "$APK" dist-release/
  echo "==> including $(basename "$APK")"
else
  echo "!!! NO ANDROID APK for $VERSION — run: make android-app VERSION=$VERSION MOBILE_VERSION_CODE=<n>" >&2
fi

cp WINDOWS.md dist-release/
(cd dist-release && sha256sum $(ls | grep -v '^SHA256SUMS$') > SHA256SUMS)

# Sign the manifest. SHA256SUMS covers every asset, so one signature
# authenticates the whole release — and clients verify the signature BEFORE
# trusting any hash in it. A release published without this is one that
# signature-enforcing builds will refuse, so fail loudly rather than shipping
# something nobody can install.
if go run ./cmd/releasekey sign dist-release/SHA256SUMS; then
  echo "==> manifest signed"
else
  echo "!!! could not sign SHA256SUMS — run 'make release-keygen' first." >&2
  echo "!!! publishing unsigned would leave signed builds unable to update." >&2
  exit 1
fi
echo && ls -lh dist-release/ && echo

echo "==> publishing $VERSION to $RELEASE_REPO"
# Create the release EMPTY, then upload assets one-by-one with retries.
# (`gh release create` with assets rolls the whole release back if any single
# upload hiccups — and large uploads DO hiccup with transient TLS resets.)
if ! gh release view "$VERSION" --repo "$RELEASE_REPO" >/dev/null 2>&1; then
  if [[ -n "$NOTES_FILE" ]]; then
    gh release create "$VERSION" --repo "$RELEASE_REPO" --title "Concord $VERSION" --notes-file "$NOTES_FILE"
  else
    gh release create "$VERSION" --repo "$RELEASE_REPO" --title "Concord $VERSION" --notes "Concord $VERSION"
  fi
fi
for f in dist-release/*; do
  ok=""
  for try in 1 2 3 4 5; do
    if gh release upload "$VERSION" "$f" --repo "$RELEASE_REPO" --clobber; then
      ok=1
      echo "uploaded: $(basename "$f")"
      break
    fi
    echo "upload hiccup ($try/5): $(basename "$f") — retrying" >&2
    sleep 2
  done
  [[ -n "$ok" ]] || { echo "error: gave up on $(basename "$f")" >&2; exit 1; }
done

echo "done: https://github.com/$RELEASE_REPO/releases/tag/$VERSION"
