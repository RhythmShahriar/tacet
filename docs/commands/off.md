# tacet off

Turns a feature off for this project.

```sh
tacet off convert
```

The switch is saved in `.tacet/config.yaml`. [`tacet undo`](undo.md) reverts it. The hooks read the switch on every call, so it takes effect in the running session.

See [`tacet on`](on.md) for the list of features.

## See also

- [Configuration](../configuration.md)
