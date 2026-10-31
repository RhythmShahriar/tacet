# handoff

**Default: on.** A new session picks up where the last one stopped.

```sh
tacet off handoff
```

## What it does

Before Claude Code compacts a session and when a session ends, tacet writes `SESSION.md` from the transcript and git: what was being done, the files changed, what is left. It stays under 60 lines and out of git.

The note is added to a new session only when your prompt asks to carry on ("continue where we left off", "pick up from yesterday"), so other sessions don't pay for it. Notes older than three days are skipped.

You can edit `SESSION.md`; your edits stay until the next write.

## Measured

"Continue where we left off" with a half-done change: -45% tokens, -26% cost, same answers. See the [benchmark](../benchmark.md).
