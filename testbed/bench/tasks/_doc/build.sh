#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Builds the bench document, a 40+ page guide made from the public
# RFC 6749 with made-up implementer notes at the start, middle and end,
# as PDF or DOCX (real heading styles) and copies it to <out>. The RFC
# text is pinned by sha256; built files are cached under cache/doc/.
#
#   build.sh pdf|docx <out>
set -euo pipefail
fmt=$1 out=$2
here=$(cd "$(dirname "$0")" && pwd)
root=${BENCH_CACHE:-$here/../../cache}
# A go.mod keeps tacet's go ./... out of the cached clones.
mkdir -p "$root"
[ -f "$root/go.mod" ] || printf 'module benchscratch\n' >"$root/go.mod"
cache=$root/doc
url=https://www.rfc-editor.org/rfc/rfc6749.txt
sha=f204fc8661d6c92d2ec6e0b54808f961a9ad26e792f57f312d9528335519bd71

case $fmt in
pdf) filter='pdf:writer_pdf_Export:{"ExportBookmarks":{"type":"boolean","value":"true"}}' ;;
docx) filter='docx:MS Word 2007 XML' ;;
*) echo "build.sh: format must be pdf or docx" >&2; exit 2 ;;
esac

mkdir -p "$cache"
if [ ! -f "$cache/rfc6749.txt" ]; then
	curl -fsSL "$url" -o "$cache/rfc6749.txt.part"
	mv "$cache/rfc6749.txt.part" "$cache/rfc6749.txt"
fi
echo "$sha  $cache/rfc6749.txt" | sha256sum -c --quiet - || {
	echo "build.sh: rfc6749.txt does not match the pinned sha256" >&2
	rm -f "$cache/rfc6749.txt"
	exit 1
}

# Rebuild when the converter or its notes change.
key=$(cat "$cache/rfc6749.txt" "$here/rfc2html.py" | sha256sum | cut -c1-16)
built=$cache/$key/oauth-guide.$fmt
if [ ! -f "$built" ]; then
	mkdir -p "$cache/$key"
	python3 -I "$here/rfc2html.py" "$cache/rfc6749.txt" "$cache/$key/oauth-guide.html"
	# A private profile, so a running LibreOffice doesn't swallow the job.
	soffice -env:UserInstallation="file://$cache/lo-profile" --headless \
		--infilter="HTML (StarWriter)" --convert-to "$filter" \
		--outdir "$cache/$key" "$cache/$key/oauth-guide.html" >/dev/null 2>&1
	[ -f "$built" ] || { echo "build.sh: soffice did not write $built" >&2; exit 1; }
fi
cp "$built" "$out"
