# FAQ

## Does it change my files?

Only after a yes and only with changes it can undo: [`tacet undo`](commands/undo.md) reverts the last one and [`tacet uninstall`](commands/uninstall.md) reverts all of them. It writes its own state to `.tacet/` and `SESSION.md`; both stay out of git.

## How much does it save?

In the head-to-head on ten tasks it cost 40% less than Claude Code alone with the same answers. Your saving depends on your setup: a project with a short `CLAUDE.md` and no documents has little to save. `/tacet` shows the figure from your own sessions after 20 requests. See the [benchmark](benchmark.md).

## Is Haiku good enough for search?

Only the read-only search subagent moves to Haiku. It finds files and reports back; your main model still reads what matters, plans and writes the code. If you prefer, run `tacet subagents sonnet` or `tacet subagents main`. See [subagents](features/subagents.md).

## How is it different from rtk, caveman or claude-mem?

Those tools each work on one thing: shorter command output, shorter replies, memory between sessions. tacet works on what Claude Code loads and reads across documents, code, instruction files and sessions. It keeps only the features that measured a saving. In the head-to-head the others ranged from 21% less to 11% more than Claude Code alone.

## Does it slow Claude Code down?

A hook call takes about 10 milliseconds. Converting a document happens once and is cached.

## Does it work in VS Code?

Yes. The plugin, `/tacet` and the hooks work the same in the VS Code extension. The statusline shows only in the terminal.

## Does it work with Codex or Cursor?

Not yet; they are on the [roadmap](../ROADMAP.md). [`tacet docs sync`](commands/docs.md) already keeps their instruction files in step with `AGENTS.md`.

## Why are the token counts estimates?

`tacet measure`, `tacet status` and the report count tokens offline, without calling any API. They are close, not exact. Billed figures come from Claude Code's own transcripts.

## A hook isn't running. What do I check?

1. Restart Claude Code after `tacet init`.
2. Run `tacet status` in the project: it says whether tacet is set up there.
3. Make sure `tacet` is installed globally, not only through `npx`.
4. Run `tacet init` again; it adds what is missing.

## How do I report a bug or a vulnerability?

Bugs: [open an issue](https://github.com/RhythmShahriar/tacet/issues). Vulnerabilities: [report them privately](https://github.com/RhythmShahriar/tacet/security/advisories/new).
