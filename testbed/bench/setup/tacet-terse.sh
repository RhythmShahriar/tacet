#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Mode "tacet-terse": tacet as in tacet.sh plus the terse output style
# (tacet on terse), to measure whether it cuts output tokens.
#
#   tacet.sh [install]          build tacet and install the plugin once
#   tacet.sh prepare <project>  tacet init --yes in a task's project
set -euo pipefail
# shellcheck source=SCRIPTDIR/_common.sh
. "$(dirname "$0")/_common.sh"
repo=$(cd "$BENCH/../.." && pwd)
case ${1:-install} in
install)
	command -v go >/dev/null || PATH=/usr/local/go/bin:$PATH
	ver=$(git -C "$repo" describe --tags --always --dirty 2>/dev/null || echo dev)
	(cd "$repo" && GOWORK=off go build -tags gotreesitter_no_copyleft \
		-ldflags "-X main.version=${ver#v}" -o "$STATE_ROOT/tacet-terse/bin/tacet" ./cmd/tacet) ||
		die "go build failed"
	mode_env tacet-terse
	link_auth
	# init once in a scratch project installs the plugin into this mode.
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	git -C "$tmp" init -q
	(cd "$tmp" && tacet init --yes >/dev/null) || die "tacet init failed"
	claude plugin list 2>/dev/null | grep -q tacet || die "the tacet plugin did not install"
	finish "tacet $ver" mcp__plugin_tacet_tacet
	;;
prepare)
	mode_env tacet-terse
	(cd "$2" && tacet init --yes --no-plugin >/dev/null && tacet on terse >/dev/null) || die "tacet init failed in $2"
	;;
esac
