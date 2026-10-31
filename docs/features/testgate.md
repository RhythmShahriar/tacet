# testgate

**Default: off.** Failing tests keep the agent from finishing.

```sh
tacet on testgate
```

## What it does

When Claude Code tries to finish after editing code, tacet runs the project's tests. On failures it sends the agent back with them, at most twice a session.

It ships off because the benchmark showed no gain on the two fix tasks and it adds a test run to every finished task.
