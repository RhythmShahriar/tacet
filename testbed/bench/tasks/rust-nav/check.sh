#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Passes when the answer names the defining file (it also calls it) and both other callers.
#
#   check.sh <project-dir> <answer-file>
set -euo pipefail
a=$2
for f in decompress.rs search.rs hiargs.rs; do
	grep -Fq "$f" "$a" || exit 1
done
