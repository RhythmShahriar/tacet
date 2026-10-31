# tacet measure

Shows the context Claude Code loads at the start of every session and what your past sessions billed.

```sh
tacet measure
```

## What it shows

- **Loaded every session:** each instruction file, rules file, skill description and MCP tool list with its token count, plus the total. This sits on top of Claude Code's own prompt and is paid on every request.
- **Last sessions:** requests, average tokens per request, cache hit rate, output tokens, cache writes and reads, subagent requests and cache misses with the tokens they reprocessed.

It reads only local files and transcripts. Token counts are offline estimates.

## Flags

| Flag | Does |
|---|---|
| `--json` | machine output |

## See also

- [`tacet audit`](audit.md) says what to change
