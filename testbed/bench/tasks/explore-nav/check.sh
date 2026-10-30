#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Passes when the answer names all 16 http.matchers.* module IDs and the
# five files that register them (found with grep at the pinned commit).
#
#   check.sh <project-dir> <answer-file>
set -euo pipefail
a=$2
for id in vars vars_regexp expression remote_ip client_ip file host path path_regexp \
	method query header header_regexp protocol tls not; do
	grep -Eq "http\.matchers\.$id([^a-z_]|$)" "$a" || { echo "missing http.matchers.$id"; exit 1; }
done
for f in vars.go celmatcher.go ip_matchers.go fileserver/matcher.go caddyhttp/matchers.go; do
	grep -Fq "$f" "$a" || { echo "missing $f"; exit 1; }
done
