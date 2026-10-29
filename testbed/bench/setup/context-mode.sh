#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Mode "context-mode": mksglu/context-mode v1.0.169 as a Claude Code
# plugin (README: /plugin marketplace add + /plugin install), from a
# checkout of the tag. Needs Node.js 22.5 or newer.
#
#   context-mode.sh [install]
set -euo pipefail
# shellcheck source=SCRIPTDIR/_common.sh
. "$(dirname "$0")/_common.sh"
[ "${1:-install}" = install ] || exit 0
tag=v1.0.169
mode_env context-mode
link_auth
git_at https://github.com/mksglu/context-mode "$tag" "$S/src"
marketplace_plugin "$S/src" context-mode@context-mode
finish "context-mode $tag" "$(mcp_rules context-mode)"
