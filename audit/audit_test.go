// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package audit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RhythmShahriar/tacet/measure"
)

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// env gives each test its own Claude config dir and project.
func env(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	write(t, root, files)
	return root
}

func rulesFor(t *testing.T, root string, u *measure.Usage) map[string]int {
	t.Helper()
	st, _, err := measure.MeasureStartup(root, measure.Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range Run(Input{Root: root, Startup: st, Usage: u}) {
		got[f.Rule]++
	}
	return got
}

func usage(sessions int, mcp map[string]int, skills map[string]int) *measure.Usage {
	return &measure.Usage{Sessions: sessions, MCPCalls: mcp, SkillCalls: skills, DocReads: map[string]int{}}
}

func TestToolSearchOff(t *testing.T) {
	pos := env(t, map[string]string{
		".mcp.json":             `{"mcpServers":{"gh":{"command":"gh-mcp"}}}`,
		".claude/settings.json": `{"env":{"ENABLE_TOOL_SEARCH":"false"}}`,
	})
	if rulesFor(t, pos, nil)["mcp/tool-search-off"] != 1 {
		t.Fatal("not flagged")
	}
	neg := env(t, map[string]string{".mcp.json": `{"mcpServers":{"gh":{"command":"gh-mcp"}}}`})
	if rulesFor(t, neg, nil)["mcp/tool-search-off"] != 0 {
		t.Fatal("tool search is on by default")
	}
}

func TestUnusedMCP(t *testing.T) {
	root := env(t, map[string]string{".mcp.json": `{"mcpServers":{"github":{"command":"x"},"db":{"command":"y"}}}`})
	got := rulesFor(t, root, usage(6, map[string]int{"github": 3}, nil))
	if got["mcp/unused"] != 1 {
		t.Fatalf("want only db flagged, got %v", got)
	}
	if rulesFor(t, root, usage(2, nil, nil))["mcp/unused"] != 0 {
		t.Fatal("too few sessions to judge")
	}
}

func TestUnusedSkillAndFix(t *testing.T) {
	root := env(t, map[string]string{
		".claude/skills/deploy/SKILL.md":  "---\nname: deploy\ndescription: Deploy the app\n---\nsteps\n",
		".claude/skills/release/SKILL.md": "---\nname: release\ndescription: Cut a release\n---\nsteps\n",
	})
	st, _, _ := measure.MeasureStartup(root, measure.Options{})
	var hit int
	for _, f := range Run(Input{Root: root, Startup: st, Usage: usage(5, nil, map[string]int{"deploy": 1})}) {
		if f.Rule != "skills/unused" {
			continue
		}
		hit++
		next, err := f.Fix.Edits[0].Change([]byte("---\nname: release\ndescription: Cut a release\n---\nsteps\n"))
		if err != nil || string(next) != "---\nname: release\ndescription: Cut a release\ndisable-model-invocation: true\n---\nsteps\n" {
			t.Fatalf("fix gave %q", next)
		}
	}
	if hit != 1 {
		t.Fatalf("want release only, got %d", hit)
	}
}

func TestAgentsHook(t *testing.T) {
	pos := env(t, map[string]string{".claude/settings.json": `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"cat AGENTS.md"}]}]}}`})
	if rulesFor(t, pos, nil)["agents/hook"] != 1 {
		t.Fatal("not flagged")
	}
	neg := env(t, map[string]string{".claude/settings.json": `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"tacet hook session-start"}]}]}}`})
	if rulesFor(t, neg, nil)["agents/hook"] != 0 {
		t.Fatal("false positive")
	}
}

func TestAttribution(t *testing.T) {
	if rulesFor(t, env(t, nil), nil)["git/attribution"] != 1 {
		t.Fatal("not offered")
	}
	neg := env(t, map[string]string{".claude/settings.json": `{"attribution":{"commit":"","pr":""}}`})
	if rulesFor(t, neg, nil)["git/attribution"] != 0 {
		t.Fatal("already set")
	}
}

func TestCacheMisses(t *testing.T) {
	u := usage(1, nil, nil)
	u.Misses = []measure.Miss{{Cause: "idle", Tokens: 50_000}, {Cause: "model-switch", Tokens: 80_000}}
	got := rulesFor(t, env(t, nil), u)
	if got["cache/idle"] != 1 || got["cache/model-switch"] != 1 {
		t.Fatalf("got %v", got)
	}
	if rulesFor(t, env(t, nil), usage(1, nil, nil))["cache/idle"] != 0 {
		t.Fatal("no misses, no finding")
	}
}

func TestSubagentShare(t *testing.T) {
	u := usage(2, nil, nil)
	u.Input, u.SubTokens, u.Subagents = 100, 900, 3
	if rulesFor(t, env(t, nil), u)["usage/subagents"] != 1 {
		t.Fatal("90% subagent share not reported")
	}
	u.SubTokens = 10
	if rulesFor(t, env(t, nil), u)["usage/subagents"] != 0 {
		t.Fatal("small share reported")
	}
}
