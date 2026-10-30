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

## Against other tools

Nine tasks, every tool at its default settings, three runs each, on Claude Haiku through Claude Code 2.1.92 (2026-10-30), with Python 3.10 and pytest 8.3.3 available to every tool the same way. Each tool runs in its own Claude Code config folder so none of them sees another. Before every measured run the tool gets an unrecorded warm-up in the same project, so every run starts with a warm prompt cache and the numbers measure the tool, not who happened to run after a pause. An answer counts only when its check passes: every expected fact for questions, the tests passing for fixes.

| Tool | Version | Cost per run | Tokens per run | Correct |
|---|---|---|---|---|
| **tacet** | 1.2.0 | **-38%** | **-41%** | 27/27 |
| tacet with feedback, testgate and bigoutput on | 1.2.0 | -41% | -40% | 27/27 |
| rtk | 1.1.0 | -9% | -8% | 27/27 |
| serena | 2.1.0 | -4% | -2% | 27/27 |
| claude-mem | 15.6.0 | 0% | +2% | 26/26 |
| caveman | 4.0.1 | +4% | -1% | 26/27 |
| context-mode | 1.0.169 | +4% | +14% | 27/27 |
| graphify | 0.9.97 | +13% | +9% | 27/27 |

Against Claude Code with no tool: $0.0531 and 212k tokens per run, 27/27 correct. claude-mem has 26 runs: one didn't record a result.

Cost per run by task (mean of three runs, US dollars):

| Task | No tool | tacet | rtk | serena | caveman | claude-mem | context-mode | graphify |
|---|---|---|---|---|---|---|---|---|
| Question about one part of a 43-page PDF | 0.094 | **0.022** | 0.075 | 0.093 | 0.079 | 0.103 | 0.055 | 0.079 |
| Facts from the start, middle and end of the PDF | 0.075 | **0.071** | 0.093 | 0.083 | 0.073 | 0.086 | 0.098 | 0.083 |
| Question about a Word document | 0.056 | **0.021** | 0.061 | 0.061 | 0.092 | 0.062 | 0.067 | 0.074 |
| Where a Go function is defined and called | 0.018 | **0.011** | 0.016 | 0.016 | 0.020 | 0.015 | 0.015 | 0.025 |
| Same in a Python repo | 0.019 | **0.009** | 0.016 | 0.016 | 0.021 | 0.018 | 0.022 | 0.034 |
| Same in a Rust repo | 0.025 | **0.010** | 0.018 | 0.024 | 0.024 | 0.018 | 0.035 | 0.046 |
| Fix a failing Python test | 0.046 | **0.038** | 0.050 | 0.040 | 0.053 | 0.049 | 0.071 | 0.054 |
| Fix a failing Rust test | 0.037 | 0.036 | 0.037 | **0.035** | 0.046 | 0.045 | 0.046 | 0.040 |
| Continue a half-done change | 0.109 | 0.079 | **0.071** | 0.093 | 0.089 | 0.083 | 0.089 | 0.105 |

Where tacet isn't first: on continuing a half-done change rtk is 10% cheaper; on fixing the Rust test serena is 2% cheaper, within the noise of three runs.

The two fix tasks were run again after the bench gave every mode a real Python with pytest. Before, only a `pytest` command worked; an agent that typed `python -m pytest` lost six to eight turns installing it, except under rtk, whose command rewrite happened to reach the working `pytest`. That made rtk look 41% cheaper on the Python fix. With the same Python for everyone, rtk is 10% more expensive than no tool there and tacet is cheapest.

The first full run, before the warm-up was added, ranked the tools the same way at the top (tacet the only tool cheaper than no tool). It also wrongly penalized whichever tool ran after a pause. It caught a tacet bug too: the PDF converter split a code name across a fake heading, so tacet got the whole-document task wrong. Both are fixed; the raw results of both runs are in `testbed/bench/results/` (in `2026-10-29.jsonl` lines 1 to 243 are the first run and the rest the second; `2026-10-30.jsonl` holds the rerun of the two fix tasks).

Features that ship off and why, from this run (tacet with all features on compared with tacet):
- **feedback** and **testgate**: no gain on the two fix tasks (Python 8% more, Rust 17% more than tacet without them), so they stay off.
- **bigoutput**: a small gain within the noise of three runs, so it stays off until a larger run shows it.

Repeat it: `testbed/bench/all.sh 3`, then `testbed/bench/table.sh`. Setup, pinned versions and fairness rules: [testbed/bench/README.md](../testbed/bench/README.md).

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
