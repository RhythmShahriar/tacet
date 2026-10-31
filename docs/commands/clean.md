# tacet clean

Removes the git worktrees Claude Code agents leave behind when they hold no unsaved work. Then it prints the space freed.

```sh
tacet clean --dry-run   # the plan, nothing removed
tacet clean             # removes after a yes
tacet clean --all       # every project Claude Code knows
```

An agent that runs in a worktree leaves a full checkout under `.claude/worktrees/`, `node_modules` and build output included. On a busy machine these add up to many gigabytes.

## When a worktree is removed

Only when all of these hold:

- **No uncommitted change** in tracked or untracked files.
- **No commit that exists nowhere else.** Every commit is on another branch or the remote.
- **Not locked** by a running agent.
- **Not changed in the last hour.**
- **No ignored file you would lose.** Git-ignored files such as `.env.local` or test logins are compared with the main checkout. One that is missing there or differs keeps the worktree. Files a build recreates (`node_modules`, `dist`, `.next`, caches, logs) don't count.

Its branch is deleted only when git agrees it is merged. Everything kept is listed with the reason, ignored files with their names. Stale worktree entries and tacet's own old caches are cleaned along the way.

## Flags

| Flag | Does |
|---|---|
| `--dry-run` | lists what would go and what stays; removes nothing |
| `--all` | covers every project in Claude Code's history, not only this one |
| `--older-than 7d` | skips worktrees changed more recently (also `2w`, `12h`) |
| `--rescue` | copies the ignored files that would keep a worktree to `.tacet/rescued/<worktree>/` first, then removes it |
| `--force <worktree>` | removes a worktree you name even with unsaved work, after a yes |
| `--transcripts` | deletes session transcripts older than 90 days instead (`--older-than` sets the age) |
| `-y`, `--yes` | removes without asking |
| `--json` | machine output |

Deleted transcripts drop out of [`tacet status`](status.md), [`tacet measure`](measure.md) and skill ideas, which read them.

## See also

- [How it works](../how-it-works.md)
