# tacet subagents

Shows or sets the model the read-only search subagent runs on.

```sh
tacet subagents          # shows the current choice
tacet subagents haiku
tacet subagents sonnet
tacet subagents main
```

Claude Code sends wide file searches to its built-in Explore subagent, which runs on your main model. `haiku` or `sonnet` runs only that read-only agent on a cheaper model; your main model still plans and writes the code. `main` puts it back on your main model.

`tacet init` asks this once. If you already have your own Explore agent, tacet leaves it alone.

See [subagents](../features/subagents.md) for what it saved in the benchmark.
