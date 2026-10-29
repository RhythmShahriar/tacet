# Roadmap

tacet ships a feature on by default only after the [benchmark](docs/benchmark.md) shows it saves
tokens without hurting answers. Everything below is measured before it is turned on.

## Shipped

- `tacet init`: baseline, safe fixes after one yes, the plugin and `/tacet`
- documents: PDF, Word, PowerPoint and Excel read as Markdown, long ones on an outline plus the
  matching sections
- guard: reads of secrets blocked
- handoff: `SESSION.md` before compact and at session end, added only when a prompt asks to
  continue
- docs lint and audit, `tacet check` for CI
- `tacet report`: the HTML report

## In flight

- **Code map.** A ranked map of the code within a token budget, served on demand through MCP
  tools and never loaded upfront.
- **Skills.** Drafts skills from instructions you repeat across sessions and checks the skills
  you have: descriptions too long, overlapping, never used.
- **Audio.** Audio files read as a transcript, with a one-time confirmed and checksummed model
  download.
- **Docs sync.** `AGENTS.md` as the one source, with `CLAUDE.md` and other tools' rules files
  kept in step and drift reported.

## Later

- Codex support
- Cursor support

Ideas and requests are welcome as issues.
