#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Passes when the tests are untouched and pytest passes.
#
#   check.sh <project-dir> <answer-file>
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
cd "$1"
diff -r -x __pycache__ "$here/project/tests" tests >/dev/null || { echo "check: the tests were changed" >&2; exit 1; }
command -v pytest >/dev/null || PATH=$here/../../shims:$PATH
pytest -q >/dev/null 2>&1
