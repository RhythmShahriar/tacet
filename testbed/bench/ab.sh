#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# A/B one scenario: the same prompt through claude -p without tacet and
# with tacet's plugin, REPS times each, alternating so cache state is
# shared fairly. Prints one JSON line per run with the billed tokens,
# turns, cost and whether the answer matched.
#
#   ab.sh <project-dir> <plugin-dir> <prompt> <answer-regex> [reps] [model]
set -euo pipefail
proj=$1 plugin=$2 prompt=$3 answer=$4 reps=${5:-3} model=${6:-haiku}
out=$(mktemp)
trap 'rm -f "$out"' EXIT
cd "$proj"
for ((i = 1; i <= reps; i++)); do
	for mode in off on; do
		# A unique id first, so no run reads another run's cached document:
		# every run pays for a first read, as a real session does.
		nonce="[bench run $RANDOM$RANDOM]"
		extra=()
		# ON_ARGS replaces the plugin for features that are settings, not hooks.
		if [ "$mode" = on ]; then
			if [ -n "${ON_ARGS:-}" ]; then read -r -a extra <<<"$ON_ARGS"; else extra=(--plugin-dir "$plugin"); fi
		fi
		if ! timeout 400 claude -p --model "$model" "${extra[@]}" --allowedTools "Read Grep Glob Bash ${EXTRA_TOOLS:-}" \
			--max-turns 12 --output-format json "$nonce $prompt" >"$out" 2>/dev/null; then
			echo "{\"mode\":\"$mode\",\"error\":true}"
			continue
		fi
		jq -c --arg mode "$mode" --arg re "$answer" '{
			mode: $mode,
			total: (.usage.input_tokens + .usage.cache_creation_input_tokens + .usage.cache_read_input_tokens),
			written: .usage.cache_creation_input_tokens,
			output: .usage.output_tokens,
			turns: .num_turns,
			cost: .total_cost_usd,
			correct: (.result | test($re; "ip"))
		}' "$out"
	done
done
