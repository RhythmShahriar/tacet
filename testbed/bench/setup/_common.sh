#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Shared by compare.sh and setup/<mode>.sh (sourced, never run).
#
# Every mode gets its own HOME and CLAUDE_CONFIG_DIR under state/<mode>/,
# so a tool's plugin, hooks, MCP servers, skills and data never reach
# another mode or the person's own ~/.claude.
# shellcheck disable=SC2034 # the variables are read by the scripts that source this file

BENCH=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
STATE_ROOT=${BENCH_STATE:-$BENCH/state}
CACHE=${BENCH_CACHE:-$BENCH/cache}
REAL_HOME=${BENCH_REAL_HOME:-$HOME}
MODES="none tacet tacet-all tacet-subagents rtk caveman context-mode claude-mem graphify serena"

# state/ and cache/ hold downloaded Go modules and clones. A go.mod at
# their top makes them modules of their own, so tacet's go ./... (lint,
# tests) never walks into them.
for d in "$STATE_ROOT" "$CACHE"; do
	mkdir -p "$d"
	[ -f "$d/go.mod" ] || printf 'module benchscratch\n' >"$d/go.mod"
done

die() {
	echo "bench: $*" >&2
	exit 1
}

valid_mode() {
	case " $MODES " in *" $1 "*) return 0 ;; esac
	return 1
}

# mode_env <mode> points HOME, CLAUDE_CONFIG_DIR and PATH at the mode's
# state. Run it in a subshell or a script of its own: it changes HOME.
mode_env() {
	S=$STATE_ROOT/$1
	mkdir -p "$S/home/.claude" "$S/bin"
	export HOME=$S/home
	export CLAUDE_CONFIG_DIR=$HOME/.claude
	unset XDG_CONFIG_HOME XDG_CACHE_HOME XDG_DATA_HOME XDG_STATE_HOME
	# Same for every mode: no auto-update in the middle of a run.
	export DISABLE_AUTOUPDATER=1
	# One real Python with pytest, first on PATH for every mode, so pytest
	# runs the same way however the agent calls it.
	[ -x "$BENCH/cache/py/bin/pytest" ] || "$BENCH/setup/pyenv.sh" "$BENCH/cache/py" >/dev/null
	PATH=$BENCH/cache/py/bin:$S/bin:$HOME/.local/bin:$PATH
	# Commands the tasks need, when the machine lacks them. Appended, so a
	# real install always wins.
	if ! command -v go >/dev/null && [ -x /usr/local/go/bin/go ]; then PATH=$PATH:/usr/local/go/bin; fi
	if ! command -v pytest >/dev/null || ! command -v cargo >/dev/null; then PATH=$PATH:$BENCH/shims; fi
	export PATH
}

# link_auth reuses the person's login without copying it: a symlink to
# ~/.claude/.credentials.json inside the mode's state (gitignored). With
# CLAUDE_CODE_OAUTH_TOKEN (claude setup-token) or ANTHROPIC_API_KEY in the
# environment no link is made at all.
link_auth() {
	local link=$CLAUDE_CONFIG_DIR/.credentials.json
	if [ -n "${CLAUDE_CODE_OAUTH_TOKEN:-}${ANTHROPIC_API_KEY:-}" ]; then
		[ -L "$link" ] && rm -f "$link"
		return 0
	fi
	[ -f "$REAL_HOME/.claude/.credentials.json" ] ||
		die "no login found: run claude once, or set CLAUDE_CODE_OAUTH_TOKEN (claude setup-token)"
	if [ -e "$link" ] && [ ! -L "$link" ]; then
		echo "bench: $link is a file, not a link: a run refreshed the login there. Deleting it; log in with claude again if the main login stops working." >&2
		rm -f "$link"
	fi
	ln -sfn "$REAL_HOME/.claude/.credentials.json" "$link"
}

# fetch <url> <sha256> <out> downloads once into cache/downloads and
# refuses a file that doesn't match the pinned hash.
fetch() {
	local url=$1 sha=$2 out=$3 f
	f=$CACHE/downloads/$sha-${url##*/}
	mkdir -p "$CACHE/downloads"
	if [ ! -f "$f" ]; then
		curl -fsSL "$url" -o "$f.part" || die "download failed: $url"
		mv "$f.part" "$f"
	fi
	echo "$sha  $f" | sha256sum -c --quiet - || {
		rm -f "$f"
		die "$url does not match the pinned sha256"
	}
	cp "$f" "$out"
}

# git_at <url> <ref> <dir> clones a tag or commit shallowly.
git_at() {
	rm -rf "$3"
	git clone -q --depth 1 --branch "$2" "$1" "$3" 2>/dev/null || {
		mkdir -p "$3"
		git -C "$3" init -q
		git -C "$3" fetch -q --depth 1 "$1" "$2"
		git -C "$3" -c advice.detachedHead=false checkout -q FETCH_HEAD
	}
}

# marketplace_plugin <dir> <plugin@marketplace> installs a Claude Code
# plugin from a local checkout of its marketplace, the same as
# /plugin marketplace add + /plugin install, only at a pinned tag.
marketplace_plugin() {
	claude plugin marketplace add "$1" || die "claude plugin marketplace add $1 failed"
	claude plugin install "$2" || die "claude plugin install $2 failed"
}

# uv_setup installs the pinned uv into the mode's bin, with its tools,
# Python builds and cache kept under the mode's state.
UV_VERSION=0.13.5
UV_SHA256=f68d291c75783122dad58ab77def0e13f3233f358731db5d6d7e8715603b928f
uv_setup() {
	export UV_TOOL_DIR=$S/uv/tools UV_TOOL_BIN_DIR=$S/bin UV_PYTHON_INSTALL_DIR=$S/uv/python
	export UV_CACHE_DIR=$CACHE/uv UV_NO_MODIFY_PATH=1
	[ -x "$S/bin/uv" ] && return 0
	local t=$S/uv/uv.tar.gz
	mkdir -p "$S/uv"
	fetch "https://github.com/astral-sh/uv/releases/download/$UV_VERSION/uv-x86_64-unknown-linux-gnu.tar.gz" "$UV_SHA256" "$t"
	tar -xzf "$t" -C "$S/uv"
	cp "$S/uv/uv-x86_64-unknown-linux-gnu/uv" "$S/uv/uv-x86_64-unknown-linux-gnu/uvx" "$S/bin/"
}

# finish <version> [extra allowed tools...] records what compare.sh needs.
finish() {
	echo "$1" >"$S/version"
	shift
	echo "$*" | tr -s '\n' ' ' | sed 's/ *$//' >"$S/allowed"
	touch "$S/.ready"
	echo "bench: $(basename "$S") ready ($(cat "$S/version"))"
}

# mcp_rules <plugin> prints one allow rule per MCP server the installed
# plugin declares (in .mcp.json or plugin.json), so every tool gets its
# own MCP tools on top of the shared list.
mcp_rules() {
	local reg=$CLAUDE_CONFIG_DIR/plugins/installed_plugins.json dir
	[ -f "$reg" ] || return 0
	jq -r --arg p "$1" '.plugins // {} | to_entries[] | select(.key | startswith($p + "@")) | .value[].installPath' "$reg" |
		while read -r dir; do
			{
				[ ! -f "$dir/.mcp.json" ] || jq -r '(.mcpServers // .) | keys[]' "$dir/.mcp.json"
				[ ! -f "$dir/.claude-plugin/plugin.json" ] ||
					jq -r '.mcpServers | if type == "object" then keys[] else empty end' "$dir/.claude-plugin/plugin.json"
			} 2>/dev/null
		done | sort -u | sed "s/^/mcp__plugin_$1_/"
}
