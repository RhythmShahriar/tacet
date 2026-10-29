#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# slug, a small Go package, halfway through a change: the tests for
# SlugifyWith are committed, options.go is uncommitted with the separator
# done and the length cap still a TODO. SESSION.md is the note tacet
# writes at session end, in its format, so the next session can resume.
#
#   setup.sh <project-dir>
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
p=$1
g() { git -C "$p" -c user.name=bench -c user.email=bench@example.invalid "$@"; }
mkdir -p "$(dirname "$p")"
cp -a "$here/project" "$p"
"$here/../_doc/git.sh" "$p"
cp "$here/change/slugwith_test.go.txt" "$p/slugwith_test.go"
g add slugwith_test.go
g commit -q -m "test: cases for SlugifyWith with a separator and a length cap"
cp "$here/change/options.go.txt" "$p/options.go"

# The header must match handoff.Header in tacet, or tacet ignores the note.
cat >"$p/SESSION.md" <<NOTE
<!-- tacet · SESSION.md is written by tacet at compact and session end; edits are kept until the next write -->
# Session handoff

Updated $(date -u '+%Y-%m-%d %H:%M UTC') on branch \`main\`. Session \`5f3c9a1e\`.

## Goal
Add SlugifyWith(s, Options) to the slug package: Options.Sep picks the separator and Options.MaxLen caps the length, cutting after the last whole word that fits. Keep Slugify as it is. Document SlugifyWith in the README when it works.

## Latest asks
- the cases are in slugwith_test.go, make them pass
- separator first, then the length cap

## Uncommitted changes
- \`?? options.go\`

## Recent commits
- $(g log -1 --format='%h %s')
- $(g log -1 --skip 1 --format='%h %s')

## Where it stopped
> Options and SlugifyWith are in options.go. The separator works.
> Next: MaxLen in SlugifyWith (the TODO), including a first word longer
> than MaxLen, cut without splitting a character. Then go test ./...
> and add a short SlugifyWith example to README.md.
NOTE
