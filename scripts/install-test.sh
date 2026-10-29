#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT

# Tests install.sh offline: a fixture release (a fake tacet binary,
# checksums.txt and a bundle) is served from a temp folder on 127.0.0.1,
# with a fake cosign that records its arguments. Needs python3 for the
# server. Run it with make test-scripts or scripts/install-test.sh
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
command -v python3 >/dev/null || { echo "skip: python3 is needed to serve the fixture release"; exit 0; }

work=$(mktemp -d)
pid=
# shellcheck disable=SC2317 # called by the EXIT trap
cleanup() {
  [ -z "$pid" ] || kill "$pid" 2>/dev/null || :
  rm -rf "$work"
}
trap cleanup EXIT
mkdir -p "$work/www/dl" "$work/bin" "$work/home"

case $(uname -s) in Linux) os=linux ;; Darwin) os=darwin ;; *) echo "skip: unsupported OS"; exit 0 ;; esac
case $(uname -m) in x86_64 | amd64) arch=amd64 ;; aarch64 | arm64) arch=arm64 ;; *) echo "skip: unsupported CPU"; exit 0 ;; esac
archive=tacet_${os}_${arch}.tar.gz

python3 -u -m http.server 0 --bind 127.0.0.1 --directory "$work/www" >"$work/server.log" 2>&1 &
pid=$!
port=
for _ in $(seq 50); do
  port=$(sed -n 's/.*port \([0-9]*\).*/\1/p' "$work/server.log" | head -n 1)
  [ -n "$port" ] && break
  sleep 0.1
done
[ -n "$port" ] || { echo "FAIL the fixture server didn't start"; cat "$work/server.log"; exit 1; }
base=http://127.0.0.1:$port

# release TAG [no-sums] [no-bundle] writes a release for TAG as latest
# and under releases/tags/TAG.
release() {
  local tag=$1 sums=yes bundle=yes
  shift
  for o in "$@"; do
    case $o in no-sums) sums= ;; no-bundle) bundle= ;; esac
  done
  rm -rf "$work/www/dl" "$work/www/repos" "$work/pkg"
  mkdir -p "$work/www/dl" "$work/pkg" "$work/www/repos/RhythmShahriar/tacet/releases/tags"
  printf '#!/bin/sh\necho "tacet %s"\n' "${tag#v}" >"$work/pkg/tacet"
  chmod +x "$work/pkg/tacet"
  printf 'readme\n' >"$work/pkg/README.md"
  tar -czf "$work/www/dl/$archive" -C "$work/pkg" tacet README.md
  (cd "$work/www/dl" && sha256sum "$archive" >checksums.txt)
  echo '{}' >"$work/www/dl/checksums.txt.sigstore.json"
  local assets="{\"name\":\"$archive\",\"browser_download_url\":\"$base/dl/$archive\"}"
  [ -z "$sums" ] || assets="$assets,{\"name\":\"checksums.txt\",\"browser_download_url\":\"$base/dl/checksums.txt\"}"
  [ -z "$bundle" ] || assets="$assets,{\"name\":\"checksums.txt.sigstore.json\",\"browser_download_url\":\"$base/dl/checksums.txt.sigstore.json\"}"
  local json="{\"tag_name\":\"$tag\",\"assets\":[$assets]}"
  printf '%s' "$json" >"$work/www/repos/RhythmShahriar/tacet/releases/latest"
  printf '%s' "$json" >"$work/www/repos/RhythmShahriar/tacet/releases/tags/$tag"
}

# A PATH without cosign, with the tools install.sh needs.
mkdir "$work/plain"
for t in sh curl tar gzip sha256sum mktemp uname tr sed grep cut head cat mkdir cp chmod mv rm id pwd printf; do
  p=$(command -v "$t" 2>/dev/null) && ln -s "$p" "$work/plain/$t"
done
cat >"$work/bin/cosign" <<FAKE
#!/bin/sh
printf '%s\n' "\$@" >"$work/cosign-args"
[ ! -e "$work/cosign-fails" ]
FAKE
chmod +x "$work/bin/cosign"
ln -s "$work/plain/"* "$work/bin/" 2>/dev/null || :

fail=0
# run pass|fail NAME PATHDIR [VAR=value...]
run() {
  local want=$1 what=$2 path=$3
  shift 3
  rm -rf "${work:?}/home" "$work/cosign-args"
  mkdir -p "$work/home"
  if env -i HOME="$work/home" PATH="$path" SHELL=/bin/sh TMPDIR="$work" \
    TACET_GITHUB_API="$base" "$@" sh "$here/install.sh" >"$work/out" 2>&1; then got=pass; else got=fail; fi
  if [ "$got" = "$want" ]; then
    echo "ok   $what"
  else
    echo "FAIL $what: got $got"
    sed 's/^/     /' "$work/out"
    fail=1
  fi
}
# check NAME TEST... passes when TEST succeeds.
check() {
  local what=$1
  shift
  if "$@"; then echo "ok   $what"; else echo "FAIL $what"; fail=1; fi
}
# shellcheck disable=SC2317 # called through check
runs() { [ "$("$1" 2>/dev/null)" = "$2" ]; }
has() { # has NAME TEXT
  if grep -qF -- "$2" "$work/out"; then echo "ok   $1"; else echo "FAIL $1: no '$2' in"; sed 's/^/     /' "$work/out"; fail=1; fi
}

release v0.1.0
run pass "installs the latest release" "$work/plain"
check "the binary runs" runs "$work/home/.local/bin/tacet" "tacet 0.1.0"
check "only the binary is extracted" [ ! -e "$work/home/.local/bin/README.md" ]
has "prints the next step" "  tacet init"
has "says how to fix PATH" "Add $work/home/.local/bin to your PATH"
has "says cosign is missing" "Signature not checked (no cosign)"

release v0.2.0
run pass "a pinned version" "$work/plain" TACET_VERSION=0.2.0 TACET_INSTALL_DIR="$work/home/custom"
check "TACET_INSTALL_DIR is used" runs "$work/home/custom/tacet" "tacet 0.2.0"
run fail "a version that doesn't exist" "$work/plain" TACET_VERSION=9.9.9
has "says there is no such release" "no such release"
run fail "a version that isn't one" "$work/plain" 'TACET_VERSION=1.0;rm'

release v0.1.0
printf '0000000000000000000000000000000000000000000000000000000000000000  %s\n' "$archive" >"$work/www/dl/checksums.txt"
run fail "a checksum mismatch" "$work/plain"
has "names the mismatch" "checksum mismatch for $archive"
check "nothing installed on a mismatch" [ ! -e "$work/home/.local/bin/tacet" ]

release v0.1.0
printf 'other\n' >"$work/www/dl/checksums.txt"
run fail "an archive missing from checksums.txt" "$work/plain"

release v0.1.0 no-sums
run fail "a release without checksums.txt" "$work/plain"
has "says why" "refusing to install an unverified binary"

release v0.1.0
run pass "cosign checks the signature" "$work/bin"
for want in --bundle --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity https://github.com/RhythmShahriar/.github/.github/workflows/release-go.yml@refs/heads/master \
  --certificate-github-workflow-repository RhythmShahriar/tacet \
  --certificate-github-workflow-ref refs/tags/v0.1.0; do
  grep -qxF -- "$want" "$work/cosign-args" 2>/dev/null || { echo "FAIL cosign was not passed $want"; fail=1; }
done

touch "$work/cosign-fails"
run fail "a bad signature" "$work/bin"
has "names the signature" "cosign says checksums.txt wasn't signed"
check "nothing installed on a bad signature" [ ! -e "$work/home/.local/bin/tacet" ]
run pass "TACET_SKIP_SIGNATURE skips cosign" "$work/bin" TACET_SKIP_SIGNATURE=1
check "cosign wasn't asked" [ ! -e "$work/cosign-args" ]
rm -f "$work/cosign-fails"

release v0.1.0 no-bundle
run pass "a release without a bundle still installs" "$work/bin"
has "warns about the missing bundle" "warning: the release has no checksums.txt.sigstore.json"

run fail "an http API that isn't loopback" "$work/plain" TACET_GITHUB_API=http://example.invalid
run fail "an API with user info" "$work/plain" TACET_GITHUB_API=https://user@example.invalid

release v0.1.0
sed -i.bak "s#$base/dl/$archive#http://example.invalid/dl/$archive#" "$work/www/repos/RhythmShahriar/tacet/releases/latest"
run fail "a download over http off loopback" "$work/plain"
has "refuses http" "refusing to download over http"

release v0.1.0
run fail "a bad timeout" "$work/plain" TACET_INSTALL_TIMEOUT=soon

exit "$fail"
