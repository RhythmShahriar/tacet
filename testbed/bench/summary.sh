#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Summarizes ab.sh lines from stdin: mean billed tokens, turns, cost and
# accuracy per mode, and the change with tacet.
set -euo pipefail
jq -s '
	map(select(.error | not)) | group_by(.mode) | map({
		mode: .[0].mode, runs: length,
		total: (map(.total) | add / length | round),
		turns: (map(.turns) | add / length * 10 | round / 10),
		cost: (map(.cost) | add / length * 10000 | round / 10000),
		correct: "\(map(select(.correct)) | length)/\(length)"
	}) | {off: (.[] | select(.mode == "off")), on: (.[] | select(.mode == "on"))}
	| . + {tokens_change: "\(((.on.total - .off.total) / .off.total * 100) | round)%",
	       cost_change: "\(((.on.cost - .off.cost) / .off.cost * 100) | round)%"}'
