# How it works

Claude Code sends the whole context with every request: your instruction files, every tool definition and everything read so far. Prompt caching makes the repeats cheap, not free. A long document read once is paid on every turn after. tacet makes that context smaller in two places: what loads at startup and what gets read during the work.

## The four steps

1. **Measure.** `tacet init` counts what every session loads before you type and what your past sessions billed. That is the baseline.
2. **Fix what is safe.** It applies only changes it can undo, after one yes. Everything else waits as a suggestion.
3. **Work on its own.** A Claude Code plugin runs tacet at the right moments.
4. **Prove it.** `/tacet`, [`tacet status`](commands/status.md) and [`tacet report`](commands/report.md) compare your sessions with the baseline.

## The plugin

`tacet init` installs a Claude Code plugin from a local marketplace. It has three parts.

**Hooks**, each a call to the `tacet` binary that takes about 10 milliseconds:

| When | tacet |
|---|---|
| a session starts | refreshes the code index in the background, checks for an update once a day |
| you send a prompt | converts named Office and audio files, adds `SESSION.md` when you ask to carry on |
| a file is read | opens long documents on an outline, blocks secrets |
| a search runs | points broad searches at the code map |
| a shell command runs | blocks commands that print secrets |
| a command or fetch returns | with [bigoutput](features/bigoutput.md) on, keeps long output on disk |
| a file is edited | with [feedback](features/feedback.md) on, runs the checker |
| the agent finishes | with [testgate](features/testgate.md) on, runs the tests |
| a session compacts or ends | writes `SESSION.md`, re-runs the audit every few sessions |

**MCP tools**: `symbols`, `callers`, `outline` and `map`, served by `tacet mcp` from the code index. See [map](features/map.md).

**The `/tacet` skill**: shows the status and applies or dismisses suggestions when you say so. It is never loaded unless you type `/tacet`.

## What tacet changes

- **Your files**, only after a yes and only with changes it can undo: an applied suggestion, a docs fix, a feature switch. [`tacet undo`](commands/undo.md) reverts the last one; [`tacet uninstall`](commands/uninstall.md) reverts all of them.
- **Its own state** in `.tacet/` in the project: config, converted documents, the code index, saved output and the change log. tacet adds `.tacet/` to `.git/info/exclude`, so it stays out of git without touching your `.gitignore`.
- **`SESSION.md`**, written at compact and session end and kept out of git the same way.
- **Claude Code's plugin list.** With [`tacet statusline --install`](commands/statusline.md) also your statusline setting.

## What it never does

- Send anything about your project anywhere. The network is used only for the audio model download you start and the daily release check, which only asks.
- Update itself without a yes.
- Credit itself in your commits or files.

## How features earn their place

Every feature is measured with A/B runs through `claude -p` on real tasks, every answer checked. A feature that doesn't save tokens without hurting answers is removed or ships off. See the [benchmark](benchmark.md).
