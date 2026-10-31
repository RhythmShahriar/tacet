# tacet skills

Turns requests you repeat into Claude Code skills and keeps the skills you have lean.

```sh
tacet skills
tacet skills new "release a new version"
tacet skills add owner/repo/path/to/skill
tacet skills check
```

## tacet skills

Lists skill ideas from requests that repeat across your past sessions, with how often each came up.

## tacet skills new

Drafts a skill and writes it under `.claude/skills/` after a yes.

| Flag | Does |
|---|---|
| `"<what>"` | what the skill should do, in your words |
| `--from-session` | drafts it from the latest session instead |
| `--transcript <file>` | drafts it from a given transcript |
| `--name <name>` | the skill's folder name |
| `--json` | shows the draft and writes nothing |

## tacet skills add

Imports a public skill from GitHub, pinned to a commit, after a yes.

```sh
tacet skills add owner/repo/path[@ref]
```

| Flag | Does |
|---|---|
| `--list` | shows example skills to import |
| `--allow-scripts` | allows a skill that ships scripts (refused by default) |
| `--json` | shows what would be written; writes only with `--yes` |

It never overwrites a skill you have.

## tacet skills check

Lists skills that never trigger, descriptions near or over Claude Code's cap and skills that overlap, with the startup tokens each fix saves.

## See also

- [skills](../features/skills.md)
