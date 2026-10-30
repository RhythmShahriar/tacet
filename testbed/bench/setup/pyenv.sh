#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# A real Python with pytest for every mode, so "pytest", "python -m pytest"
# and "python3 -m pytest" all work the same whatever tool is installed.
# This Python has no ensurepip, so the venv is made without pip and the
# pinned pure-Python wheels are unpacked into it, each checked against the
# sha256 PyPI publishes.
#
#   pyenv.sh <dir>
set -euo pipefail
dir=$1
[ -x "$dir/bin/pytest" ] && exit 0
python3 -m venv --without-pip "$dir"
site=$("$dir/bin/python" -c 'import sysconfig; print(sysconfig.get_paths()["purelib"])')
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
while read -r name sum url; do
	curl -fsSL -o "$tmp/$name" "$url"
	echo "$sum  $tmp/$name" | sha256sum -c --quiet -
	"$dir/bin/python" -m zipfile -e "$tmp/$name" "$site"
done <<'WHEELS'
pytest-8.3.3-py3-none-any.whl a6853c7375b2663155079443d2e45de913a911a11d669df02a50814944db57b2 https://files.pythonhosted.org/packages/6b/77/7440a06a8ead44c7757a64362dd22df5760f9b12dc5f11b6188cd2fc27a0/pytest-8.3.3-py3-none-any.whl
pluggy-1.5.0-py3-none-any.whl 44e1ad92c8ca002de6377e165f3e0f1be63266ab4d554740532335b9d75ea669 https://files.pythonhosted.org/packages/88/5f/e351af9a41f866ac3f1fac4ca0613908d9a41741cfcf2228f4ad853b697d/pluggy-1.5.0-py3-none-any.whl
iniconfig-2.0.0-py3-none-any.whl b6a85871a79d2e3b22d2d1b94ac2824226a63c6b741c88f7ae975f18b6778374 https://files.pythonhosted.org/packages/ef/a6/62565a6e1cf69e10f5727360368e451d4b7f58beeac6173dc9db836a5b46/iniconfig-2.0.0-py3-none-any.whl
packaging-24.1-py3-none-any.whl 5b8f2217dbdbd2f7f384c41c628544e6d52f2d0f53c6d0c3ea61aa5d1d7ff124 https://files.pythonhosted.org/packages/08/aa/cc0199a5f0ad350994d660967a8efb233fe0416e4639146c089643407ce6/packaging-24.1-py3-none-any.whl
exceptiongroup-1.2.2-py3-none-any.whl 3111b9d131c238bec2f8f516e123e14ba243563fb135d3fe885990585aa7795b https://files.pythonhosted.org/packages/02/cc/b7e31358aac6ed1ef2bb790a9746ac2c69bcb3c8588b41616914eb106eaf/exceptiongroup-1.2.2-py3-none-any.whl
tomli-2.0.1-py3-none-any.whl 939de3e7a6161af0c887ef91b7d41a53e7c5a1ca976325f429cb46ea9bc30ecc https://files.pythonhosted.org/packages/97/75/10a9ebee3fd790d20926a90a2547f0bf78f371b2f13aa822c759680ca7b9/tomli-2.0.1-py3-none-any.whl
WHEELS
printf '#!/bin/sh\nexec "%s/bin/python" -m pytest "$@"\n' "$dir" >"$dir/bin/pytest"
chmod +x "$dir/bin/pytest"
"$dir/bin/python" -m pytest --version
