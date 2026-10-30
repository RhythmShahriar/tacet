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

func tacet(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	var out, errw bytes.Buffer
	c := run(append([]string{"-C", dir}, args...), &out, &errw)
	return out.String() + errw.String(), c
}

func TestSubagentsFeature(t *testing.T) {
	dir := t.TempDir()
	agent := audit.ExploreAgentPath(dir)
	if config.Load(dir).On("subagents") {
		t.Fatal("subagents is on by default")
	}
	if out, c := tacet(t, dir, "on", "subagents"); c != exitOK {
		t.Fatalf("on: exit %d: %s", c, out)
	}
	if b, err := os.ReadFile(agent); err != nil || string(b) != audit.ExploreAgent {
		t.Fatalf("agent file %q, %v", b, err)
	}
	if !config.Load(dir).On("subagents") {
		t.Fatal("config not saved")
	}
	if out, c := tacet(t, dir, "on", "subagents"); c != exitOK {
		t.Fatalf("on twice: exit %d: %s", c, out)
	}
	if out, c := tacet(t, dir, "off", "subagents"); c != exitOK {
		t.Fatalf("off: exit %d: %s", c, out)
	}
	if _, err := os.Stat(agent); !os.IsNotExist(err) {
		t.Fatal("off left the agent file")
	}
	// undo reverts the off, then the on.
	if out, c := tacet(t, dir, "undo"); c != exitOK {
		t.Fatalf("undo: exit %d: %s", c, out)
	}
	if _, err := os.Stat(agent); err != nil {
		t.Fatal("undo of off did not restore the agent file")
	}
	if out, c := tacet(t, dir, "undo"); c != exitOK {
		t.Fatalf("undo: exit %d: %s", c, out)
	}
	if _, err := os.Stat(agent); !os.IsNotExist(err) {
		t.Fatal("undo of on left the agent file")
	}
}

func TestSubagentsFeatureKeepsTheirExplore(t *testing.T) {
	dir := t.TempDir()
	mine := filepath.Join(dir, ".claude", "agents", "search.md")
	if err := os.MkdirAll(filepath.Dir(mine), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("---\nname: Explore\nmodel: sonnet\n---\nmine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, c := tacet(t, dir, "on", "subagents"); c == exitOK {
		t.Fatal("on should refuse next to the person's Explore agent")
	}
	if config.Load(dir).On("subagents") {
		t.Fatal("feature turned on anyway")
	}
	if _, err := os.Stat(audit.ExploreAgentPath(dir)); !os.IsNotExist(err) {
		t.Fatal("wrote a second Explore agent")
	}
	if out, c := tacet(t, dir, "off", "subagents"); c != exitOK {
		t.Fatalf("off: exit %d: %s", c, out)
	}
	if _, err := os.Stat(mine); err != nil {
		t.Fatal("off removed the person's agent")
	}
}
