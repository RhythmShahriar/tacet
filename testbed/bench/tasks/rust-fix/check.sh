#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Passes when the tests are untouched and cargo test passes.
#
#   check.sh <project-dir> <answer-file>
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
cd "$1"
diff -r "$here/project/tests" tests >/dev/null || { echo "check: the tests were changed" >&2; exit 1; }
command -v cargo >/dev/null || PATH=$here/../../shims:$PATH
cargo test --offline -q >/dev/null 2>&1
