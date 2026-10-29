#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# click 8.1.7 (71 Python files), shallow at a pinned commit.
#
#   setup.sh <project-dir>
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
"$here/../_doc/clone.sh" pallets/click 874ca2bc1c30d93a4ac6e36a15ed685eafe89097 "$1"
