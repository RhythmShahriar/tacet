#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# A folder with one 40+ page PDF (RFC 6749 laid out as a guide).
#
#   setup.sh <project-dir>
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$1"
"$here/../_doc/build.sh" pdf "$1/oauth-guide.pdf"
"$here/../_doc/git.sh" "$1"
