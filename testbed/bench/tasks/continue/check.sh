#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Passes when the committed tests are untouched, go test passes and the
# README documents SlugifyWith, the last step the handoff note names.
#
#   check.sh <project-dir> <answer-file>
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
cd "$1"
cmp -s "$here/change/slugwith_test.go.txt" slugwith_test.go || { echo "check: the tests were changed" >&2; exit 1; }
command -v go >/dev/null || PATH=/usr/local/go/bin:$PATH
go test ./... >/dev/null 2>&1 || { echo "check: go test fails" >&2; exit 1; }
grep -q SlugifyWith README.md || { echo "check: README.md does not mention SlugifyWith" >&2; exit 1; }
