# tacet check

Runs the docs lint and the audit for CI and exits 1 on a finding at or above `--fail-on`. It judges only what is in the repository, not your machine.

```sh
tacet check --fail-on medium
```

## Flags

| Flag | Does |
|---|---|
| `--fail-on high\|medium\|low\|info` | the lowest severity that fails (default `medium`) |
| `--json` | machine output |

Exit codes: 0 ok, 1 a finding at or above `--fail-on`, 2 usage error, 3 any other error.

See [CI](../ci.md) for a GitHub Actions example.
