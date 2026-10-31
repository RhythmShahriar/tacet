# tacet uninstall

Removes tacet from this project and restores your files.

```sh
tacet uninstall
```

It reverts every change tacet made in the project, removes the Claude Code plugin and the `/tacet` skill and deletes `.tacet/`.

Then remove the binary the way you installed it:

| Installed with | Remove with |
|---|---|
| Install script | delete `tacet` from the folder it used (`~/.local/bin`, Windows: `%LOCALAPPDATA%\Programs\tacet`) |
| Homebrew | `brew uninstall --cask tacet` |
| Scoop | `scoop uninstall tacet` |
| npm | `npm uninstall -g @rhythmsh/tacet`, then delete the cached binary in `~/.cache/tacet/npm` (`~/Library/Caches/tacet/npm` on macOS, `%LOCALAPPDATA%\tacet\npm` on Windows) |
| Go | delete `tacet` from `$(go env GOPATH)/bin` |
