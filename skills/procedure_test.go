// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package skills

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/docs/lint"
	"github.com/RhythmShahriar/tacet/fix"
)

const beforeMD = `# Project

Use Go 1.24.

## Release

1. Update CHANGELOG.md
2. Run make check
   and wait for it to pass
3. Tag the commit
4. Push the tag
5. Check the release page

## Style

- gofmt everything
`

const afterMD = `# Project

Use Go 1.24.

## Release

Release: follow the ` + "`release`" + ` skill.

## Style

- gofmt everything
`

const wantSkill = `---
name: release
description: "Use for release: the 5 steps this project follows."
---
<!-- tacet · moved from CLAUDE.md -->

# Release

1. Update CHANGELOG.md
2. Run make check
   and wait for it to pass
3. Tag the commit
4. Push the tag
5. Check the release page
`

func load(t *testing.T, files map[string]string) (string, *lint.Set, []core.Finding) {
	t.Helper()
	root := t.TempDir()
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	set, err := lint.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	fs := lint.Check(set)
	AttachProcedureFixes(set, fs, Used(root))
	return root, set, fs
}

func procedureFix(fs []core.Finding) *core.Fix {
	for _, f := range fs {
		if f.Rule == "move/procedure" {
			return f.Fix
		}
	}
	return nil
}

func TestMoveProcedureGolden(t *testing.T) {
	root, _, fs := load(t, map[string]string{"CLAUDE.md": beforeMD})
	fx := procedureFix(fs)
	if fx == nil {
		t.Fatal("no fix on move/procedure")
	}
	if fx.ID != "skill-release" || fx.Safe || fx.Tokens <= 0 {
		t.Fatalf("fix %+v", fx)
	}
	if _, err := fix.Apply(root, []*core.Fix{fx}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if string(got) != afterMD {
		t.Errorf("CLAUDE.md after:\n%s\nwant:\n%s", got, afterMD)
	}
	sk, _ := os.ReadFile(filepath.Join(root, ".claude", "skills", "release", "SKILL.md"))
	if string(sk) != wantSkill {
		t.Errorf("SKILL.md:\n%s\nwant:\n%s", sk, wantSkill)
	}
	// Undo puts both files back.
	if _, err := fix.Undo(root); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md")); string(b) != beforeMD {
		t.Errorf("undo left:\n%s", b)
	}
}

func TestMoveProcedureNegative(t *testing.T) {
	// Too short to be a procedure: no finding, no fix.
	_, _, fs := load(t, map[string]string{"CLAUDE.md": "## Release\n1. tag\n2. push\n"})
	if procedureFix(fs) != nil {
		t.Fatal("two steps got a fix")
	}
	// A scoped rule isn't loaded every session.
	_, _, fs = load(t, map[string]string{"CLAUDE.md": "x\n", ".claude/rules/r.md": "---\npaths: [\"src/**\"]\n---\n" + beforeMD})
	if procedureFix(fs) != nil {
		t.Fatal("path-scoped rule got a fix")
	}
}

func TestMoveProcedureNameTaken(t *testing.T) {
	root, _, fs := load(t, map[string]string{"CLAUDE.md": beforeMD, ".claude/skills/release/SKILL.md": "---\nname: release\n---\nmine\n"})
	fx := procedureFix(fs)
	if fx == nil || fx.ID != "skill-release-2" {
		t.Fatalf("fix %+v", fx)
	}
	if _, err := fix.Apply(root, []*core.Fix{fx}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".claude", "skills", "release", "SKILL.md")); string(b) != "---\nname: release\n---\nmine\n" {
		t.Fatalf("existing skill overwritten: %q", b)
	}
}

func TestMoveProcedureChanged(t *testing.T) {
	root, _, fs := load(t, map[string]string{"CLAUDE.md": beforeMD})
	fx := procedureFix(fs)
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# rewritten\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fix.Apply(root, []*core.Fix{fx}); !errors.Is(err, ErrMoved) {
		t.Fatalf("want ErrMoved, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "release")); err == nil {
		t.Fatal("skill written although the move failed")
	}
}

func TestProceduresUnnamed(t *testing.T) {
	ps := Procedures("CLAUDE.md", "1. build the image\n2. b\n3. c\n4. d\n5. e\n")
	if len(ps) != 1 || ps[0].Line != 1 || ps[0].Steps != 5 {
		t.Fatalf("%+v", ps)
	}
	fx := MoveFix("/r", ps[0], map[string]bool{})
	if fx.ID != "skill-build-image" {
		t.Fatalf("id %q", fx.ID)
	}
}
