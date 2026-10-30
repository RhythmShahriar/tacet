// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/RhythmShahriar/tacet/audit"
	"github.com/RhythmShahriar/tacet/internal/config"
)

func agentModel(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(audit.ExploreAgentPath(root))
	if err != nil {
		return "main"
	}
	return audit.ExploreModelOf(b)
}

func TestSubagentsCommandSwitchesModels(t *testing.T) {
	root := t.TempDir()
	a := &app{out: &bytes.Buffer{}, err: &bytes.Buffer{}, dir: root}
	for _, want := range []string{"haiku", "sonnet", "main", "haiku"} {
		if err := runSubagents(a, []string{want}); err != nil {
			t.Fatalf("%s: %v", want, err)
		}
		if got := agentModel(t, root); got != want {
			t.Fatalf("after %s the agent runs on %s", want, got)
		}
		if on := config.Load(root).On("subagents"); on != (want != "main") {
			t.Fatalf("after %s the feature is on=%v", want, on)
		}
	}
	if err := runSubagents(a, []string{"opus5"}); err == nil {
		t.Fatal("unknown model accepted")
	}
}

func TestInitChoiceDefaultsAndFlags(t *testing.T) {
	root := t.TempDir()
	yes := &app{out: &bytes.Buffer{}, err: &bytes.Buffer{}, dir: root, yes: true}
	if err := chooseSubagents(yes, root, ""); err != nil {
		t.Fatal(err)
	}
	if got := agentModel(t, root); got != "haiku" {
		t.Fatalf("--yes should take the recommended haiku, got %s", got)
	}
	if err := chooseSubagents(yes, root, "sonnet"); err != nil {
		t.Fatal(err)
	}
	if got := agentModel(t, root); got != "sonnet" {
		t.Fatalf("--subagents sonnet gave %s", got)
	}
	quiet := &app{out: &bytes.Buffer{}, err: &bytes.Buffer{}, dir: t.TempDir()}
	if err := chooseSubagents(quiet, quiet.dir, ""); err != nil {
		t.Fatal(err)
	}
	if got := agentModel(t, quiet.dir); got != "main" {
		t.Fatalf("without a terminal or --yes nothing changes, got %s", got)
	}
}

func TestInitChoiceKeepsTheirExplore(t *testing.T) {
	root := t.TempDir()
	own := filepath.Join(root, ".claude", "agents", "finder.md")
	os.MkdirAll(filepath.Dir(own), 0o755)
	body := "---\nname: Explore\nmodel: opus\n---\nmine\n"
	os.WriteFile(own, []byte(body), 0o644)
	a := &app{out: &bytes.Buffer{}, err: &bytes.Buffer{}, dir: root, yes: true}
	if err := chooseSubagents(a, root, "haiku"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(own); string(b) != body {
		t.Fatal("their Explore agent changed")
	}
	if _, err := os.Stat(audit.ExploreAgentPath(root)); err == nil {
		t.Fatal("tacet wrote a second Explore agent")
	}
}
