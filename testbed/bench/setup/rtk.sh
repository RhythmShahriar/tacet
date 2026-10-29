#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Mode "rtk": rtk-ai/rtk 1.1.0 (the default edition), the release binary
# checked against its pinned sha256, then "rtk init -g" as its README says.
# The PreToolUse hook rewrites Bash commands through rtk; no MCP tools.
#
#   rtk.sh [install]
set -euo pipefail
# shellcheck source=SCRIPTDIR/_common.sh
. "$(dirname "$0")/_common.sh"
[ "${1:-install}" = install ] || exit 0
version=1.1.0
sha=9454ad1b9489daa57ab0a9f4ebe2faaf4dc82b8a0f26c9e5135151abd7c9378d
mode_env rtk
link_auth
fetch "https://github.com/rtk-ai/rtk/releases/download/v$version/rtk-x86_64-unknown-linux-musl.tar.gz" "$sha" "$S/rtk.tar.gz"
tar -xzf "$S/rtk.tar.gz" -C "$S/bin" rtk
rm -f "$S/rtk.tar.gz"
# HOME points at the mode's state, so -g writes there, not to ~/.claude.
rtk init -g --auto-patch --no-trust-filters </dev/null || die "rtk init -g failed"
grep -q rtk "$CLAUDE_CONFIG_DIR/settings.json" || die "rtk init did not add its hook"
finish "rtk $(rtk --version | cut -d' ' -f2)"
