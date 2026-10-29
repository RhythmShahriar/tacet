# Contributing to tacet

Thanks for helping. This page covers how tacet is built, how a feature earns its place and the
rules every change follows.

```sh
make build         # bin/tacet (version dev unless HEAD is on a release tag)
make hooks         # Conventional Commits check
make check         # lint, file headers and every Go test with the race detector
make fuzz          # every fuzz target for 30s (FUZZTIME=10m for longer)
make test-scripts  # install.sh and verify-release.sh against local fixtures, no network
make bench         # A/B runs through claude -p; writes docs/benchmark.md
```

tacet is its own Go module and builds with `GOWORK=off`; the Makefile sets it for you.

## The one rule: measure first

A feature ships on by default only when it saves tokens on real Claude Code runs without making
answers worse. `testbed/bench/ab.sh` runs the same prompt with and without tacet in turns.
[docs/benchmark.md](docs/benchmark.md) records every result, including the features that were
removed. A pull request that adds or changes an automatic behavior includes its A/B numbers. A
feature that can't show a saving stays off or goes.

The README quotes only numbers from `docs/benchmark.md`. Never write an estimate there as if it
were measured.

## Hooks

The plugin's hooks run on every read, prompt and session event, so they follow three rules:

- **Never break Claude Code.** A hook that fails logs to `.tacet/hook.log` and exits 0 with no
  output. When in doubt, let the original call through.
- **Stay fast.** Aim for under 30 ms per call. Slow work (re-audits, indexing) starts in the
  background.
- **Do only what the project turned on.** Every behavior checks its feature in
  `.tacet/config.yaml` first.

## Fixes and suggestions

A fix that `tacet init` applies on its own is marked `Safe` and must be undoable: it goes through
the `fix` package, which keeps a backup for `tacet undo`. Anything that removes or rewrites
something the person wrote (an MCP server, a skill, their docs) is a suggestion that waits for a
yes in `/tacet`.

## Tests

- Every docs lint rule and audit check has a positive and a negative fixture: a case it must
  report and one it must stay quiet on.
- Tests never contact the network and never run `claude`. Transcripts, settings and releases are
  fixtures in a temp folder; `CLAUDE_CONFIG_DIR` points tacet at them.
- Never write a real-looking credential into a file, not even a revoked one: GitHub push
  protection blocks the push. Build fake keys from parts at run time.
- Every parser of local or fetched text has a `Fuzz<Name>` target next to its tests.

## Writing

Help, messages, docs and comments use short plain sentences: no em dashes, no comma before "and"
or "but" and no AI attribution in code, docs, commits or generated files. "Claude Code" is named
only as the tool tacet works with. Every `.go`, `.yml`, `.sh` and `.ps1` file starts with the
copyright line and `SPDX-License-Identifier: MIT`; `make brand` checks both.

## Commits

[Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/), a lowercase subject of at
most 72 characters, small and focused. The scope names the area of the tree:

| Scope | Area |
|---|---|
| `core`, `cli`, `ui` | shared types, commands and the terminal theme |
| `claude`, `tokens`, `measure` | Claude Code's files, token estimates and measurements |
| `audit`, `docs`, `skills`, `memory` | setup findings, instruction files, skills and memory |
| `convert`, `audio`, `speak`, `guard`, `handoff`, `trim`, `map` | the engines the hooks run |
| `hook`, `mcp`, `plugin`, `report` | hooks, the MCP server, the plugin and the HTML report |
| `bench`, `testbed`, `ci`, `release`, `deps` | benchmarks, fixtures, workflows, packaging and dependencies |

`make hooks` enables the check locally; CI runs the same script on every commit and pull request
title.

## Reporting a vulnerability

Please don't open a public issue. See [SECURITY.md](SECURITY.md).
