# convert

**Default: on.** Long PDF, Word, PowerPoint and Excel files open small.

```sh
tacet off convert
```

## What it does

When Claude Code reads a PDF, `.docx`, `.pptx` or `.xlsx`, tacet converts it to Markdown once and caches the result in `.tacet/`. Then:

- **A short document** (under about 6,000 tokens) is read whole, as Markdown.
- **A long one** opens on an outline of its sections plus the sections that match your request, about 4,000 tokens. Claude Code reads more sections when it needs them.
- **Word, PowerPoint and Excel files** are refused by Claude Code before any hook runs. tacet converts them as soon as your prompt names the file, so Claude Code gets the Markdown instead.

The first page read of a long PDF in a session goes to the outline too, even when the model asks for pages.

## Measured

| Scenario | Tokens | Cost |
|---|---|---|
| Question about one part of a 28-page PDF | -56% | -65% |
| Facts from start, middle and end of a 28-page PDF, plus a summary | -69% | -72% |
| Question about one part of a Word file | -63% | -46% |

Same answers with and without tacet. See the [benchmark](../benchmark.md).

## Limits

- A scanned PDF with no text layer passes through to Claude Code as usual.
- Files over 100 MB are left alone.
- An Office file has to be named in your prompt or be in the repo when the session starts.
