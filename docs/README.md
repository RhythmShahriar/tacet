# tacet docs

tacet makes Claude Code use fewer tokens on any project. Run `tacet init` once; from then on it works through Claude Code's own hooks.

## Start here

- [Getting started](getting-started.md): install, set up a project, see what it saved
- [How it works](how-it-works.md): the hooks, what tacet changes and how to undo it
- [Configuration](configuration.md): `.tacet/config.yaml` and the global settings
- [CI](ci.md): `tacet check` in GitHub Actions
- [FAQ](faq.md)
- [Benchmark](benchmark.md): every measurement and how to repeat it

## Features

| Feature | Default | Does |
|---|---|---|
| [convert](features/convert.md) | on | long PDF and Office files open on an outline |
| [guard](features/guard.md) | on | blocks reads of secrets |
| [handoff](features/handoff.md) | on | `SESSION.md` carries work to the next session |
| [audio](features/audio.md) | on | audio read as a transcript |
| [map](features/map.md) | on | code found by symbol |
| [skills](features/skills.md) | on | repeated requests become skill suggestions |
| [subagents](features/subagents.md) | off (init asks) | search subagent on Haiku or Sonnet |
| [budget](features/budget.md) | off | warns as a session nears its token budget |
| [bigoutput](features/bigoutput.md) | off | long command output kept on disk, shown short |
| [feedback](features/feedback.md) | off | lint and type errors after each edit |
| [testgate](features/testgate.md) | off | failing tests keep the agent from finishing |

## Commands

Most people only run `tacet init`. Every command takes `--json` for machine output, `-y` to skip prompts and `-C <dir>` to run in another folder.

| Command | Does |
|---|---|
| [`tacet init`](commands/init.md) | sets tacet up on a project, once |
| [`tacet status`](commands/status.md) | what tacet saved and what is waiting for you |
| [`tacet on`](commands/on.md) | turns a feature on |
| [`tacet off`](commands/off.md) | turns a feature off |
| [`tacet undo`](commands/undo.md) | reverts tacet's last change |
| [`tacet uninstall`](commands/uninstall.md) | removes tacet from a project |
| [`tacet measure`](commands/measure.md) | context loaded at startup and what sessions billed |
| [`tacet audit`](commands/audit.md) | setup findings, each with a fix |
| [`tacet map`](commands/map.md) | ranked code map |
| [`tacet docs`](commands/docs.md) | lint, fix, create and sync instruction files |
| [`tacet subagents`](commands/subagents.md) | which model the search subagent uses |
| [`tacet skills`](commands/skills.md) | draft, import and check skills |
| [`tacet clean`](commands/clean.md) | removes agent worktrees with no unsaved work |
| [`tacet check`](commands/check.md) | docs lint and audit for CI |
| [`tacet transcribe`](commands/transcribe.md) | audio to a Markdown transcript |
| [`tacet report`](commands/report.md) | HTML report of what tacet saved |
| [`tacet statusline`](commands/statusline.md) | savings in Claude Code's statusline |
| [`tacet update`](commands/update.md) | updates to the latest signed release |
| [`tacet version`](commands/version.md) | prints the version |

<p align="center"><sub>Made by Rhythm Shahriar</sub></p>
