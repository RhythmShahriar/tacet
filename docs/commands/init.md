# tacet init

Sets tacet up on a project. Run it once in each project; after that tacet works on its own inside Claude Code.

```sh
cd your-project
tacet init
```

## What it does

1. **Measures** what every session loads before you type (your `CLAUDE.md`, rules, skills, MCP tools) and what your past sessions billed. That becomes the baseline [`tacet status`](status.md) compares against.
2. **Asks one question:** which model the read-only search subagent uses. See [subagents](../features/subagents.md).
3. **Applies only safe changes** after one yes. Each one can be reverted with [`tacet undo`](undo.md). Everything else becomes a suggestion that waits for you in `/tacet`.
4. **Installs the Claude Code plugin** that runs tacet's hooks, the code tools over MCP and the `/tacet` skill.

Restart Claude Code afterwards so it loads the plugin.

Running `tacet init` again is safe: it measures again and adds what is missing.

## Flags

| Flag | Does |
|---|---|
| `--subagents haiku\|sonnet\|main` | answers the subagent question without asking |
| `--no-plugin` | sets up the project without installing the Claude Code plugin |
| `-y`, `--yes` | applies the safe changes without asking and picks Haiku for subagents |
| `--json` | prints the result as JSON and asks nothing |

## See also

- [Getting started](../getting-started.md)
- [How it works](../how-it-works.md)
- [`tacet uninstall`](uninstall.md)
