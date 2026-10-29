#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Head-to-head: runs one task through claude -p with one tool installed
# (or none) REPS times and appends one JSON line per run to
# results/<date>.jsonl. table.sh turns those lines into the Markdown table.
#
#   compare.sh <task> <mode> [reps]
#
#   modes: none tacet rtk caveman context-mode claude-mem graphify serena
#   tasks: the folders under tasks/ (pdf-part, go-nav, py-fix, ...)
#
# Environment:
#   BENCH_MODEL      model for claude -p (default haiku)
#   BENCH_WARM       1 (default) warms the cache before each run; 0 skips it
#   BENCH_MAX_TURNS  turn cap (default 30, a task can lower or raise it)
#   BENCH_TIMEOUT    seconds per run (default 600)
#   BENCH_REP        number of the first rep (default 1), for interleaving
#   BENCH_KEEP=1     keep each run's project folder and raw output
#   BENCH_RESULTS    results folder (default results/)
#
# Each mode is set up once by setup/<mode>.sh into state/<mode>/; delete
# that folder to set it up again.
set -euo pipefail
# shellcheck source=SCRIPTDIR/setup/_common.sh
. "$(dirname "$0")/setup/_common.sh"

[ $# -ge 2 ] || { sed -n '5,22p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2; }
task=$1 mode=$2 reps=${3:-1}
model=${BENCH_MODEL:-haiku}
timeout=${BENCH_TIMEOUT:-600}
first=${BENCH_REP:-1}
results=${BENCH_RESULTS:-$BENCH/results}
tdir=$BENCH/tasks/$task
[ -f "$tdir/prompt.txt" ] || die "no such task: $task (see tasks/)"
valid_mode "$mode" || die "no such mode: $mode (one of: $MODES)"
command -v jq >/dev/null || die "jq is needed"
command -v claude >/dev/null || die "the claude command is needed"

# Set up the mode once, from scratch again when its setup script changes.
S=$STATE_ROOT/$mode
if [ ! -f "$S/.ready" ] || [ "$BENCH/setup/$mode.sh" -nt "$S/.ready" ] || [ "$BENCH/setup/_common.sh" -nt "$S/.ready" ]; then
	chmod -R u+w "$S" 2>/dev/null || true
	rm -rf "$S"
	"$BENCH/setup/$mode.sh" install || die "setup of $mode failed"
fi
tool_version=$(cat "$S/version")
allowed=$(cat "$S/allowed" 2>/dev/null || true)
claude_version=$(claude --version | cut -d' ' -f1)
max_turns=${BENCH_MAX_TURNS:-$(cat "$tdir/max_turns" 2>/dev/null || echo 30)}
date=$(date -u +%Y-%m-%d)
mkdir -p "$results/raw/$date"
out_file=$results/$date.jsonl

for ((rep = first; rep < first + reps; rep++)); do
	work=$(mktemp -d "${TMPDIR:-/tmp}/tacet-bench.XXXXXX")
	proj=$work/project
	"$tdir/setup.sh" "$proj" >"$work/setup.log" 2>&1 || {
		cat "$work/setup.log" >&2
		die "task setup failed: $task"
	}
	if ! "$BENCH/setup/$mode.sh" prepare "$proj" >"$work/prepare.log" 2>&1; then
		cat "$work/prepare.log" >&2
		die "prepare failed: $mode on $task"
	fi
	# Warm the prompt cache for this mode in this project first and don't
	# record it. Without it, whichever mode runs after a pause pays for a
	# cold cache and the numbers measure timing, not the tool. The project
	# is then rebuilt at the same path, so hooks that wrote files during
	# the warm-up (a handoff note, an index) leave nothing behind.
	if [ "${BENCH_WARM:-1}" = 1 ]; then
		(
			cd "$proj"
			mode_env "$mode"
			link_auth
			timeout 120 claude -p --model "$model" --max-turns 1 --output-format json \
				"[warm-$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')] Reply with just: ok" >/dev/null 2>&1
		) || :
		chmod -R u+w "$proj" 2>/dev/null || :
		rm -rf "$proj"
		"$tdir/setup.sh" "$proj" >"$work/setup.log" 2>&1 || die "task setup failed: $task"
		"$BENCH/setup/$mode.sh" prepare "$proj" >"$work/prepare.log" 2>&1 || die "prepare failed: $mode on $task"
	fi

	# A unique id first, so no run reads another run's cached prompt:
	# every run pays for a first read, as a real session does.
	nonce="bench-$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')"
	prompt="[$nonce] $(cat "$tdir/prompt.txt")"
	start=$(date +%s)
	rc=0
	(
		cd "$proj"
		mode_env "$mode"
		link_auth
		# shellcheck disable=SC2086 # allowed is a space-separated list of rules
		exec timeout "$timeout" claude -p --model "$model" \
			--allowedTools "Read Grep Glob Bash Edit Write $allowed" \
			--max-turns "$max_turns" --output-format json "$prompt"
	) >"$work/out.json" 2>"$work/err.log" || rc=$?
	secs=$(($(date +%s) - start))
	link=$S/home/.claude/.credentials.json
	if [ -e "$link" ] && [ ! -L "$link" ]; then
		echo "bench: warning: the login was refreshed inside $S (the link became a file)." >&2
	fi

	raw=$results/raw/$date/$task-$mode-$rep-$nonce.json
	if jq -e .type "$work/out.json" >/dev/null 2>&1; then
		cp "$work/out.json" "$raw"
		jq -r '.result // ""' "$work/out.json" >"$work/answer.txt"
	else
		: >"$work/answer.txt"
		raw=""
	fi
	pass=false
	if [ -n "$raw" ] && "$tdir/check.sh" "$proj" "$work/answer.txt" >"$work/check.log" 2>&1; then pass=true; fi

	if [ -n "$raw" ]; then
		# Billed tokens count every model the run used (modelUsage), so a
		# tool's own helper calls inside Claude Code are included.
		jq -c --arg mode "$mode" --arg task "$task" --argjson rep "$rep" --arg model "$model" \
			--arg tv "$tool_version" --arg cv "$claude_version" --argjson pass "$pass" \
			--argjson rc "$rc" --argjson secs "$secs" --arg nonce "$nonce" '
			(.modelUsage // {} | [.[]]) as $m |
			{
				mode: $mode, task: $task, rep: $rep, model: $model, nonce: $nonce,
				input: (if ($m | length) > 0 then ($m | map(.inputTokens) | add) else .usage.input_tokens end),
				cache_write: (if ($m | length) > 0 then ($m | map(.cacheCreationInputTokens) | add) else .usage.cache_creation_input_tokens end),
				cache_read: (if ($m | length) > 0 then ($m | map(.cacheReadInputTokens) | add) else .usage.cache_read_input_tokens end),
				output: (if ($m | length) > 0 then ($m | map(.outputTokens) | add) else .usage.output_tokens end),
				cost: .total_cost_usd, turns: .num_turns, duration_ms: .duration_ms, wall_s: $secs,
				pass: $pass, subtype: .subtype, error: (.is_error == true), exit: $rc,
				denials: ((.permission_denials // []) | length),
				tool_version: $tv, claude_version: $cv
			} | .billed = (.input + .cache_write + .cache_read)' "$work/out.json" >>"$out_file"
	else
		jq -cn --arg mode "$mode" --arg task "$task" --argjson rep "$rep" --arg model "$model" \
			--arg tv "$tool_version" --arg cv "$claude_version" --argjson rc "$rc" --argjson secs "$secs" \
			--arg err "$(tail -c 400 "$work/err.log")" --arg nonce "$nonce" \
			'{mode: $mode, task: $task, rep: $rep, model: $model, nonce: $nonce, pass: false,
			  error: true, exit: $rc, wall_s: $secs, stderr: $err, tool_version: $tv, claude_version: $cv}' >>"$out_file"
	fi
	tail -n 1 "$out_file"

	if [ -n "${BENCH_KEEP:-}" ]; then
		echo "bench: kept $work" >&2
	else
		chmod -R u+w "$work" 2>/dev/null || true
		rm -rf "$work"
	fi
done
