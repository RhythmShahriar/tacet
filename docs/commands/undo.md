# tacet undo

Reverts the last change tacet made to your files.

```sh
tacet undo
```

Every change tacet makes (an applied suggestion, a docs fix, a feature switch) is recorded with the file contents before it. `tacet undo` restores the last one. Run it again to go further back.

## See also

- [`tacet uninstall`](uninstall.md) reverts all of them at once
