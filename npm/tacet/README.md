<p align="center">
  <img alt="tacet: one command makes Claude Code use fewer tokens on any project. 40% cheaper than Claude Code alone with the same answers, one command to set up." src="https://raw.githubusercontent.com/RhythmShahriar/tacet/master/docs/assets/hero-light.svg" width="760">
</p>

<p align="center">
  <a href="https://github.com/RhythmShahriar/tacet"><b>GitHub</b></a> ·
  <a href="https://github.com/RhythmShahriar/tacet/blob/master/docs/benchmark.md">Benchmark</a> ·
  <a href="https://github.com/RhythmShahriar/tacet#what-it-does">What it does</a> ·
  <a href="https://github.com/RhythmShahriar/tacet/releases">Releases</a>
</p>

Claude Code rereads its whole context on every turn: your `CLAUDE.md`, every tool, every document it opened. tacet trims what it loads and what it reads. Run `tacet init` once in a project. From then on it works through Claude Code's own hooks: long PDFs open on an outline, code is found by symbol, secrets stay out and a new session picks up where the last one stopped.

## Install

```sh
npm i -g @rhythmsh/tacet
cd your-project
tacet init
```

Restart Claude Code and type `/tacet` to see what it saved. `npx @rhythmsh/tacet init` works too.

## Results

<p align="center">
  <img alt="Cost per run against Claude Code alone, 10 tasks, 3 runs each on Claude Code 2.1.314: tacet -40%, caveman -21%, rtk -15%, serena -11%, Concise style -10%, context-mode -6%, graphify -3%, claude-mem +11%" src="https://raw.githubusercontent.com/RhythmShahriar/tacet/master/docs/assets/bench-light.svg" width="760">
</p>

Head to head with the six most popular tools of its kind on ten tasks, three runs each on Claude Code 2.1.314, every answer checked. tacet cost 40% less than Claude Code alone with the same answers. [Method and raw data](https://github.com/RhythmShahriar/tacet/blob/master/docs/benchmark.md#against-other-tools).

## What it does

- **Long documents open small.** PDF, Word, PowerPoint and Excel files open on an outline plus the sections that match your request.
- **Code found by symbol.** A code map answers "where is this defined and who calls it" without a grep through the tree.
- **Office and audio files work.** tacet converts a document or transcribes a voice note as soon as your prompt names it.
- **Secrets stay out.** Reads of `.env` files, keys and credentials are blocked.
- **Sessions continue.** `SESSION.md` hands the next session what the last one was doing.
- **Lean instruction files.** `CLAUDE.md` and `AGENTS.md` are checked for stale paths, duplicates and procedures that belong in a skill.

## About this package

It is a few kilobytes with no dependencies and no install scripts. The first time you run `tacet` it downloads the binary for your system from the GitHub release, checks it against the SHA-256 published with this package and keeps it in your user cache. The package is published from CI with npm provenance. Update with `npm i -g @rhythmsh/tacet@latest`.

Other installs (curl, PowerShell, Homebrew, Scoop, Go), docs and the source: [github.com/RhythmShahriar/tacet](https://github.com/RhythmShahriar/tacet).

Made by Rhythm Shahriar. MIT license.
