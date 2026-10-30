<h1 align="center">tacet</h1>

<p align="center">
  <b>Why is Claude Code reading all that?</b><br>
  tacet makes Claude Code use fewer tokens on any project. Run <code>tacet init</code> once: it measures what every session loads, fixes what is safe and then works on its own through Claude Code's hooks, so long documents, secrets and stale context stop costing you.
</p>

<p align="center">
  <a href="https://github.com/RhythmShahriar/tacet/releases/latest"><img alt="Release" src="https://img.shields.io/github/v/release/RhythmShahriar/tacet?style=flat-square&color=ff7c52&label=release"></a>
  <a href="https://github.com/RhythmShahriar/tacet/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/RhythmShahriar/tacet/ci.yml?branch=master&style=flat-square&label=ci"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-MIT-100f0d?style=flat-square"></a>
  <a href="go.mod"><img alt="Go" src="https://img.shields.io/github/go-mod/go-version/RhythmShahriar/tacet?style=flat-square&color=100f0d"></a>
</p>

<p align="center">
  <a href="#install">Install</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#what-it-does">What it does</a> ·
  <a href="#measured-savings">Measured savings</a> ·
  <a href="#honest-limits">Honest limits</a> ·
  <a href="#privacy">Privacy</a> ·
  <a href="docs/benchmark.md">Benchmark</a>
</p>

<p align="center">
  <img alt="A real tacet init run on a sample project, then tacet status listing four suggestions that each wait for a yes in /tacet: move a 5-step release procedure into a skill, delete a directory tree, import AGENTS.md and turn off commit credit lines" src="docs/assets/demo.svg" width="720">
</p>

## Install

macOS and Linux:

```sh
curl -fsSL https://rythm.sh/tacet/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://rythm.sh/tacet/install.ps1 | iex
```

Both scripts download the build for your system from the latest release, check it against the release's `checksums.txt` and, when [cosign](https://github.com/sigstore/cosign) is installed, check that `checksums.txt` was signed by tacet's release workflow. They install to `~/.local/bin` (Windows: `%LOCALAPPDATA%\Programs\tacet`); set `TACET_INSTALL_DIR` for another folder or `TACET_VERSION=0.1.0` for a specific version.

| With | Run |
|---|---|
| Homebrew (macOS, Linux) | `brew install rhythmshahriar/tap/tacet` |
| Scoop (Windows) | `scoop bucket add rhythmshahriar https://github.com/RhythmShahriar/scoop-bucket` then `scoop install tacet` |
| npm | `npm i -g @rhythmsh/tacet` |
| Go | `go install -tags gotreesitter_no_copyleft github.com/RhythmShahriar/tacet/cmd/tacet@latest` |

The npm package is a few kilobytes: on first run it downloads the binary for your system from the release, checks it against the SHA-256 published with the package and keeps it in your user cache, so `npx @rhythmsh/tacet init` works as well as a global install. `tacet update` replaces a binary from the install scripts; for Homebrew, Scoop, npm and Go it prints the command to update with instead.

To check a release archive by hand, run [`scripts/verify-release.sh`](scripts/verify-release.sh) next to the archive, `checksums.txt` and `checksums.txt.sigstore.json`.

## Quick start

```sh
cd your-project
tacet init
```

That's it. `tacet init` measures the project, shows the safe fixes and applies them after one yes (`--yes` skips the question), installs the Claude Code plugin and saves a baseline to compare against. Restart Claude Code and tacet works on its own from then on.

Inside Claude Code, type `/tacet` to see what it saved and the suggestions waiting for a yes. You accept or dismiss each one there. It works the same in the terminal and in the VS Code extension.

## What it does

Once `tacet init` has run, the plugin's hooks do this without being asked:

- **Long documents open small.** When Claude Code reads a PDF, Word, PowerPoint or Excel file, tacet converts it to Markdown once (cached) and hands over an outline plus the sections that match your request instead of the whole file.
- **Office files work at all.** Claude Code refuses Word, PowerPoint and Excel files before any hook runs, so tacet converts a document as soon as your prompt names it.
- **Secrets stay out.** Reads of `.env` files, keys and credential files are blocked. So are commands like `cat .env`, each with a short reason Claude Code can act on.
- **Handoff notes.** Before a compact and at session end tacet writes `SESSION.md` from the transcript and git. It is added to the context only when a prompt asks to continue.
- **Quiet re-audits.** Every 10 sessions it checks the setup again in the background and leaves one line for the next session when a suggestion is waiting.
- **Safe fixes only.** `tacet init` applies only changes it can undo, such as importing `AGENTS.md` from `CLAUDE.md`. Anything riskier (turning off an MCP server, hiding a skill, rewriting docs) waits as a suggestion.

## Measured savings

**Against other tools.** Nine tasks (PDF and Word questions, code navigation in Go, Python and Rust, fixing failing tests, continuing a session), three runs each on Claude Haiku, every answer checked:

| Tool | Cost per run | Tokens | Correct |
|---|---|---|---|
| **tacet** | **-38%** | **-41%** | 27/27 |
| rtk | -9% | -8% | 27/27 |
| serena | -4% | -2% | 27/27 |
| claude-mem | 0% | +2% | 26/26 |
| caveman | +4% | -1% | 26/27 |
| context-mode | +4% | +14% | 27/27 |
| graphify | +13% | +9% | 27/27 |

Compared with Claude Code and no tool. tacet is cheapest on seven of the nine tasks; rtk is cheaper on continuing a half-done change and serena on fixing a failing Rust test. Per-task numbers, method and how to rerun it: [docs/benchmark.md](docs/benchmark.md#against-other-tools).

**Per feature.**

Each feature is measured with A/B runs through `claude -p`, with and without tacet, three times each. A feature that doesn't save tokens without hurting answers is removed or stays off. Full method and how to repeat it: [docs/benchmark.md](docs/benchmark.md).

| Feature | Scenario | Tokens | Cost | Correct (with / without) |
|---|---|---|---|---|
| Documents | Question about one part of a 28-page PDF | **-56%** | **-65%** | 3/3 / 3/3 |
| Documents | Facts from start, middle and end of a 28-page PDF, plus a summary | **-69%** | **-72%** | 3/3 / 3/3 |
| Documents | Question about one part of a Word file | **-63%** | **-46%** | 3/3 / 3/3 |
| Handoff | "Continue where we left off" with a half-done change | **-45%** | **-26%** | 2/3 / 2/3 |
| Code map | Where a widely used function is defined and called, 430-file repo | **-57%** | **-81%** | 3/3 / 3/3 |
| Code map | Same, for a name grep finds at once | +3% | -10% | 3/3 / 3/3 |
| Audio | What a voice note says (one run each; without tacet it can't be done) | -34% | -70% | 1/1 / 0/1 |

Model: Claude Haiku. Claude Code 2.1.311. Date: 2026-10-29. Denying reads of lockfiles and build output was measured too and removed: it saved nothing and blocked answers.

## Honest limits

- **Token counts are estimates.** `tacet measure`, `tacet status` and the report count tokens offline. They are close, not exact.
- **Billed savings need data.** `tacet status` compares main-session requests only (subagents start small and fresh) and shows a figure only after 20 of them since init. Subagent usage is listed on its own line.
- **Savings depend on your setup.** The numbers above come from the scenarios listed. A project with a short `CLAUDE.md` and no documents has little to save.
- **Office files must be named.** Claude Code refuses Word, PowerPoint and Excel files before any hook runs, so tacet can only help when your prompt names the file or the file is there when the session starts.
- **Scanned PDFs pass through.** A PDF with no text layer is read by Claude Code as usual.
- **No statusline in VS Code.** The optional statusline segment shows only in the terminal. `/tacet` works in both.
- **Claude Code only, for now.** Codex and Cursor support is on the [roadmap](ROADMAP.md).

## Privacy

tacet runs on your machine and reads only your project, its Claude Code settings and its local transcripts. It sends nothing about your project anywhere. It uses the network for two things. One is the one-time download of the speech engine and model for audio, which you start with `tacet transcribe --setup` and which is checked against pinned checksums. The other is a check for a newer tacet release on GitHub, at most once a day. That check only asks; tacet never updates without your yes. Set `update: notify` or `update: off` in `~/.config/tacet/config.yaml`, or `TACET_NO_UPDATE_NOTIFIER=1`, to quiet or stop it. It never runs in CI. Its state lives in `.tacet/` in your project, which `tacet init` keeps out of git through `.git/info/exclude`.

## Commands

Most people only run `tacet init`. For a look or a change:

| Command | Does |
|---|---|
| `tacet` or `tacet status` | what tacet saved and what is waiting for you |
| `tacet report` | writes `.tacet/report.html` and opens it (`--no-open` to skip) |
| `tacet measure` | context loaded at startup and what past sessions billed |
| `tacet audit` | setup findings, each with a fix |
| `tacet docs lint` | checks `CLAUDE.md`, `AGENTS.md` and rules files |
| `tacet skills new "<what>"` | drafts a skill from a description (or the latest session with `--from-session`) and writes it after a yes |
| `tacet skills check` | skills that never trigger, run long or overlap, with the startup tokens each fix saves |
| `tacet on <feature>` / `tacet off <feature>` | turns a feature on or off for this project |
| `tacet undo` | reverts tacet's last change |
| `tacet uninstall` | removes tacet from this project and restores your files |
| `tacet update` | installs the latest release after a yes, once its signature and checksum verify (`--rollback` restores the previous binary) |

Features for `tacet on` and `tacet off`:

| Feature | Default | What it does |
|---|---|---|
| `convert` | on | PDF and Office files read as Markdown, long ones on an outline of their sections |
| `guard` | on | blocks reads of secrets |
| `handoff` | on | `SESSION.md` written before compact and at session end |
| `audio` | on | audio read as a transcript, asking before the first download |
| `map` | on | code map for the MCP tools |
| `skills` | on | suggests skills from repeated instructions |
| `bigoutput` | off | experimental: keeps long Bash and WebFetch output in `.tacet/out` and shows the agent the start, the end and the errors |
| `feedback` | off | runs the project's own linter or type checker (ruff, pyright, cargo check, tsc, go vet) on each edited file and shows the agent only the failures |
| `testgate` | off | runs the tests when the agent tries to finish after editing code and sends it back on failures, at most twice a session |

The same switches live in `.tacet/config.yaml`, with `audit_every` for the number of sessions between quiet re-audits. Every command takes `--json` for machine output, `-y` to skip prompts and `-C <dir>` to run in another folder.

## CI

`tacet check` runs the docs lint and the audit and exits 1 on a finding at or above `--fail-on` (high, medium, low or info; default medium). It judges only what is in the repository, so findings that need transcripts or your own Claude Code settings are left out.

```yaml
- uses: actions/setup-go@v5
  with:
    go-version: stable
- run: go install -tags gotreesitter_no_copyleft github.com/RhythmShahriar/tacet/cmd/tacet@latest
- run: tacet check --fail-on medium
```

Add `--json` for output another tool reads.

## Exit codes

| Code | Means |
|---|---|
| 0 | ok |
| 1 | `tacet check` found something at or above `--fail-on` |
| 2 | usage error |
| 3 | any other error |

## Uninstall

In each project where you ran `tacet init`:

```sh
tacet uninstall
```

It reverts every change tacet made there, removes the Claude Code plugin and the `/tacet` skill and deletes `.tacet/`. Then remove the binary the way you installed it:

| Installed with | Remove with |
|---|---|
| Install script | delete `tacet` from the folder it used (`~/.local/bin`, Windows: `%LOCALAPPDATA%\Programs\tacet`) |
| Homebrew | `brew uninstall --cask tacet` |
| Scoop | `scoop uninstall tacet` |
| npm | `npm uninstall -g @rhythmsh/tacet`, then delete the cached binary in `~/.cache/tacet/npm` (`~/Library/Caches/tacet/npm` on macOS, `%LOCALAPPDATA%\tacet\npm` on Windows) |
| Go | delete `tacet` from `$(go env GOPATH)/bin` |

## Contributing

```sh
git clone https://github.com/RhythmShahriar/tacet && cd tacet
make build         # bin/tacet
make hooks         # Conventional Commits check
make check         # lint, headers and tests
make test-scripts  # the install and release scripts against local fixtures
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for how features are measured before they ship. What comes next is in the [ROADMAP](ROADMAP.md). Found a vulnerability? Please [report it privately](https://github.com/RhythmShahriar/tacet/security/advisories/new).

## License

MIT

The binary links Go modules under the Apache License 2.0 and embeds a WebAssembly build of PDFium. Release archives carry their license and NOTICE files under `THIRD_PARTY/`; [THIRD_PARTY/README.md](THIRD_PARTY/README.md) lists them.

<p align="center"><sub>Made by Rhythm Shahriar</sub></p>
