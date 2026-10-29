#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Mode "graphify": Graphify-Labs/graphify 0.9.97 (PyPI graphifyy) as its
# README says: uv tool install graphifyy, graphify install (the skill),
# then per project the always-on setup, graphify claude install (a
# CLAUDE.md section plus PreToolUse hooks), on a graph built beforehand.
#
# The graph is built with graphify update . (code only, no LLM) and
# GRAPHIFY_BACKEND=none, so building it costs no tokens and isn't counted,
# the same as tacet's own index. Documents get no semantic pass: that
# would need an LLM call outside the measured run.
#
#   graphify.sh [install]          uv, graphify and its skill
#   graphify.sh prepare <project>  build the graph and the always-on setup
set -euo pipefail
# shellcheck source=SCRIPTDIR/_common.sh
. "$(dirname "$0")/_common.sh"
version=0.9.97
mode_env graphify
uv_setup
case ${1:-install} in
install)
	link_auth
	uv tool install --force "graphifyy==$version" >/dev/null || die "uv tool install graphifyy failed"
	graphify install >/dev/null || die "graphify install failed"
	[ -d "$CLAUDE_CONFIG_DIR/skills/graphify" ] || die "graphify install did not add its skill"
	finish "graphify $version"
	;;
prepare)
	cd "$2"
	export GRAPHIFY_BACKEND=none
	graphify update . || echo "bench: graphify update found nothing to index in $2" >&2
	graphify claude install || die "graphify claude install failed in $2"
	;;
esac
