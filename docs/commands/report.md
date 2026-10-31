# tacet report

Writes `.tacet/report.html` and opens it: what tacet saved since init, plus usage and estimated cost by day, model and tool for the last 14 days.

```sh
tacet report
tacet report --no-open
```

## Flags

| Flag | Does |
|---|---|
| `--no-open` | writes the file without opening it |
| `--json` | machine output |

The page is a single file with no network requests.
