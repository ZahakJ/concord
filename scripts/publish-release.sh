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
DIST_REPO="ZahakJ/concord-dist"

[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "error: version must look like v0.9.0" >&2; exit 1; }
command -v gh >/dev/null || { echo "error: needs the gh CLI, authenticated" >&2; exit 1; }

echo "==> building web release binaries ($VERSION)"
make release VERSION="$VERSION"

# Native Linux desktop app — buildable right here when the WebView dev package
# is present; skipped (with a note) otherwise.
if pkg-config --exists webkit2gtk-4.1 2>/dev/null; then
  echo "==> building native Linux desktop app"
  go build -tags "wails desktop production webkit2_41" -trimpath \
    -ldflags "-X github.com/zahak/concord/internal/version.Version=$VERSION" \
    -o "dist-release/concord-desktop-linux-$VERSION" .
else
  echo "==> webkit2gtk-4.1 not found; skipping the native Linux desktop build"
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
echo && ls -lh dist-release/ && echo

echo "==> publishing $VERSION to $DIST_REPO"
# Create the release EMPTY, then upload assets one-by-one with retries.
# (`gh release create` with assets rolls the whole release back if any single
# upload hiccups — and large uploads DO hiccup with transient TLS resets.)
if ! gh release view "$VERSION" --repo "$DIST_REPO" >/dev/null 2>&1; then
  if [[ -n "$NOTES_FILE" ]]; then
    gh release create "$VERSION" --repo "$DIST_REPO" --title "Concord $VERSION" --notes-file "$NOTES_FILE"
  else
    gh release create "$VERSION" --repo "$DIST_REPO" --title "Concord $VERSION" --notes "Concord $VERSION"
  fi
fi
for f in dist-release/*; do
  ok=""
  for try in 1 2 3 4 5; do
    if gh release upload "$VERSION" "$f" --repo "$DIST_REPO" --clobber; then
      ok=1
      echo "uploaded: $(basename "$f")"
      break
    fi
    echo "upload hiccup ($try/5): $(basename "$f") — retrying" >&2
    sleep 2
  done
  [[ -n "$ok" ]] || { echo "error: gave up on $(basename "$f")" >&2; exit 1; }
done

echo "done: https://github.com/$DIST_REPO/releases/tag/$VERSION"
