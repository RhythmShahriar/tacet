# tacet update

Updates tacet to the latest release after a yes. It never updates on its own.

```sh
tacet update
tacet update --check
```

The new binary is used only after its release signature and checksum verify. The plugin is then rewritten for the new version.

Once a day at most, tacet asks GitHub whether a newer release exists and mentions it; it never downloads without you. Set `update: notify` or `update: off` in `~/.config/tacet/config.yaml`, or `TACET_NO_UPDATE_NOTIFIER=1`, to quiet or stop that check. It never runs in CI.

Installed with Homebrew, Scoop, npm or Go? `tacet update` prints the command to update with instead of replacing the binary.

## Flags

| Flag | Does |
|---|---|
| `--check` | only says whether a newer release exists |
| `--version <version>` | installs that version |
| `--rollback` | restores the binary from before the last update |
| `-y`, `--yes` | updates without asking |
