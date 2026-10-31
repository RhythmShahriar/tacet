# skills

**Default: on.** Requests you repeat become skill suggestions.

```sh
tacet off skills
```

## What it does

A skill is loaded only when it is needed, so a procedure in a skill costs nothing in sessions that don't use it. tacet watches for instructions you give again and again across sessions and suggests a skill draft for each. The suggestion waits in `/tacet` and `tacet status`; nothing is written without a yes.

See [`tacet skills`](../commands/skills.md) to draft, import or check skills by hand.
