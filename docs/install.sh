#!/bin/sh
# Install the latest Concord web build on Linux or macOS.
#
#   curl -fsSL https://zahakj.github.io/concord/install.sh | sh
#
# Downloads the single-file binary for this OS and CPU from the latest GitHub
# release, checks it against the release's SHA256SUMS, and puts it at
# $PREFIX/concord (default ~/.local/bin). Nothing runs as root. Running it
# again updates in place; so does the app itself.
set -eu

REPO=ZahakJ/concord
PREFIX=${PREFIX:-"$HOME/.local/bin"}

die() { echo "install.sh: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "needs $1"; }
need curl

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=macos ;;
  *) die "unsupported OS $(uname -s); Windows and Android downloads are at https://github.com/$REPO/releases/latest" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "unsupported CPU $(uname -m)" ;;
esac
[ "$os" = macos ] && { [ "$arch" = amd64 ] && arch=intel; }

# /releases/latest redirects to /releases/tag/vX.Y.Z; no API token needed.
tag=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")
tag=${tag##*/}
case "$tag" in v[0-9]*) ;; *) die "could not work out the latest release" ;; esac

asset="concord-$os-$arch-$tag"
base="https://github.com/$REPO/releases/download/$tag"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Concord $tag for $os/$arch"
curl -fL --progress-bar -o "$tmp/$asset" "$base/$asset"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"

want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1 }' "$tmp/SHA256SUMS")
[ -n "$want" ] || die "$asset is not listed in SHA256SUMS"
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
else
  got=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)
fi
[ "$got" = "$want" ] || die "checksum mismatch for $asset"

mkdir -p "$PREFIX"
chmod +x "$tmp/$asset"
mv "$tmp/$asset" "$PREFIX/concord"
echo "installed $PREFIX/concord"
case ":$PATH:" in *":$PREFIX:"*) ;; *) echo "note: $PREFIX is not on your PATH" ;; esac
echo "run: concord   (your browser opens on the app)"
