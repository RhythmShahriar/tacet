#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# ripgrep 14.1.1 (98 Rust files), shallow at a pinned commit.
#
#   setup.sh <project-dir>
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
"$here/../_doc/clone.sh" BurntSushi/ripgrep 4649aa9700619f94cf9c66876e9549d83420e16c "$1"
