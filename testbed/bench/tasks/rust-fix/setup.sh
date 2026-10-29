#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# spans, a small cargo project with one seeded bug: one test fails.
#
#   setup.sh <project-dir>
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$(dirname "$1")"
cp -a "$here/project" "$1"
"$here/../_doc/git.sh" "$1"
