#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Mode "concise": plain Claude Code with its built-in Concise output
# style and no tacet, a baseline for shorter answers. Concise
# needs Claude Code v2.1.237 or later; an older one falls back to Default.
#
#   concise.sh [install]          the login, nothing else
#   concise.sh prepare <project>  outputStyle Concise in settings.local.json
set -euo pipefail
# shellcheck source=SCRIPTDIR/_common.sh
. "$(dirname "$0")/_common.sh"
case ${1:-install} in
install)
	mode_env concise
	link_auth
	finish "claude $(claude --version | cut -d' ' -f1) Concise"
	;;
prepare)
	f=$2/.claude/settings.local.json
	mkdir -p "$(dirname "$f")"
	[ -s "$f" ] || echo '{}' >"$f"
	jq '.outputStyle = "Concise"' "$f" >"$f.tmp" && mv "$f.tmp" "$f"
	;;
esac
