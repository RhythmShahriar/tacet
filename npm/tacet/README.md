# @rhythmsh/tacet

**Why is Claude Code reading all that?**

tacet makes Claude Code use fewer tokens on any project. This package is a few kilobytes: the first time you run `tacet`, it downloads the binary for your system from the GitHub release, checks it against the SHA-256 published with this package and keeps it in your user cache. No install scripts, so `--ignore-scripts` is fine.

```sh
npm i -g @rhythmsh/tacet
cd your-project
tacet init
```

`npx @rhythmsh/tacet init` works too: the binary lives in your user cache, not in npx's temporary folder, so the hooks keep working.

Update with `npm i -g @rhythmsh/tacet@latest`. Docs, the other install methods and the source are at [github.com/RhythmShahriar/tacet](https://github.com/RhythmShahriar/tacet).

Made by Rhythm Shahriar. MIT license.
