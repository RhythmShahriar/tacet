#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Turns compare.sh results into Markdown: one table per task, one row per
# mode, sorted by cost per correct answer (total cost of the runs divided
# by the runs that passed). Crashed runs count as failures and their
# cost, when known, still counts.
#
#   table.sh [results.jsonl ...]   (default: the newest file in results/)
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
if [ $# -eq 0 ]; then
	newest=$(find "$here/results" -maxdepth 1 -name '*.jsonl' 2>/dev/null | sort | tail -n 1)
	[ -n "$newest" ] || { echo "table.sh: no results yet" >&2; exit 1; }
	set -- "$newest"
fi
cat "$@" | jq -rs '
	def r(n): . * pow(10; n) | round / pow(10; n);
	def avg(f): (map(f | select(. != null)) | if length > 0 then add / length else null end);
	def num: if . == null then "n/a" elif . >= 1000 then (. / 1000 | r(1) | tostring) + "k" else (r(1) | tostring) end;
	def usd: if . == null then "n/a" else "$" + (r(4) | tostring) end;
	group_by(.task) | map(
		.[0].task as $task |
		(group_by(.mode) | map({
			mode: .[0].mode, runs: length,
			passed: (map(select(.pass)) | length),
			cost: (map(.cost // 0) | add),
			billed: avg(.billed), output: avg(.output), turns: avg(.turns),
			errors: (map(select(.error)) | length),
			version: .[0].tool_version
		} | .per_correct = (if .passed > 0 then .cost / .passed else null end))
		| sort_by(.per_correct // 1e9)) as $rows |
		"### \($task)\n",
		"| Mode | Cost per correct answer | Pass | Billed tokens (mean) | Output tokens (mean) | Turns (mean) | Cost (mean) | Version |",
		"|---|---|---|---|---|---|---|---|",
		($rows[] | "| \(.mode) | \(.per_correct | usd) | \(.passed)/\(.runs)\(if .errors > 0 then " (\(.errors) crashed)" else "" end) | \(.billed | num) | \(.output | num) | \(.turns | if . == null then "n/a" else r(1) end) | \(.cost / .runs | usd) | \(.version) |"),
		""
	) | .[]'

# Totals across tasks, per mode.
cat "$@" | jq -rs '
	def r(n): . * pow(10; n) | round / pow(10; n);
	"### All tasks\n",
	"| Mode | Cost per correct answer | Pass | Total cost |",
	"|---|---|---|---|",
	(group_by(.mode) | map({
		mode: .[0].mode, runs: length,
		passed: (map(select(.pass)) | length),
		cost: (map(.cost // 0) | add)
	} | .per_correct = (if .passed > 0 then .cost / .passed else null end))
	| sort_by(.per_correct // 1e9) | .[]
	| "| \(.mode) | \(if .per_correct == null then "n/a" else "$" + (.per_correct | r(4) | tostring) end) | \(.passed)/\(.runs) | $\(.cost | r(2)) |")'
