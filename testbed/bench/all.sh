#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# The full head-to-head: every task through every mode, REPS times. Reps
# are interleaved (rep 1 of everything, then rep 2, ...) and the mode
# order rotates each rep, so no mode always runs first or last.
#
#   all.sh [reps] [task ...]          (default: 3 reps, every task)
#   BENCH_MODES="none tacet" all.sh   (only some modes)
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=SCRIPTDIR/setup/_common.sh
. "$here/setup/_common.sh"
reps=${1:-3}
[ $# -eq 0 ] || shift
if [ $# -eq 0 ]; then
	mapfile -t tasks < <(cd "$here/tasks" && find . -mindepth 1 -maxdepth 1 -type d ! -name '_*' | sed 's|^\./||' | sort)
else
	tasks=("$@")
fi
read -r -a modes <<<"${BENCH_MODES:-$MODES}"
n=${#modes[@]}
for ((rep = 1; rep <= reps; rep++)); do
	for task in "${tasks[@]}"; do
		for ((i = 0; i < n; i++)); do
			mode=${modes[$(((i + rep - 1) % n))]}
			BENCH_REP=$rep "$here/compare.sh" "$task" "$mode" 1 ||
				echo "bench: $task $mode rep $rep failed to run" >&2
		done
	done
done
