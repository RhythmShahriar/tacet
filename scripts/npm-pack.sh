#!/bin/sh
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Builds the npm packages from GoReleaser's release archives:
#   scripts/npm-pack.sh <version> <dir> [out]
# <dir> holds tacet_<os>_<arch>.tar.gz (.zip on Windows) and checksums.txt,
# as in dist/ after goreleaser or a gh release download. Every archive must
# match its line in checksums.txt. Writes one folder per package under
# [out] (default npm/out): six @rhythmsh/tacet-<platform>-<arch> packages
# and @rhythmsh/tacet, all at <version>.
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

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

rm -rf "$out"
mkdir -p "$out"

# npm platform and arch, then GoReleaser's names for the same target.
for t in darwin:arm64:darwin:arm64 darwin:x64:darwin:amd64 \
	linux:arm64:linux:arm64 linux:x64:linux:amd64 \
	win32:arm64:windows:arm64 win32:x64:windows:amd64; do
	IFS=: read -r platform arch goos goarch <<-END
		$t
	END
	if [ "$goos" = windows ]; then
		archive=tacet_${goos}_${goarch}.zip
		binary=tacet.exe
	else
		archive=tacet_${goos}_${goarch}.tar.gz
		binary=tacet
	fi
	[ -f "$src/$archive" ] || die "$src/$archive not found"

	want=$(awk -v f="$archive" '$2 == f || $2 == "*" f { print $1 }' "$src/checksums.txt")
	[ -n "$want" ] || die "$archive has no line in checksums.txt"
	got=$(sha256 "$src/$archive")
	[ "$got" = "$want" ] || die "$archive checksum mismatch: got $got, want $want"

	x=$work/$platform-$arch
	mkdir -p "$x"
	if [ "$goos" = windows ]; then
		unzip -q "$src/$archive" -d "$x"
	else
		tar -xzf "$src/$archive" -C "$x"
	fi
	[ -f "$x/$binary" ] || die "$archive holds no $binary"

	pkg=$out/tacet-$platform-$arch
	mkdir -p "$pkg/bin"
	cp "$x/$binary" "$pkg/bin/$binary"
	chmod 755 "$pkg/bin/$binary"
	cp "$x/LICENSE" "$pkg/LICENSE"
	cp -R "$x/THIRD_PARTY" "$pkg/THIRD_PARTY"
	for f in package.json README.md; do
		sed -e "s/@PLATFORM@/$platform/g" -e "s/@ARCH@/$arch/g" \
			-e "s/@VERSION@/$version/g" -e "s/@BINARY@/$binary/g" \
			"$root/npm/platform/$f" >"$pkg/$f"
	done
	echo "npm-pack: @rhythmsh/tacet-$platform-$arch $version"
done

main=$out/tacet
mkdir -p "$main/bin"
cp "$root/npm/tacet/bin/tacet.js" "$main/bin/tacet.js"
chmod 755 "$main/bin/tacet.js"
cp "$root/npm/tacet/README.md" "$main/README.md"
cp "$root/LICENSE" "$main/LICENSE"
# The template's own version and every optional dependency are 0.0.0.
sed "s/\"0\.0\.0\"/\"$version\"/g" "$root/npm/tacet/package.json" >"$main/package.json"
grep -q '"0\.0\.0"' "$main/package.json" && die "a 0.0.0 version is left in package.json"
echo "npm-pack: @rhythmsh/tacet $version"
