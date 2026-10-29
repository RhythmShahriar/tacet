#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Mode "caveman": JuliusBrussee/caveman v4.0.1 as a Claude Code plugin
# (README: claude plugin marketplace add + claude plugin install), from a
# checkout of the tag, so the version can't drift.
#
#   caveman.sh [install]
set -euo pipefail
# shellcheck source=SCRIPTDIR/_common.sh
. "$(dirname "$0")/_common.sh"
[ "${1:-install}" = install ] || exit 0
tag=v4.0.1
mode_env caveman
link_auth
git_at https://github.com/JuliusBrussee/caveman "$tag" "$S/src"
marketplace_plugin "$S/src" caveman@caveman
finish "caveman $tag" "$(mcp_rules caveman)"
