#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# The known finish, for selftest.sh only: MaxLen done and the README updated.
#
#   solution.sh <project-dir>
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
cp "$here/solution-options.go.txt" "$1/options.go"
# shellcheck disable=SC2016 # backticks are Markdown here
printf '\n%s\n' 'SlugifyWith takes Options: `SlugifyWith("The quick brown fox", Options{MaxLen: 14})` gives `the-quick`.' >>"$1/README.md"
