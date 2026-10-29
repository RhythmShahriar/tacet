#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Passes when the answer has both facts from the note in section 4.4.
#
#   check.sh <project-dir> <answer-file>
set -euo pipefail
a=$2
grep -Eiq '\b38[ -]?(minutes|min)\b' "$a" && grep -Eiq 'ledger-?sync' "$a"
