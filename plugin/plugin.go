// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package plugin writes tacet's Claude Code plugin and installs it. The
// plugin is generated from the running binary, so its hooks always call
// the tacet that installed them and match its version.
package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/RhythmShahriar/tacet/claude"
)

// Name is the plugin and marketplace name.
const Name = "tacet"

// Dir is the local marketplace tacet installs from.
func Dir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "tacet", "marketplace")
	}
	return filepath.Join(claude.Home(), "tacet-marketplace")
}

// hookEvents maps Claude Code events to tacet hook names and matchers.
var hookEvents = []struct{ event, matcher, name string }{
	{"SessionStart", "", "session-start"},
	{"UserPromptSubmit", "", "prompt"},
	{"PreToolUse", "Read", "pre-read"},
	{"PreToolUse", "Bash", "pre-bash"},
	{"PreToolUse", "Grep|Glob", "pre-search"},
	{"PostToolUse", "Bash|WebFetch", "post-output"},
	{"PreCompact", "", "pre-compact"},
	{"SessionEnd", "", "session-end"},
}

// Write generates the marketplace and plugin under dir for the tacet
// binary at bin.
func Write(dir, bin, version string) error {
	pdir := filepath.Join(dir, "plugins", Name)
	files := map[string]any{
		filepath.Join(dir, ".claude-plugin", "marketplace.json"): map[string]any{
			"name":     Name,
			"owner":    map[string]any{"name": "Rhythm Shahriar", "email": "me@rhy.io"},
			"metadata": map[string]any{"description": "tacet, written locally by the tacet binary"},
			"plugins": []any{map[string]any{
				"name": Name, "source": "./plugins/" + Name,
				"description": "Keeps Claude Code lean: lighter context, documents as Markdown, session handoff and secret guard.",
				"version":     version,
			}},
		},
		filepath.Join(pdir, ".claude-plugin", "plugin.json"): map[string]any{
			"name": Name, "version": version,
			"description": "Keeps Claude Code lean on projects where tacet init ran.",
			"author":      map[string]any{"name": "Rhythm Shahriar", "email": "me@rhy.io"},
			"homepage":    "https://github.com/RhythmShahriar/tacet",
			"repository":  "https://github.com/RhythmShahriar/tacet",
			"license":     "MIT",
		},
		filepath.Join(pdir, "hooks", "hooks.json"): hooks(bin),
		filepath.Join(pdir, ".mcp.json"): map[string]any{"mcpServers": map[string]any{
			Name: map[string]any{"command": bin, "args": []string{"mcp"}},
		}},
	}
	for p, v := range files {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		if err := writeFile(p, append(b, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func hooks(bin string) map[string]any {
	ev := map[string][]any{}
	for _, h := range hookEvents {
		entry := map[string]any{"hooks": []any{map[string]any{
			"type": "command", "command": quote(bin) + " hook " + h.name, "timeout": 30,
		}}}
		if h.matcher != "" {
			entry["matcher"] = h.matcher
		}
		ev[h.event] = append(ev[h.event], entry)
	}
	return map[string]any{"hooks": ev}
}

func quote(p string) string {
	if strings.ContainsAny(p, " '\"") {
		return `"` + strings.ReplaceAll(p, `"`, `\"`) + `"`
	}
	return p
}

// SkillPath is the user skill behind /tacet. It is hidden from the
// model, so it costs no context; only the person can call it.
func SkillPath() string { return filepath.Join(claude.Home(), "skills", "tacet", "SKILL.md") }

// Skill is the /tacet skill body.
func Skill(bin string) string {
	b := quote(bin)
	return `---
name: tacet
description: Show what tacet saved on this project and the suggestions waiting for a yes.
disable-model-invocation: true
allowed-tools: Bash(` + b + ` status:*), Bash(` + b + ` apply:*), Bash(` + b + ` dismiss:*), Bash(` + b + ` on:*), Bash(` + b + ` off:*), Bash(` + b + ` undo:*), Bash(` + b + ` skills new:*)
---
<!-- tacet · github.com/RhythmShahriar/tacet -->

Run ` + "`" + b + ` status --json` + "`" + ` and show the person, briefly:

1. Tokens saved since tacet init: the startup context, then billed tokens per request and cache hits from the main-session fields (per_request_main, cache_hit_rate_main). Show those billed numbers only when enough_data is true; otherwise say there is not enough data yet (requests_main of min_requests requests). List subagent usage on its own line, never as part of the saving. Only the numbers it reports.
2. Each suggestion: its title, what it saves and why, one per line.

When it reports ` + "`update`" + `, add one line first: tacet <latest> is available: run <command>.

Then ask which suggestions to apply. For each yes run ` + "`" + b + ` apply <id>` + "`" + `; for each no run ` + "`" + b + ` dismiss <id>` + "`" + `.
Never apply a suggestion the person did not choose. If they ask to change a feature, run ` + "`" + b + ` on <feature>` + "`" + ` or ` + "`" + b + ` off <feature>` + "`" + `.

If the person says "make this a skill" or types /tacet skill <what>, run ` + "`" + b + ` skills new --from-session --json` + "`" + `, adding <what> in quotes when given.
Show the draft's path, description and steps. Only on a yes run the same command with --yes in place of --json; it never overwrites a skill.
`
}

// ErrNoClaude means the claude CLI isn't on PATH.
var ErrNoClaude = errors.New("the claude command isn't on PATH")

// Install writes the plugin and the /tacet skill, then registers the
// marketplace and installs the plugin through the claude CLI.
func Install(bin, version string) error {
	dir := Dir()
	if err := Write(dir, bin, version); err != nil {
		return err
	}
	if err := writeFile(SkillPath(), []byte(Skill(bin))); err != nil {
		return err
	}
	cl, err := exec.LookPath("claude")
	if err != nil {
		return ErrNoClaude
	}
	// Adding a marketplace that is already known fails; update instead.
	if out, err := exec.Command(cl, "plugin", "marketplace", "add", dir).CombinedOutput(); err != nil { // #nosec G204 -- fixed args, our own dir
		if !strings.Contains(strings.ToLower(string(out)), "already") {
			_, _ = exec.Command(cl, "plugin", "marketplace", "update", Name).CombinedOutput() // #nosec G204
		}
	}
	out, err := exec.Command(cl, "plugin", "install", Name+"@"+Name).CombinedOutput() // #nosec G204 -- fixed args
	if err != nil && !strings.Contains(strings.ToLower(string(out)), "already") {
		return fmt.Errorf("claude plugin install: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Uninstall removes the plugin, its marketplace and the /tacet skill.
func Uninstall() error {
	if cl, err := exec.LookPath("claude"); err == nil {
		_, _ = exec.Command(cl, "plugin", "uninstall", Name+"@"+Name).CombinedOutput()    // #nosec G204
		_, _ = exec.Command(cl, "plugin", "marketplace", "remove", Name).CombinedOutput() // #nosec G204
	}
	_ = os.RemoveAll(filepath.Dir(SkillPath()))
	return os.RemoveAll(Dir())
}

func writeFile(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}
