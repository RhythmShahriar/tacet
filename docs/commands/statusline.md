# tacet statusline

Adds tokens saved today, cache hits, context use and rate limits to Claude Code's statusline.

```sh
tacet statusline --install
tacet statusline --install --chain   # keeps the statusline you already have
```

`--install` writes the statusline command to your Claude Code settings. Claude Code then runs `tacet statusline` with the session's JSON on stdin and shows the line it prints.

The statusline shows only in the terminal. The VS Code extension doesn't display it; use `/tacet` or [`tacet status`](status.md) there.

## Flags

| Flag | Does |
|---|---|
| `--install` | sets tacet as Claude Code's statusline |
| `--chain` | with `--install`, keeps your own statusline command and adds tacet's part after it |
