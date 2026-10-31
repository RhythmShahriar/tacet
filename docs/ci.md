# CI

[`tacet check`](commands/check.md) runs the docs lint and the audit and exits 1 on a finding at or above `--fail-on`. It judges only what is in the repository.

## GitHub Actions

```yaml
- uses: actions/setup-go@v5
  with:
    go-version: stable
- run: go install -tags gotreesitter_no_copyleft github.com/RhythmShahriar/tacet/cmd/tacet@latest
- run: tacet check --fail-on medium
```

To keep the Cursor, Copilot and Gemini files in step with `AGENTS.md`:

```yaml
- run: tacet docs sync --check
```

Exit codes: 0 ok, 1 a finding at or above `--fail-on`, 2 usage error, 3 any other error.

tacet never checks for updates in CI.
