#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Commits everything in <dir> as a fresh repository, so every task's
# project looks the same to tools that key on git (handoff notes, memory).
#
#   git.sh <dir>
set -euo pipefail
cd "$1"
git init -q -b main
git -c user.name=bench -c user.email=bench@example.invalid add -A
git -c user.name=bench -c user.email=bench@example.invalid commit -q -m "start"
