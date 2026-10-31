# subagents

**Default: off.** `tacet init` asks whether to turn it on. Cheaper search subagents.

```sh
tacet subagents haiku
```

## What it does

Claude Code sends wide file searches to its built-in Explore subagent, which runs on your main model. With this feature tacet writes an Explore agent that runs on Haiku or Sonnet. Only that read-only search agent changes: your main model still plans, decides and writes the code.

| Choice | Means |
|---|---|
| `haiku` | cheapest; recommended for searches |
| `sonnet` | in between |
| `main` | your main model, as without tacet (turns the feature off) |

`tacet init` asks once. Change it later with [`tacet subagents`](../commands/subagents.md). If you have your own Explore agent, tacet leaves it alone.

## Measured

On a large search a subagent does, it halved the cost with the same answers. With search on the main model, tacet's total saving in the head-to-head drops from -40% to -6%. See the [benchmark](../benchmark.md).
