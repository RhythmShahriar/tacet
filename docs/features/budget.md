# budget

**Default: off.** Warns when a session nears its token budget.

```sh
tacet on budget
```

## What it does

tacet tells you once per threshold when a session's tokens reach a share of the budget. It never blocks anything.

Set the budget in `.tacet/config.yaml`:

```yaml
budget:
  session_tokens: 2000000   # every token one session may process, cache reads included
  warn_at: [0.8, 1.0]       # warn at 80% and at 100%
  session_cost_usd: 5       # optional; needs the tacet statusline for Claude Code's cost estimate
```

The defaults are 2,000,000 tokens and warnings at 80% and 100%.

It ships off because it saves nothing by itself; it helps you notice a session that runs long.
