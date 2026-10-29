#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Checks every task without a model: setup.sh builds the project, check.sh
# fails on the untouched project with an empty answer (negative) and
# passes with the known answer or fix (positive). Costs nothing.
#
#   selftest.sh [task ...]   (default: every task)
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
if [ $# -eq 0 ]; then
	mapfile -t all < <(cd "$here/tasks" && find . -mindepth 1 -maxdepth 1 -type d ! -name '_*' | sed 's|^\./||' | sort)
	set -- "${all[@]}"
fi
status=0
for t in "$@"; do
	d=$here/tasks/$t
	work=$(mktemp -d)
	if ! "$d/setup.sh" "$work/p" >"$work/log" 2>&1; then
		echo "FAIL $t: setup.sh"; sed 's/^/  /' "$work/log"; status=1; rm -rf "$work"; continue
	fi
	: >"$work/empty"
	if "$d/check.sh" "$work/p" "$work/empty" >/dev/null 2>&1; then
		echo "FAIL $t: check.sh passes before any work"; status=1
	else
		[ ! -x "$d/solution.sh" ] || "$d/solution.sh" "$work/p"
		answer=$work/empty
		[ ! -f "$d/answer.txt" ] || answer=$d/answer.txt
		if "$d/check.sh" "$work/p" "$answer" >"$work/log" 2>&1; then
			echo "ok   $t"
		else
			echo "FAIL $t: check.sh rejects the known answer"; sed 's/^/  /' "$work/log"; status=1
		fi
	fi
	chmod -R u+w "$work" 2>/dev/null || true
	rm -rf "$work"
done
exit $status
