# audio

**Default: on.** Audio files become transcripts Claude Code can read.

```sh
tacet off audio
```

## What it does

Claude Code can't read audio. When your prompt names an audio file, tacet transcribes it on your machine and hands Claude Code the Markdown transcript.

The speech engine and model are downloaded once after you agree. Both are checked against pinned checksums. Start that with `tacet transcribe --setup` or wait for the first prompt that needs it. Nothing is sent anywhere.

To transcribe by hand, see [`tacet transcribe`](../commands/transcribe.md).

## Measured

What a voice note says: without tacet it can't be done; with tacet the answer was right. See the [benchmark](../benchmark.md).
