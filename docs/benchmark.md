# Benchmark

Every tacet feature has to save tokens on real Claude Code runs without
making answers worse. A feature that doesn't is removed or stays off by
default. This page lists each measurement and how to repeat it.

## Method

- `testbed/bench/ab.sh` runs the same prompt through `claude -p` without
  tacet and with tacet's plugin, alternating, three times each.
- Every prompt starts with a unique id, so no run reuses another run's
  prompt cache: each one pays for a first read, as a new session does.
- **Tokens** is everything the runs processed: input, cache writes and
  cache reads. **Cost** is what Claude Code reported (`total_cost_usd`),
  which weighs cache writes high and cache reads low, as your bill and
  usage limits do.
- An answer counts as correct only when it holds every expected fact.
- Model: Claude Haiku. Claude Code 2.1.311. Date: 2026-10-29.

## Results

| Feature | Scenario | Tokens | Cost | Turns | Correct (with / without) | Verdict |
|---|---|---|---|---|---|---|
| Documents | Question about one part of a 28-page PDF | **-56%** | **-65%** | 5.3 → 2 | 3/3 / 3/3 | on |
| Documents | Facts from start, middle and end of a 28-page PDF, plus a summary | **-69%** | **-72%** | 7.3 → 2 | 3/3 / 3/3 | on |
| Documents | Question about one part of a Word file (real heading styles) | **-63%** | **-46%** | 6.3 → 2 | 3/3 / 3/3 | on |
| Handoff | "Continue where we left off" with a half-done change | **-45%** | **-26%** | 3 → 1.7 | 2/3 / 2/3 | on, only on prompts that ask to continue |
| Code map (MCP tools) | Where a widely used function is defined and called, in a 430-file Go repo | **-57%** | **-81%** | 5.7 → 3 | 3/3 / 3/3 | on |
| Code map (MCP tools) | Same, for a function with a distinctive name grep finds at once | +3% | -10% | 3 → 3 | 3/3 / 3/3 | on: the tool list costs a little when grep is already enough |
| Audio | "What does the speaker say in voice-note.wav?" (one run each) | -34% | -70% | 3 → 2 | 1/1 / 0/1 | on: without tacet Claude Code can't transcribe at all |
| Deny reads of lockfiles and build output | Exact version from a 475 KB lockfile | +12% | +7% | 2.7 → 3 | 0/3 / 3/3 | **removed**: Claude Code already refuses a file that big and greps it |
| Deny reads of lockfiles and build output | Overview of the project's dependencies | 0% | -3% | 2 → 2 | 3/3 / 3/3 | **removed** |

## What we learned

- Turning a PDF into Markdown alone saved only about 14%: the text is the
  cost. The savings come from not loading the whole document. A long
  document opens on an outline plus the sections that match the request,
  so most questions are answered from one read.
- Claude Code reads a PDF at about 1.5k tokens a page. The same text as
  Markdown is about 1.1k. Converted text read in full is still cheaper.
- Claude Code refuses Word, PowerPoint, Excel and audio files before any
  hook runs, so tacet converts a file as soon as a prompt names it and
  lists the Office files a repo tracks when a session starts.
- The AGENTS.md bridge makes Claude follow rules it was skipping but adds
  600 to 3,500 tokens to every session on real projects, so it is a
  suggestion, never applied on its own.
- A first comparison looked good because the run without tacet skipped
  pages. Questions now ask for facts spread over the whole document.

## Repeat it

```sh
make build
bin/tacet plugin write /tmp/tacet-plugin
testbed/bench/ab.sh <project> /tmp/tacet-plugin/plugins/tacet "<prompt>" "<answer regex>" 3 | testbed/bench/summary.sh
```

Settings-only features run with `ON_ARGS="--settings <file>"` instead of the plugin.
