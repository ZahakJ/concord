#!/bin/sh
# Point the project site's download buttons at a release.
#
#   scripts/stamp-site.sh v0.59.0
#
# Release asset names carry their version (the in-app updater needs that), so
# a link that always means "the latest" does not exist for a single file. The
# site names the version instead, and this rewrites every such link, plus the
# visible version label, in docs/index.html. publish-release.sh refuses to
# publish a tag the site has not been stamped for.
set -eu
cd "$(dirname "$0")/.."
V=${1:?usage: stamp-site.sh vX.Y.Z}
case "$V" in v[0-9]*.[0-9]*.[0-9]*) ;; *) echo "stamp-site.sh: version must look like v0.9.0" >&2; exit 1 ;; esac
N=${V#v}
SEM='v[0-9]+\.[0-9]+\.[0-9]+'
sed -E -i \
  -e "s#releases/download/$SEM/#releases/download/$V/#g" \
  -e "/releases\/download\//s#-$SEM(\.exe)?\"#-$V\1\"#g" \
  -e "/releases\/download\//s#concord-[0-9]+\.[0-9]+\.[0-9]+-android\.apk#concord-$N-android.apk#g" \
  -e "s#(data-version>)$SEM<#\1$V<#g" \
  docs/index.html
n=$(grep -c "releases/download/$V/" docs/index.html || true)
[ "$n" -gt 0 ] || { echo "stamp-site.sh: no download links found in docs/index.html" >&2; exit 1; }
echo "docs/index.html: $n download links now point at $V"
