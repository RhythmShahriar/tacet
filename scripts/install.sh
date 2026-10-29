#!/bin/sh
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT

# Installs tacet from GitHub Releases on macOS and Linux:
#   curl -fsSL https://raw.githubusercontent.com/RhythmShahriar/tacet/master/scripts/install.sh | sh
#
# Environment:
#   TACET_INSTALL_DIR     where to install (default ~/.local/bin, else the
#                         first writable of /usr/local/bin and /opt/homebrew/bin)
#   TACET_VERSION         a specific version, e.g. 0.1.0 (default: latest)
#   TACET_SKIP_SIGNATURE  1 skips the cosign check, for a mirror you trust
#   TACET_GITHUB_API      GitHub API base, for mirrors (default https://api.github.com).
#                         It must be https; plain http is accepted only for
#                         127.0.0.1, ::1 and localhost, so the tests can run offline.
#   GITHUB_TOKEN          sent to api.github.com only, to lift the anonymous rate limit
#   TACET_INSTALL_TIMEOUT seconds to wait for a connection, or on one that
#                         has stalled (default 30, at most 3600)
#
# Every download is checked against the release's checksums.txt. When
# cosign is installed, checksums.txt itself must carry a Sigstore
# signature from tacet's release workflow for that tag.
#
# Everything is inside main, called on the last line, so a download cut
# off halfway through is a syntax error instead of a partial install.

main() {
	set -eu

	REPO=RhythmShahriar/tacet
	# The shared workflow that signs every release. Its identity alone would
	# accept any repository that calls it, so the repository and tag are
	# pinned as well (see scripts/verify-release.sh).
	WORKFLOW=RhythmShahriar/.github/.github/workflows/release-go.yml
	ISSUER=https://token.actions.githubusercontent.com

	say() { printf '%s\n' "$*" >&2; }
	warn() { say "warning: $*"; }
	die() {
		say "tacet install: $*"
		exit 1
	}

	# Spelled out: some tr builds, such as BusyBox 1.30, get [:upper:] wrong.
	lower() { printf '%s' "$1" | tr ABCDEFGHIJKLMNOPQRSTUVWXYZ abcdefghijklmnopqrstuvwxyz; }

	sha256() {
		if command -v sha256sum >/dev/null 2>&1; then
			sha256sum "$1" | cut -d' ' -f1
		elif command -v shasum >/dev/null 2>&1; then
			shasum -a 256 "$1" | cut -d' ' -f1
		else
			die "sha256sum or shasum is required to verify the download"
		fi
	}

	# url_host prints the lower-case host of a URL, without user info, port
	# or IPv6 brackets. The authority ends at the first '/', '?' or '#', as
	# it does for curl.
	url_host() {
		_h=${1#*://}
		_h=${_h%%[/?#]*}
		_h=${_h##*@}
		case $_h in
		\[*)
			_h=${_h#\[}
			_h=${_h%%\]*}
			;;
		*) _h=${_h%%:*} ;;
		esac
		lower "$_h"
	}

	is_loopback() {
		case $(url_host "$1") in
		127.0.0.1 | ::1 | localhost) return 0 ;;
		esac
		return 1
	}

	# check_url refuses a URL that isn't https. Plain http passes only when
	# both the API base and the URL are on loopback.
	check_url() {
		case $(lower "${1%%://*}") in
		https) return 0 ;;
		http) is_loopback "$API" && is_loopback "$1" && return 0 ;;
		esac
		die "refusing to download over http: $1"
	}

	# fetch downloads URL $1 to file $2. curl exits 28 on a timeout and 1
	# when --proto-redir turns down a redirect.
	fetch() {
		check_url "$1"
		# shellcheck disable=SC2086 # $limits is several flags
		curl -fsSL --proto "$proto" --proto-redir =https --tlsv1.2 $limits -o "$2" "$1" || {
			_rc=$?
			[ $_rc -ne 28 ] || die "timed out after ${timeout}s downloading $1; set TACET_INSTALL_TIMEOUT to wait longer"
			[ $_rc -ne 1 ] || die "refusing to follow a redirect that isn't https from $1"
			return $_rc
		}
	}

	# api_get downloads API URL $1 to file $2 and prints the HTTP status.
	# The token goes to curl on stdin, so it never shows up in a process
	# listing, and curl drops it when a redirect leaves the host.
	api_get() {
		# shellcheck disable=SC2086 # $limits is several flags
		if [ -n "$token" ]; then
			printf 'header = "Authorization: Bearer %s"\n' "$token" |
				curl -sSL --proto "$proto" --proto-redir =https --tlsv1.2 $limits -K - -H 'Accept: application/vnd.github+json' \
					-o "$2" -w '%{http_code}' "$1"
		else
			curl -sSL --proto "$proto" --proto-redir =https --tlsv1.2 $limits -H 'Accept: application/vnd.github+json' \
				-o "$2" -w '%{http_code}' "$1"
		fi
	}

	# asset prints the download URL of a release asset by file name. jq
	# can't be counted on, so the JSON is split at commas and read with sed.
	asset() {
		tr ',' '\n' <"$tmp/release.json" | grep '"browser_download_url"' |
			sed 's/.*"browser_download_url"[^"]*"\([^"]*\)".*/\1/' | grep "/$1\$" | head -n 1
	}

	# good_tag reports whether $1 looks like a release tag: v, a digit, then
	# only [0-9A-Za-z.+-], so it is safe in a URL path and a cosign flag.
	good_tag() {
		case $1 in
		v[0-9]*[!0-9A-Za-z.+-]*) return 1 ;;
		v[0-9]*) return 0 ;;
		esac
		return 1
	}

	# verify_signature checks that checksums.txt was signed by tacet's
	# release workflow for $tag when cosign is installed. Without cosign it
	# says so and goes on: the checksum is still checked. A release with no
	# bundle while cosign is there gets a warning, since whoever can change
	# a release can delete the bundle.
	verify_signature() {
		if ! command -v cosign >/dev/null 2>&1; then
			say "Signature not checked (no cosign); the checksum still is. Install cosign to check who signed the release."
			return
		fi
		if [ ! -s "$tmp/checksums.txt.sigstore.json" ]; then
			warn "the release has no checksums.txt.sigstore.json, so its signature wasn't checked"
			return
		fi
		if cosign verify-blob --bundle "$tmp/checksums.txt.sigstore.json" \
			--certificate-oidc-issuer "$ISSUER" \
			--certificate-identity "https://github.com/$WORKFLOW@refs/heads/master" \
			--certificate-github-workflow-repository "$REPO" \
			--certificate-github-workflow-ref "refs/tags/$tag" \
			"$tmp/checksums.txt" >"$tmp/verify.out" 2>&1; then
			say "Signature ok  $REPO @ refs/tags/$tag (cosign)"
			return
		fi
		cat "$tmp/verify.out" >&2
		die "cosign says checksums.txt wasn't signed for $REPO $tag; nothing was installed"
	}

	# writable reports whether folder $1 exists, or can be made, and takes
	# a new file.
	writable() {
		mkdir -p "$1" 2>/dev/null || return 1
		[ -d "$1" ] && [ -w "$1" ]
	}

	# path_hint says how to put $dir on PATH in the user's shell. zsh never
	# reads ~/.profile; a macOS terminal starts bash as a login shell, which
	# reads ~/.bash_profile. Each ' in the folder is written as '\''.
	path_hint() {
		qdir=$(printf '%s\n' "$dir" | sed "s/'/'\\\\''/g")
		case ${SHELL:-} in
		*/fish | fish)
			say "Add $dir to your PATH: fish_add_path '$qdir'"
			return
			;;
		*/zsh | zsh) rc=.zshrc ;;
		*/bash | bash)
			rc=.bashrc
			[ "$os" != darwin ] || rc=.bash_profile
			;;
		*) rc=.profile ;;
		esac
		say "Add $dir to your PATH, then open a new terminal: echo 'export PATH=\"$qdir:\$PATH\"' >> ~/$rc"
	}

	command -v curl >/dev/null 2>&1 || die "curl is required"
	command -v tar >/dev/null 2>&1 || die "tar is required"

	API=${TACET_GITHUB_API:-https://api.github.com}
	API=${API%/}
	case $API in
	*[@?#\\]*) die "TACET_GITHUB_API must be a plain base URL, without user info, query or fragment: $API" ;;
	esac
	# $proto is what the first URL may use. Redirects must be https in every
	# mode (--proto-redir), since check_url sees only the first URL.
	proto='=https'
	case $(lower "${API%%://*}") in
	https) ;;
	http)
		is_loopback "$API" ||
			die "TACET_GITHUB_API must be https (http only for 127.0.0.1, ::1 and localhost): $API"
		proto='=http,https'
		;;
	*) die "TACET_GITHUB_API must be an https URL: $API" ;;
	esac

	# GITHUB_TOKEN belongs to github.com, so it never goes to a mirror.
	token=
	if [ "$proto" = '=https' ] && [ "$(url_host "$API")" = api.github.com ]; then
		token=${GITHUB_TOKEN:-}
	fi
	case $token in
	*[\"\\]*) die "GITHUB_TOKEN has a character curl can't send in a header" ;;
	esac

	timeout=${TACET_INSTALL_TIMEOUT:-30}
	secs=${timeout#"${timeout%%[!0]*}"}
	case $timeout in
	'' | *[!0-9]*) secs= ;;
	esac
	[ -n "$secs" ] || die "TACET_INSTALL_TIMEOUT must be a whole number of seconds: '$timeout'"
	if [ ${#secs} -gt 4 ] || [ "$secs" -gt 3600 ]; then
		die "TACET_INSTALL_TIMEOUT must be at most 3600 seconds: '$timeout'"
	fi
	timeout=$secs
	limits="--connect-timeout $timeout --speed-limit 1 --speed-time $timeout"

	os=$(lower "$(uname -s)")
	case $os in
	linux | darwin) ;;
	*) die "unsupported OS '$os'. On Windows, run scripts/install.ps1 in PowerShell" ;;
	esac
	case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "unsupported CPU '$(uname -m)'" ;;
	esac

	tmp=$(mktemp -d "${TMPDIR:-/tmp}/tacet-install.XXXXXX")
	# dash skips the EXIT trap when a signal ends the script, so INT and
	# TERM exit on purpose and the cleanup still runs.
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 130' INT
	trap 'exit 143' TERM

	if [ -n "${TACET_VERSION:-}" ]; then
		tag=v${TACET_VERSION#v}
		good_tag "$tag" || die "TACET_VERSION isn't a version: '$TACET_VERSION'"
		release_url="$API/repos/$REPO/releases/tags/$tag"
	else
		release_url="$API/repos/$REPO/releases/latest"
	fi
	rc=0
	status=$(api_get "$release_url" "$tmp/release.json") || rc=$?
	case $rc in
	0) ;;
	28) die "timed out after ${timeout}s waiting for $(url_host "$API"); set TACET_INSTALL_TIMEOUT to wait longer" ;;
	*) die "couldn't reach $(url_host "$API") ($release_url)" ;;
	esac
	case $status in
	200) ;;
	403 | 429)
		[ -n "$token" ] || [ "$proto" != '=https' ] || die "GitHub rate limit reached; set GITHUB_TOKEN"
		die "rate limit reached at $(url_host "$API")"
		;;
	404) die "no such release: $release_url" ;;
	*) die "couldn't fetch the release ($release_url): HTTP $status" ;;
	esac

	if [ -z "${TACET_VERSION:-}" ]; then
		tag=$(tr ',' '\n' <"$tmp/release.json" | sed -n 's/.*"tag_name"[^"]*"\([^"]*\)".*/\1/p' | head -n 1)
		good_tag "$tag" || die "the release has an odd tag: '$tag'"
	fi

	name="tacet_${os}_${arch}.tar.gz"
	archive_url=$(asset "$name")
	sums_url=$(asset checksums.txt)
	bundle_url=$(asset checksums.txt.sigstore.json)
	[ -n "$archive_url" ] || die "the release has no build for $os/$arch"
	[ -n "$sums_url" ] || die "the release has no checksums.txt; refusing to install an unverified binary"

	fetch "$archive_url" "$tmp/$name" || die "download failed: $archive_url"
	fetch "$sums_url" "$tmp/checksums.txt" || die "download failed: $sums_url"
	if [ -n "$bundle_url" ]; then
		fetch "$bundle_url" "$tmp/checksums.txt.sigstore.json" || die "download failed: $bundle_url"
	fi
	case ${TACET_SKIP_SIGNATURE:-0} in
	0) verify_signature ;;
	*) warn "TACET_SKIP_SIGNATURE is set: nothing checks that $REPO signed this release. Use it only with a mirror you trust." ;;
	esac

	# A sum line is "<hex>  <name>", or "<hex> *<name>" in binary mode. A
	# missing line counts as a mismatch.
	want=$(grep " \*\{0,1\}$name\$" "$tmp/checksums.txt" | cut -d' ' -f1)
	got=$(sha256 "$tmp/$name")
	if [ -z "$want" ] || [ "$want" != "$got" ]; then
		die "checksum mismatch for $name; nothing was installed"
	fi
	say "Checksum ok   $name"
	# Only the binary is taken out, so nothing else in the archive lands
	# anywhere.
	tar -xzf "$tmp/$name" -C "$tmp" tacet || die "the archive has no tacet binary"

	if [ -n "${TACET_INSTALL_DIR:-}" ]; then
		dir=$TACET_INSTALL_DIR
		writable "$dir" || die "can't write to $dir"
	else
		dir=
		for d in "$HOME/.local/bin" /usr/local/bin /opt/homebrew/bin; do
			if writable "$d"; then
				dir=$d
				break
			fi
		done
		[ -n "$dir" ] || die "no writable folder for tacet; set TACET_INSTALL_DIR to one you own"
	fi
	dir=$(CDPATH='' cd -- "$dir" && pwd) || die "can't open $dir"
	# Copied next to the target first and moved over it, so a running tacet
	# is never half written.
	cp "$tmp/tacet" "$dir/.tacet.new.$$" || die "can't write to $dir"
	chmod 755 "$dir/.tacet.new.$$"
	mv -f "$dir/.tacet.new.$$" "$dir/tacet" || {
		rm -f "$dir/.tacet.new.$$"
		die "can't replace $dir/tacet"
	}
	say "Installed tacet ${tag#v} to $dir/tacet"

	case ":$PATH:" in
	*":$dir:"*)
		found=$(command -v tacet 2>/dev/null) || found=
		[ -z "$found" ] || [ "$found" = "$dir/tacet" ] ||
			say "Note: $found comes first on your PATH; remove it to use this install."
		;;
	*) path_hint ;;
	esac
	say ""
	say "Next, in your project folder:"
	say "  tacet init"
}

main "$@"
