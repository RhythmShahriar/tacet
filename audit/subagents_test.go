// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/fix"
	"github.com/RhythmShahriar/tacet/measure"
)

// heavy is usage where subagents spent most tokens and Explore ran on
// the given models.
func heavy(models map[string]int) *measure.Usage {
	u := usage(6, nil, nil)
	u.Input, u.SubTokens = 1_000_000, 900_000
	for _, n := range models {
		u.ExploreTokens += n
	}
	u.ExploreModels = models
	return u
}

func exploreFinding(t *testing.T, root string, u *measure.Usage) *core.Finding {
	t.Helper()
	for _, f := range exploreModel(Input{Root: root, Usage: u}) {
		return &f
	}
	return nil
}

func TestExploreModelFlagged(t *testing.T) {
	root := env(t, nil)
	f := exploreFinding(t, root, heavy(map[string]int{"claude-opus-5-5": 300_000, "claude-haiku-5-5": 10_000}))
	if f == nil || f.Rule != "subagents/model" || f.Fix == nil || f.Fix.Safe {
		t.Fatalf("got %+v", f)
	}
	if !strings.Contains(f.Message, "claude-opus-5-5") || !strings.Contains(f.Hint, "may miss on hard searches") {
		t.Fatalf("message %q hint %q", f.Message, f.Hint)
	}
	if rulesFor(t, root, heavy(map[string]int{"claude-opus-5-5": 1}))["subagents/model"] != 1 {
		t.Fatal("not in the audit")
	}
}

func TestExploreModelNotFlagged(t *testing.T) {
	root := env(t, nil)
	cases := map[string]*measure.Usage{
		"no usage":     nil,
		"on haiku":     heavy(map[string]int{"claude-haiku-5-5": 300_000}),
		"no explore":   heavy(nil),
		"mostly haiku": heavy(map[string]int{"claude-haiku-5-5": 300_000, "claude-opus-5-5": 1_000}),
	}
	light := heavy(map[string]int{"claude-opus-5-5": 300_000})
	light.SubTokens = 100_000
	cases["light subagent use"] = light
	for name, u := range cases {
		if f := exploreFinding(t, root, u); f != nil {
			t.Errorf("%s: flagged %+v", name, f)
		}
	}
	own := env(t, map[string]string{".claude/agents/search.md": "---\nname: \"Explore\"\nmodel: sonnet\n---\nmine\n"})
	if f := exploreFinding(t, own, heavy(map[string]int{"claude-opus-5-5": 300_000})); f != nil {
		t.Fatal("the project already defines Explore")
	}
}

func TestExploreFixAppliesAndUndoes(t *testing.T) {
	root := env(t, nil)
	if _, err := fix.Apply(root, []*core.Fix{ExploreOnFix(root)}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(ExploreAgentPath(root))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "explore.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("agent file differs from testdata/explore.md:\n%s", got)
	}
	if ProjectExplore(root) != ExploreAgentPath(root) || !IsTacetExplore(got) {
		t.Fatal("tacet's agent not recognized")
	}
	if f := exploreFinding(t, root, heavy(map[string]int{"claude-opus-5-5": 300_000})); f != nil {
		t.Fatal("flagged after the fix")
	}
	if _, err := fix.Undo(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ExploreAgentPath(root)); !os.IsNotExist(err) {
		t.Fatalf("undo left the agent file: %v", err)
	}
}

func TestExploreFixKeepsTheirFile(t *testing.T) {
	mine := "---\nname: explorer\n---\nmine\n"
	root := env(t, map[string]string{".claude/agents/explore.md": mine})
	if _, err := fix.Apply(root, []*core.Fix{ExploreOnFix(root)}); err == nil {
		t.Fatal("replaced the person's agent file")
	}
	if _, err := fix.Apply(root, []*core.Fix{ExploreOffFix(root)}); err == nil {
		t.Fatal("removed the person's agent file")
	}
	if b, _ := os.ReadFile(ExploreAgentPath(root)); string(b) != mine {
		t.Fatalf("file changed: %q", b)
	}
}

func TestExploreOffRemovesOnlyOurs(t *testing.T) {
	root := env(t, map[string]string{".claude/agents/explore.md": ExploreAgent})
	if _, err := fix.Apply(root, []*core.Fix{ExploreOffFix(root)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ExploreAgentPath(root)); !os.IsNotExist(err) {
		t.Fatal("agent file kept")
	}
	if _, err := fix.Undo(root); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(ExploreAgentPath(root)); string(b) != ExploreAgent {
		t.Fatal("undo did not bring the agent file back")
	}
}
