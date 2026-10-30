// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package plugin

import "path/filepath"

// Output styles the plugin ships. Claude Code lists a plugin style as
// <plugin>:<name>, which is the value outputStyle takes. Every word of a
// style is sent with every request, so they stay short. Both keep Claude
// Code's coding instructions: a custom style drops them otherwise.
const (
	TerseStyle    = Name + ":terse"
	LessCodeStyle = Name + ":lesscode"
)

const terseRules = `Lead with the result. No preamble, step narration or closing recap.
Report findings and changes, not process.
Never echo file contents or diffs back; cite path and line.
Show code only when the person must read it.
Keep every fact, number, path, command and warning.
Explain in full when asked and for errors and risks.
`

// styles maps each style file under output-styles/ to its content.
var styles = map[string]string{
	"terse.md": `---
name: terse
description: tacet terse, results first with no preamble or recap
keep-coding-instructions: true
---

` + terseRules,
	"lesscode.md": `---
name: lesscode
description: tacet terse plus less code, the smallest change that works
keep-coding-instructions: true
---

` + terseRules + `Make the smallest change that works: no speculative abstractions, options or helpers, no edits outside the task.
`,
}

// StylePath is where the written plugin keeps a style, for checks that
// the plugin carries it. name is the part after the plugin prefix.
func StylePath(dir, name string) string {
	return filepath.Join(dir, "plugins", Name, "output-styles", name+".md")
}
