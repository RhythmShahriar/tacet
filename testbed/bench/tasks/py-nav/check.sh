#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Passes when the answer names the defining file and both calling files.
#
#   check.sh <project-dir> <answer-file>
set -euo pipefail
a=$2
for f in _compat.py formatting.py _termui_impl.py; do
	grep -Fq "$f" "$a" || exit 1
done
