# tacet audit

Lists findings about the project's Claude Code setup, each with a fix and what it saves.

```sh
tacet audit
```

## What it looks for

- MCP servers whose tools load into every session but are rarely used
- MCP tool search turned off
- skills that never trigger, long skill descriptions and skills that overlap
- `CLAUDE.md` importing a large `AGENTS.md`
- commit and PR credit lines you may not want
- cache misses in past sessions and what caused them
- the search subagent model

Each finding has an id. Apply one with `/tacet` inside Claude Code or dismiss it there so it stops showing. The session-end hook runs the audit again quietly every few sessions (`audit_every` in [configuration](../configuration.md)).

## Flags

| Flag | Does |
|---|---|
| `--json` | machine output |

## See also

- [`tacet docs`](docs.md) for the instruction files themselves
- [`tacet check`](check.md) runs the audit in CI
