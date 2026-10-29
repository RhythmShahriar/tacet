#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Passes when the answer names all three code names (pdftotext may drop a
# hyphen at a line break, so that is allowed) and their sections:
# 1.2 at the start, 4.3 in the middle and Appendix B at the end.
#
#   check.sh <project-dir> <answer-file>
set -euo pipefail
a=$2
grep -Eiq 'HARBOR-?LANTERN-?17' "$a" &&
	grep -Eiq 'COPPER-?WILLOW-?42' "$a" &&
	grep -Eiq 'SALT-?MERIDIAN-?88' "$a" &&
	grep -Eq '\b1\.2\b' "$a" &&
	grep -Eq '\b4\.3\b' "$a" &&
	grep -Eiq 'appendix[ *]*b\b' "$a"
