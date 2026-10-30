# Head-to-head benchmark

tacet against the six most popular token tools for Claude Code and
against no tool at all, on the same tasks with the same model. The
headline is **cost per correct answer**: what all runs of a mode cost,
divided by the runs that passed. Billed tokens, output tokens and turns
sit beside it.

`ab.sh` and `summary.sh` are the older two-way A/B (tacet on or off)
behind [docs/benchmark.md](../../docs/benchmark.md). This folder adds
the many-way comparison.

## Reproduce

```sh
testbed/bench/selftest.sh                  # every task's check, no model, free
testbed/bench/compare.sh go-nav none 3     # one task, one mode, 3 runs
testbed/bench/all.sh 3                     # every task x every mode x 3 runs
testbed/bench/table.sh                     # Markdown tables from the newest results
```

`compare.sh <task> <mode> [reps]` sets the mode up on first use, then
for each run:

1. builds a fresh copy of the task's project in a temp folder (`tasks/<task>/setup.sh`)
2. lets the mode prepare it, if it works per project (`setup/<mode>.sh prepare`)
3. runs `claude -p --model haiku --output-format json` with a unique
   prefix on the prompt (`[bench-<random>]`), so no run reuses another
   run's prompt cache
4. checks the result with `tasks/<task>/check.sh`: the answer must hold
   every expected fact, or the tests must pass with the test files untouched
5. appends one JSON line to `results/<date>.jsonl` and keeps the raw
   `claude -p` output under `results/raw/<date>/`

Environment: `BENCH_MODEL` (default `haiku`), `BENCH_MAX_TURNS` (30),
`BENCH_TIMEOUT` (600 s), `BENCH_KEEP=1` keeps each run's project,
`BENCH_RESULTS` moves the results, `BENCH_MODES` limits `all.sh`.

### What a result line holds

`mode`, `task`, `rep`, `model`, `billed` (input + cache write + cache
read, summed over every model the run used), `input`, `cache_write`,
`cache_read`, `output`, `cost` (`total_cost_usd`), `turns`,
`duration_ms`, `pass`, `denials` (tool calls refused by the allow list),
`subtype` (`success` or `error_max_turns`), `error` (the API or Claude Code failed the run), `tool_version`,
`claude_version`. A run that crashed is a line with `error: true` and the
end of its stderr; it counts as a failure.

### Login

Each mode runs with its own `HOME` and `CLAUDE_CONFIG_DIR` under
`state/<mode>/` (gitignored). The login is reused without copying it:
setup puts a symlink to `~/.claude/.credentials.json` there. If
`CLAUDE_CODE_OAUTH_TOKEN` (from `claude setup-token`) or
`ANTHROPIC_API_KEY` is set, no link is made at all; that is the safer
choice for long runs, since a login refreshed inside a mode's folder
can replace the link with a file (compare.sh warns when that happens).
Nothing is printed or committed.

## Modes and pinned versions

| Mode | Version | Installed as its README says | Extra allowed tools |
|---|---|---|---|
| none | Claude Code as installed | nothing | none |
| tacet | this checkout | built from source; `tacet init --yes` once to install the plugin, then in each project | `mcp__plugin_tacet_tacet` |
| tacet-terse | this checkout | as tacet, then `tacet on terse` in each project (output style `tacet:terse`) | `mcp__plugin_tacet_tacet` |
| concise | Claude Code as installed (v2.1.237 or later) | nothing; `outputStyle` `Concise` in each project's settings.local.json | none |
| rtk | 1.1.0 | release binary (sha256 pinned), `rtk init -g` | none (Bash hook) |
| caveman | v4.0.1 | `claude plugin marketplace add` + `claude plugin install caveman@caveman`, from a checkout of the tag | its MCP servers, if any |
| context-mode | v1.0.169 | `claude plugin marketplace add` + `claude plugin install context-mode@context-mode`, from the tag | `mcp__plugin_context-mode_context-mode` |
| claude-mem | 15.6.0 | `npx claude-mem@15.6.0 install --provider claude --no-auto-start`, sign-in skipped | `mcp__plugin_claude-mem_mcp-search` |
| graphify | 0.9.97 | `uv tool install graphifyy`, `graphify install`; per project `graphify update .` then `graphify claude install` | none (CLI through Bash, hooks) |
| serena | 2.1.0 | `uv tool install -p 3.13 serena-agent`, `claude mcp add --scope user serena -- serena start-mcp-server --context claude-code --project-from-cwd` | `mcp__serena` |

uv is 0.13.5 (sha256 pinned) and lives in each mode's own folder.
Serena's gopls is v0.23.0.

## Tasks

All inputs are public or written for this bench. Repositories are shallow
clones at a fixed commit; the document text is pinned by sha256.

| Task | What it asks | Input | Passes when |
|---|---|---|---|
| pdf-part | one fact pair from one section | 43-page PDF made with LibreOffice from RFC 6749, with made-up implementer notes | both facts of the note in 4.4 |
| pdf-whole | three code names from the start, middle and end, with their sections | the same PDF | all three names and sections 1.2, 4.3, Appendix B |
| docx | one fact pair from one section | the same text as DOCX with real heading styles | both facts of the note in 6 |
| go-nav | where a function is defined and every non-test caller | caddy v2.9.1 (279 Go files) | all six files named |
| py-nav | the same | click 8.1.7 | all three files named |
| rust-nav | the same | ripgrep 14.1.1 | all three files named |
| py-fix | fix a failing pytest suite without touching the tests | a small invoice package with one seeded bug | tests untouched and pytest passes |
| rust-fix | fix a failing cargo test without touching the tests | a small span-set crate with one seeded bug | tests untouched and cargo test passes |
| continue | "Continue where we left off." | a Go package halfway through a change, with tests committed, the code uncommitted and a SESSION.md in tacet's format | tests untouched, go test passes and the README documents the new function (a step only the note names) |

The notes in the documents are invented, so no model can answer from
memory. Every check has a negative case (the untouched project fails)
and a positive one (the known answer or fix passes): `selftest.sh` runs
both.

## Fairness rules

- Every tool runs at its default settings, installed the way its own
  README says, at the pinned version above, in its own config folder.
- Every mode gets the same allowed tools: Read, Grep, Glob, Bash, Edit
  and Write, plus that tool's own MCP tools. Same model, same turn cap,
  same timeout, same prompt.
- Every prompt starts with a unique id, so each run pays for a first
  read as a new session does. Claude Code's own system prompt and tool
  list come before the prompt, so their cache is shared between runs
  that follow each other closely, as it is between real sessions;
  rotating the mode order spreads that evenly.
- `all.sh` interleaves reps and rotates the mode order each rep.
- Failures, crashes and timeouts are reported as they happened. Nothing
  is retried or dropped.
- When the machine lacks pytest or cargo, `shims/` runs them in Docker
  (python:3.12-slim with pytest 8.3.3, rust:1.83-slim) for every mode
  alike, so the agent can run the tests it is asked to fix.

### Known limits

- Index builds are not counted for any tool: tacet indexes inside its
  hooks, graphify's graph is built in `prepare` with no LLM
  (`GRAPHIFY_BACKEND=none`). Graphify's semantic pass over documents
  needs an LLM, so on the document tasks it has no graph.
- claude-mem's memory provider is the person's own Claude login. Its
  background summaries run outside `claude -p`, so they are not in the
  reported cost. Its memory starts empty: every run is a first session.
- Serena's Rust support needs rust-analyzer and a Rust toolchain; with
  neither on the machine it falls back to the shared tools on rust-nav.
- claude-mem and serena may leave a worker process running after a run.
- context-mode v1.0.169 failed every request on Claude Code 2.1.92 in the
  smoke test: the API refused one of its MCP tools (`input_schema does
  not support oneOf, allOf, or anyOf at the top level`). That is
  recorded as a crash, not fixed up.
