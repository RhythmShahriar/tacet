#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Mode "none": plain Claude Code, nothing installed. The baseline.
#
#   none.sh [install]
set -euo pipefail
# shellcheck source=SCRIPTDIR/_common.sh
. "$(dirname "$0")/_common.sh"
[ "${1:-install}" = install ] || exit 0
mode_env none
link_auth
finish "claude $(claude --version | cut -d' ' -f1)"
