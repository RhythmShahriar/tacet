# feedback

**Default: off.** Lint and type errors right after an edit.

```sh
tacet on feedback
```

## What it does

After each edit, tacet runs the project's own checker on the changed file (ruff, pyright, cargo check, tsc or go vet, whichever the project uses) and shows Claude Code only the failures, up to 40 lines. A checker that takes over 20 seconds says nothing.

It ships off because the benchmark showed no gain: on the two fix tasks it cost 8% (Python) and 17% (Rust) more than tacet without it.
