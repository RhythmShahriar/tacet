#!/bin/sh
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Checks every tracked source file carries the owner's header and that no
# file credits an AI tool. A line that has to name such a pattern (the
# detector itself, its tests) ends with the marker brand:allow.
set -eu
cd "$(dirname "$0")/.."

status=0
files=$(git ls-files 2>/dev/null || find . -type f -not -path './.git/*')

for f in $files; do
	case $f in
	testbed/fixtures/*) continue ;;
	*.go | *.yml | *.yaml | *.sh | *.ps1 | .githooks/*) ;;
	*) continue ;;
	esac
	top=$(head -n 5 "$f")
	case $top in
	*"Copyright (c) 20"*" Rhythm Shahriar <me@rhy.io>"*) ;;
	*) echo "$f: missing the copyright header" >&2; status=1; continue ;;
	esac
	case $top in
	*"SPDX-License-Identifier: MIT"*) ;;
	*) echo "$f: missing SPDX-License-Identifier: MIT" >&2; status=1 ;;
	esac
done

# AI credit anywhere in the tree, the fixtures included.
pattern='co-authored-by:.*(claude|anthropic|copilot|openai)|generated (with|by) \[?(claude|copilot|chatgpt)|noreply@anthropic\.com' # brand:allow
for f in $files; do
	[ -f "$f" ] || continue
	if grep -EinI "$pattern" "$f" | grep -v 'brand:allow' >/dev/null; then
		grep -EinI "$pattern" "$f" | grep -v 'brand:allow' | sed "s|^|$f:|" >&2
		status=1
	fi
done

[ $status -eq 0 ] && echo "brand: ok"
exit $status
