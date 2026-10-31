# tacet transcribe

Turns audio files into Markdown transcripts, on your machine.

```sh
tacet transcribe --setup        # one-time download of the speech engine and model
tacet transcribe meeting.m4a
```

With the [audio](../features/audio.md) feature on, a prompt that names an audio file gets the transcript on its own; this command is for doing it by hand.

## Flags

| Flag | Does |
|---|---|
| `--setup` | downloads the speech engine and model, checked against pinned checksums |
| `--english` | uses the English-only model, a little more accurate on English speech |
| `--json` | machine output |

Nothing is sent anywhere: the model runs locally.
