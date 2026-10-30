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
- code map: symbols, callers and file outlines through MCP tools, never loaded upfront
- skills: drafts from instructions you repeat across sessions and from long procedures
- audio: voice notes and recordings read as a transcript, after a one-time checksummed download
- docs fix, init and sync: `AGENTS.md` as the one source, other tools' rules files kept in step

## Later

- Codex support
- Cursor support

Ideas and requests are welcome as issues.
