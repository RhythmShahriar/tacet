# tacet map

Updates the code index and prints a ranked map of the project: the files and symbols that matter most, within a token budget.

```sh
tacet map
tacet map --budget 2000 --dir internal
```

The same index answers the plugin's MCP tools (`symbols`, `callers`, `outline`, `map`), which Claude Code uses to find code by name instead of a grep through the tree. See [map](../features/map.md).

## Flags

| Flag | Does |
|---|---|
| `--budget N` | tokens for the map (default 1024) |
| `--dir path` | maps only that folder |
| `--json` | machine output |
