#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# A folder with one Word file with real heading styles (RFC 6749 as a guide).
#
#   setup.sh <project-dir>
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$1"
"$here/../_doc/build.sh" docx "$1/oauth-guide.docx"
"$here/../_doc/git.sh" "$1"
