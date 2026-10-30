#!/bin/sh
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Builds the npm packages from GoReleaser's release archives:
#   scripts/npm-pack.sh <version> <dir> [out]
# <dir> holds tacet_<os>_<arch>.tar.gz (.zip on Windows) and checksums.txt,
# as in dist/ after goreleaser or a gh release download. Every archive must
# match its line in checksums.txt. Writes the single @rhythmsh/tacet
# package under [out] (default npm/out/tacet). It holds no binary: its
# launcher downloads this platform's archive from the release on first
# run and checks it against the SHA-256 written into package.json here.
set -eu

die() {
	echo "npm-pack: $*" >&2
	exit 1
}

[ $# -ge 2 ] || die "usage: scripts/npm-pack.sh <version> <dir> [out]"
version=${1#v}
src=$2
root=$(cd "$(dirname "$0")/.." && pwd)
out=${3:-$root/npm/out}

printf '%s' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' ||
	die "version '$version' is not semver"
[ -f "$src/checksums.txt" ] || die "$src/checksums.txt not found"

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

rm -rf "$out"
main=$out/tacet
mkdir -p "$main/bin"
archives=
# npm platform and arch, then GoReleaser's names for the same target.
for t in darwin:arm64:darwin:arm64 darwin:x64:darwin:amd64 \
	linux:arm64:linux:arm64 linux:x64:linux:amd64 \
	win32:arm64:windows:arm64 win32:x64:windows:amd64; do
	IFS=: read -r platform arch goos goarch <<-END
		$t
	END
	if [ "$goos" = windows ]; then
		archive=tacet_${goos}_${goarch}.zip
	else
		archive=tacet_${goos}_${goarch}.tar.gz
	fi
	[ -f "$src/$archive" ] || die "$src/$archive not found"
	want=$(awk -v f="$archive" '$2 == f || $2 == "*" f { print $1 }' "$src/checksums.txt")
	[ -n "$want" ] || die "$archive has no line in checksums.txt"
	got=$(sha256 "$src/$archive")
	[ "$got" = "$want" ] || die "$archive checksum mismatch: got $got, want $want"
	archives="$archives${archives:+,}\"$platform-$arch\":{\"file\":\"$archive\",\"sha256\":\"$got\"}"
done
cp "$root/npm/tacet/bin/tacet.js" "$main/bin/tacet.js"
chmod 755 "$main/bin/tacet.js"
cp "$root/npm/tacet/README.md" "$main/README.md"
cp "$root/LICENSE" "$main/LICENSE"
jq --arg v "$version" --argjson a "{$archives}" '.version = $v | .tacet = {archives: $a}' \
	"$root/npm/tacet/package.json" >"$main/package.json"
echo "npm-pack: @rhythmsh/tacet $version"
