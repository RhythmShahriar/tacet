// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package fix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/core"
)

func TestApplyComposeAndUndo(t *testing.T) {
	root := t.TempDir()
	settings := filepath.Join(root, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(settings, []byte(`{"model":"opus","permissions":{"deny":["Read(./.env)"]}}`), 0o644)
	claudeMD := filepath.Join(root, "CLAUDE.md")
	os.WriteFile(claudeMD, []byte("x\n"), 0o644)
	newSkill := filepath.Join(root, ".claude", "skills", "release", "SKILL.md")

	fixes := []*core.Fix{
		{ID: "a", Edits: []core.Edit{AddDeny(settings, "Read(./dist/**)")}},
		{ID: "b", Edits: []core.Edit{AddDeny(settings, "Read(./dist/**)", "Read(./**/*.lock)")}},
		{ID: "c", Edits: []core.Edit{{Path: claudeMD, Old: []byte("x\n"), New: []byte("@AGENTS.md\n\nx\n")}}},
		{ID: "d", Edits: []core.Edit{{Path: newSkill, New: []byte("---\nname: release\n---\n")}}},
	}
	if _, err := Apply(root, fixes); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(settings)
	got := string(b)
	for _, want := range []string{`"model": "opus"`, `Read(./.env)`, `Read(./dist/**)`, `Read(./**/*.lock)`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	if strings.Count(got, "Read(./dist/**)") != 1 {
		t.Fatalf("deny duplicated: %s", got)
	}
	if _, err := os.Stat(newSkill); err != nil {
		t.Fatal(err)
	}

	if _, err := Undo(root); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(settings)
	if string(b) != `{"model":"opus","permissions":{"deny":["Read(./.env)"]}}` {
		t.Fatalf("settings not restored: %s", b)
	}
	b, _ = os.ReadFile(claudeMD)
	if string(b) != "x\n" {
		t.Fatalf("CLAUDE.md not restored: %q", b)
	}
	if _, err := os.Stat(newSkill); !os.IsNotExist(err) {
		t.Fatal("created file should be removed on undo")
	}
	if _, err := Undo(root); err == nil {
		t.Fatal("second undo should have nothing to do")
	}
}

func TestApplyRefusesChangedFile(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "CLAUDE.md")
	os.WriteFile(p, []byte("edited by hand\n"), 0o644)
	_, err := Apply(root, []*core.Fix{{ID: "x", Edits: []core.Edit{{Path: p, Old: []byte("x\n"), New: []byte("y\n")}}}})
	if err == nil {
		t.Fatal("want ErrChanged")
	}
}
