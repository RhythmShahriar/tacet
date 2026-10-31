# map

**Default: on.** Code found by symbol instead of a grep through the tree.

```sh
tacet off map
```

## What it does

tacet keeps an index of the project's symbols and calls in `.tacet/` and serves it to Claude Code through four MCP tools:

| Tool | Answers |
|---|---|
| `symbols` | where a name is defined |
| `callers` | who calls it |
| `outline` | the symbols in one file, with line numbers |
| `map` | the most important files and symbols in the project |

When Claude Code starts a broad search, tacet points it at these tools. The index updates in the background when a session starts.

Languages: Go, TypeScript, JavaScript, Python, PHP, Rust, Java, Ruby, C, C++, C# and Kotlin.

## Measured

| Scenario | Tokens | Cost |
|---|---|---|
| Where a widely used function is defined and called, 430-file repo | -57% | -81% |
| The same for a name grep finds at once | +3% | -10% |

The tools add about 450 tokens to each request, so on quick lookups in small repos Claude Code alone can be a little cheaper. See the [benchmark](../benchmark.md).

See also [`tacet map`](../commands/map.md).
