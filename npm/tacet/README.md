# @rhythmsh/tacet

**Why is Claude Code reading all that?**

tacet makes Claude Code use fewer tokens on any project. This package installs the tacet binary for your system from npm, with no install scripts and no downloads at install time.

```sh
npm i -g @rhythmsh/tacet
cd your-project
tacet init
```

Install it globally: `tacet init` points Claude Code's hooks at the installed binary, so a temporary `npx` copy would stop working once npm clears its cache.

Update with `npm i -g @rhythmsh/tacet@latest`. Docs, the other install methods and the source are at [github.com/RhythmShahriar/tacet](https://github.com/RhythmShahriar/tacet).

Made by Rhythm Shahriar. MIT license.
