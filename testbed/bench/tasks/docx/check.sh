#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Passes when the answer has both facts from the note in section 6.
#
#   check.sh <project-dir> <answer-file>
set -euo pipefail
a=$2
grep -Eiq '\b(9|nine)\b' "$a" && grep -Eiq 'tokens-?audit' "$a"
