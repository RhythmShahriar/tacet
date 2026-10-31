# Configuration

Most people never edit these files: `tacet init`, `tacet on`, `tacet off` and `tacet subagents` write them.

## Per project: `.tacet/config.yaml`

```yaml
features:
  convert: true
  audio: true
  guard: true
  handoff: true
  map: true
  skills: true
  subagents: true
  budget: false
  bigoutput: false
  feedback: false
  testgate: false
subagent_model: haiku   # haiku or sonnet, for the subagents feature
audit_every: 10         # sessions between quiet re-audits at session end
budget:                 # thresholds for the budget feature
  session_tokens: 2000000
  warn_at: [0.8, 1.0]
  session_cost_usd: 5   # optional
```

A feature left out takes its default. Unknown names are ignored. Edit the file by hand or use [`tacet on`](commands/on.md) and [`tacet off`](commands/off.md), which [`tacet undo`](commands/undo.md) can revert.

## Global: `~/.config/tacet/config.yaml`

```yaml
update: prompt   # prompt (default), notify or off
```

| Value | Daily release check |
|---|---|
| `prompt` | asks [Y/n] before an interactive command when a new release is out |
| `notify` | prints one line, never asks |
| `off` | no check |

## Environment

| Variable | Does |
|---|---|
| `TACET_NO_UPDATE_NOTIFIER=1` | no release check |
| `NO_COLOR=1` | plain output |
| `TACET_INSTALL_DIR` | where the install scripts put the binary |
| `TACET_VERSION` | which version the install scripts install |
