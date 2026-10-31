# tacet status

Shows what tacet saved on this project and the suggestions waiting for you. `tacet` with no command runs it too.

```sh
tacet status
```

Inside Claude Code, type `/tacet` for the same view. There you can accept or dismiss each suggestion in plain words.

## What it shows

- **Savings:** startup context before and after `tacet init`, billed tokens per main-session request against the baseline and the cache hit rate. The billed figure shows after 20 requests since init. Subagent usage is listed on its own line and left out of the comparison.
- **Idle misses:** requests that came after a long pause and had to write the whole context to the cache again, with the tokens each one cost and its session.
- **Suggestions:** fixes from the audit and the docs lint that wait for a yes, each with an id and what it saves.
- **Features:** which features are on.

Billed tokens per request grow with long sessions, so compare runs of similar work. Token counts are offline estimates: close, not exact.

## Flags

| Flag | Does |
|---|---|
| `--json` | machine output |

## See also

- [`tacet report`](report.md) for the same numbers as an HTML page with daily usage
- [`tacet statusline`](statusline.md) for savings in Claude Code's statusline
