#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Copies a public repository at a pinned commit into <dir>. The shallow
# clone is fetched once into cache/git/ and copied from there.
#
#   clone.sh <github owner/repo> <commit sha> <dir>
set -euo pipefail
repo=$1 sha=$2 dir=$3
here=$(cd "$(dirname "$0")" && pwd)
root=${BENCH_CACHE:-$here/../../cache}
# A go.mod keeps tacet's go ./... out of the cached clones.
mkdir -p "$root"
[ -f "$root/go.mod" ] || printf 'module benchscratch\n' >"$root/go.mod"
cache=$root/git/${repo//\//-}-$sha
if [ ! -d "$cache/.git" ]; then
	rm -rf "$cache.part"
	mkdir -p "$cache.part"
	git -C "$cache.part" init -q
	git -C "$cache.part" fetch -q --depth 1 "https://github.com/$repo" "$sha"
	git -C "$cache.part" -c advice.detachedHead=false checkout -q FETCH_HEAD
	[ "$(git -C "$cache.part" rev-parse HEAD)" = "$sha" ] || { echo "clone.sh: $repo is not at $sha" >&2; exit 1; }
	mv "$cache.part" "$cache"
fi
mkdir -p "$(dirname "$dir")"
cp -a "$cache" "$dir"
