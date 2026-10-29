#!/usr/bin/env bash
# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT
#
# Mode "serena": oraios/serena (PyPI serena-agent) as its README says:
# uv tool install -p 3.13 serena-agent, then the Claude Code setup from
# its docs, claude mcp add --scope user serena -- serena start-mcp-server
# --context claude-code --project-from-cwd. Serena's hooks are opt-in
# (alpha) in its docs, so they stay off. --open-web-dashboard False keeps
# it from opening a browser on every run; that changes no tokens.
#
# Serena needs a language server per language: it fetches pyright for
# Python itself, gopls (Go) is installed here, rust-analyzer (Rust) needs
# a Rust toolchain and is used only when one is on PATH.
#
#   serena.sh [install]
set -euo pipefail
# shellcheck source=SCRIPTDIR/_common.sh
. "$(dirname "$0")/_common.sh"
[ "${1:-install}" = install ] || exit 0
version=2.1.0
gopls=v0.23.0
mode_env serena
uv_setup
link_auth
uv tool install --force -p 3.13 "serena-agent==$version" >/dev/null || die "uv tool install serena-agent failed"
claude mcp remove --scope user serena >/dev/null 2>&1 || true
claude mcp add --scope user serena -- "$S/bin/serena" start-mcp-server --context claude-code --project-from-cwd \
	--open-web-dashboard False ||
	die "claude mcp add serena failed"
# Serena finds gopls on PATH; install it into the mode's bin, pinned.
command -v go >/dev/null || PATH=$PATH:/usr/local/go/bin
GOBIN=$S/bin GOFLAGS='' go install "golang.org/x/tools/gopls@$gopls" || echo "bench: gopls did not install" >&2
# Language servers ahead of time, so the first run doesn't time out on a
# download. A language that fails here is reported, not fatal.
for lang in python go rust; do
	timeout 300 serena download-ls-dependencies "$lang" >/dev/null 2>&1 ||
		echo "bench: serena could not fetch the $lang language server ahead of time" >&2
done
finish "serena $version" mcp__serena
