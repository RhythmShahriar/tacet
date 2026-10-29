#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Mode "claude-mem": thedotmack/claude-mem 15.6.0 through its README's
# installer, npx claude-mem install, pinned to the version. That path also
# sets up Bun and the hook launcher, which the bare /plugin install lacks.
# Sign-in is skipped (CLAUDE_MEM_ONLINE_OPTIN=false) as the README allows
# for non-interactive installs; the memory provider is the default
# (claude, the person's own login), whose background calls are not in
# claude -p's cost report. Telemetry is turned off.
#
#   claude-mem.sh [install]
set -euo pipefail
# shellcheck source=SCRIPTDIR/_common.sh
. "$(dirname "$0")/_common.sh"
[ "${1:-install}" = install ] || exit 0
version=15.6.0
export CLAUDE_MEM_ONLINE_OPTIN=false
mode_env claude-mem
link_auth
cd "$S"
npx -y "claude-mem@$version" install --provider claude --no-auto-start </dev/null || die "npx claude-mem install failed"
npx -y "claude-mem@$version" telemetry disable </dev/null >/dev/null 2>&1 || true
command -v claude-mem >/dev/null || die "the claude-mem hook launcher is not on PATH"
claude plugin list 2>/dev/null | grep -q claude-mem || die "the claude-mem plugin did not install"
finish "claude-mem $version" "$(mcp_rules claude-mem)"
