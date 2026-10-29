#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# The known fix, for selftest.sh only: touching spans merge too.
#
#   solution.sh <project-dir>
set -euo pipefail
sed -i 's/if s.start < last.end =>/if s.start <= last.end =>/' "$1/src/lib.rs"
