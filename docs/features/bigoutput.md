# bigoutput

**Default: off.** Experimental: long command output is kept on disk and shown short.

```sh
tacet on bigoutput
```

## What it does

When a Bash command or WebFetch returns more than about 8,000 tokens, tacet saves the full output in `.tacet/out/` and shows Claude Code the first and last 40 lines, up to 40 lines that look like errors from the middle and the path to the full file. Claude Code can read the file when it needs more. Saved output is deleted after a week.

It ships off because the benchmark showed only a small gain within the noise of three runs. It stays off until a larger run shows more.
