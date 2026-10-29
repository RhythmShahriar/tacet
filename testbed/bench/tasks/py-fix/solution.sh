#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# The known fix, for selftest.sh only: the bulk discount starts at 10 units.
#
#   solution.sh <project-dir>
set -euo pipefail
sed -i 's/if self.units() > BULK_UNITS:/if self.units() >= BULK_UNITS:/' "$1/tally/invoice.py"
