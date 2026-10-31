# Getting started

## 1. Install

| On | Run |
|---|---|
| macOS, Linux | `curl -fsSL https://rythm.sh/tacet/install.sh \| sh` |
| Windows | `irm https://rythm.sh/tacet/install.ps1 \| iex` |
| Homebrew | `brew install rhythmshahriar/tap/tacet` |
| Scoop | `scoop bucket add rhythmshahriar https://github.com/RhythmShahriar/scoop-bucket` then `scoop install tacet` |
| npm | `npm i -g @rhythmsh/tacet` |
| Go | `go install -tags gotreesitter_no_copyleft github.com/RhythmShahriar/tacet/cmd/tacet@latest` |

Install globally rather than through `npx`: the plugin's hooks call the `tacet` binary on every tool use.

## 2. Set up a project

```sh
cd your-project
tacet init
```

It measures your setup, asks which model search subagents should use (Haiku is cheapest; your main model still writes the code), applies the safe fixes after one yes and installs the Claude Code plugin. Details: [`tacet init`](commands/init.md).

## 3. Restart Claude Code

Claude Code loads the plugin at start. It works the same in the terminal and in the VS Code extension.

## 4. Work as usual

Nothing new to learn. tacet runs when Claude Code reads a file, searches, gets a prompt that names a document or compacts a session. You see a short `tacet:` line when it steps in.

## 5. See what it saved

Type `/tacet` inside Claude Code, or run:

```sh
tacet status
tacet report      # the same as an HTML page
```

Suggestions that need your yes wait there. Accept or dismiss each one in `/tacet`.

## Next

- [How it works](how-it-works.md)
- [Features](README.md#features) to turn on or off
- [`tacet clean`](commands/clean.md) to get disk space back from old agent worktrees
