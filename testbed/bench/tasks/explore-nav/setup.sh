#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# caddy v2.9.1 (279 Go files), shallow at a pinned commit: the same
# clone as go-nav.
#
#   setup.sh <project-dir>
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
"$here/../_doc/clone.sh" caddyserver/caddy 0e570e0cc717f02cf3800ae741df70cd074c7275 "$1"
