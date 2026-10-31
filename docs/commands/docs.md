# tacet docs

Keeps the instruction files lean: `CLAUDE.md`, `AGENTS.md` and rules files are loaded into every session, so every line in them is paid on every request.

```sh
tacet docs lint
tacet docs fix
tacet docs init
tacet docs sync
```

## tacet docs lint

Checks the instruction files and prints each finding with its file and line. `tacet docs` with no subcommand runs it.

| Rule | Finds |
|---|---|
| `size/lines`, `size/vendor` | files too long to load every session, vendored text pasted in |
| `import/missing`, `import/depth` | `@imports` that point nowhere or nest too deep |
| `agents/shadowed` | an `AGENTS.md` that Claude Code never loads because `CLAUDE.md` doesn't import it |
| `stale/path`, `stale/command` | paths and commands that no longer exist in the repo |
| `derivable/tree` | directory trees Claude Code can list for itself |
| `move/procedure` | long step-by-step procedures that belong in a skill, loaded only when needed |
| `dup/line` | lines repeated across files |
| `contradiction` | one file says do, another says don't |
| `vague` | instructions too vague to act on |

## tacet docs fix

Shows each fix as a diff with the tokens it takes off startup and applies the ones you confirm. Every fix can be reverted with [`tacet undo`](undo.md).

| Flag | Does |
|---|---|
| `--safe` | only the fixes that change no meaning |
| `--dry-run` | shows the fixes and writes nothing |
| `-y`, `--yes` | applies without asking |

## tacet docs init

Writes lean `AGENTS.md`, `CLAUDE.md` and `ARCHITECTURE.md` templates for the files a project is missing. It never touches a file that exists.

## tacet docs sync

Copies `AGENTS.md` into the files other coding agents read: `.cursor/rules/agents.mdc`, `.github/copilot-instructions.md` and `GEMINI.md`. A generated file edited by hand is reported instead of overwritten.

| Flag | Does |
|---|---|
| `--to cursor,copilot,gemini` | which files to write (default: the ones that exist) |
| `--force` | overwrites files edited by hand |
| `--check` | exits 1 when a file is out of date and writes nothing, for CI |
| `-y`, `--yes` | writes without asking |

## See also

- [`tacet check`](check.md) runs the lint in CI
