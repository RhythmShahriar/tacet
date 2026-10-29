#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Passes when the answer names the defining file and all five calling
# files (found with grep at the pinned commit).
#
#   check.sh <project-dir> <answer-file>
set -euo pipefail
a=$2
for f in caddyhttp/caddyhttp.go fileserver/matcher.go browsetplcontext.go fileserver/browse.go fastcgi/fastcgi.go staticfiles.go; do
	grep -Fq "$f" "$a" || exit 1
done
