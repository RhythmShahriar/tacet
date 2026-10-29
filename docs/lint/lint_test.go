// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// project writes files into a temp dir; keys are slash paths.
func project(t *testing.T, files map[string]string) string {
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
	return root
}

func rules(t *testing.T, files map[string]string) map[string]int {
	t.Helper()
	s, err := Load(project(t, files), "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range Check(s) {
		got[f.Rule]++
	}
	return got
}

func TestRules(t *testing.T) {
	long := strings.Repeat("- keep this line\n", MaxLines+5)
	steps := "## Release\n1. tag\n2. push\n3. wait for ci\n4. verify\n5. announce\n"
	cases := []struct {
		rule string
		pos  map[string]string
		neg  map[string]string
	}{
		{"size/lines", map[string]string{"CLAUDE.md": long}, map[string]string{"CLAUDE.md": "- short\n"}},
		{"size/vendor", map[string]string{"AGENTS.md": strings.Repeat("x", CodexLimit+10)}, map[string]string{"AGENTS.md": "fine\n"}},
		{"import/missing", map[string]string{"CLAUDE.md": "See @docs/gone.md\n"}, map[string]string{"CLAUDE.md": "See @docs/here.md\n", "docs/here.md": "x\n"}},
		{"import/depth", map[string]string{
			"CLAUDE.md": "@a/1.md\n", "a/1.md": "@2.md\n", "a/2.md": "@3.md\n", "a/3.md": "@4.md\n", "a/4.md": "@5.md\n", "a/5.md": "end\n",
		}, map[string]string{"CLAUDE.md": "@a/1.md\n", "a/1.md": "@2.md\n", "a/2.md": "end\n"}},
		{"agents/shadowed", map[string]string{"CLAUDE.md": "x\n", "AGENTS.md": "rules\n"}, map[string]string{"CLAUDE.md": "@AGENTS.md\n", "AGENTS.md": "rules\n"}},
		{"stale/path", map[string]string{"CLAUDE.md": "Config lives in `config/app.yaml`.\n"}, map[string]string{"CLAUDE.md": "Config lives in `config/app.yaml`.\n", "config/app.yaml": "a: 1\n"}},
		{"stale/command", map[string]string{"CLAUDE.md": "Run `make lint`.\n", "Makefile": "test:\n\tgo test\n"}, map[string]string{"CLAUDE.md": "Run `make test`.\n", "Makefile": "test:\n\tgo test\n"}},
		{"derivable/tree", map[string]string{"CLAUDE.md": "```\nsrc/\n├── a.go\n└── b.go\n```\n"}, map[string]string{"CLAUDE.md": "```\ngo test ./...\n```\n"}},
		{"move/procedure", map[string]string{"CLAUDE.md": steps}, map[string]string{"CLAUDE.md": "## Release\n1. tag\n2. push\n"}},
		{"vague", map[string]string{"CLAUDE.md": "Always write clean code.\n"}, map[string]string{"CLAUDE.md": "Run make check before committing.\n"}},
		{"dup/line", map[string]string{
			"CLAUDE.md": "- Every public function needs a doc comment in this repo\n",
			"AGENTS.md": "- every public function needs a doc comment in this repo.\n",
			"x.md":      "",
		}, map[string]string{"CLAUDE.md": "- Every public function needs a doc comment in this repo\n"}},
		{"contradiction", map[string]string{"CLAUDE.md": "Always use pnpm for installs.\n\nNever use pnpm in CI scripts.\n"}, map[string]string{"CLAUDE.md": "Always use pnpm for installs.\n"}},
	}
	for _, c := range cases {
		t.Run(c.rule, func(t *testing.T) {
			if c.rule == "dup/line" {
				// AGENTS.md has to load for a cross-file duplicate: import it.
				c.pos["CLAUDE.md"] = "@AGENTS.md\n" + c.pos["CLAUDE.md"]
			}
			if rules(t, c.pos)[c.rule] == 0 {
				t.Errorf("positive fixture: %s not reported", c.rule)
			}
			if n := rules(t, c.neg)[c.rule]; n != 0 {
				t.Errorf("negative fixture: %s reported %d times", c.rule, n)
			}
		})
	}
}

func TestAgentsLoadedWithoutClaude(t *testing.T) {
	s, _ := Load(project(t, map[string]string{"AGENTS.md": "rules\n"}), "")
	if len(s.Docs) != 1 || s.Docs[0].Kind != Agents {
		t.Fatalf("AGENTS.md should load when no CLAUDE.md exists: %+v", s.Docs)
	}
	s, _ = Load(project(t, map[string]string{"AGENTS.md": "rules\n", "CLAUDE.local.md": "me\n"}), "")
	for _, d := range s.Docs {
		if d.Kind == Agents {
			t.Fatal("CLAUDE.local.md shadows AGENTS.md")
		}
	}
}

func TestPathScopedRulesNotAlways(t *testing.T) {
	s, _ := Load(project(t, map[string]string{
		".claude/rules/api.md":  "---\npaths:\n  - api/**\n---\nAPI rules\n",
		".claude/rules/base.md": "Base rules\n",
	}), "")
	always := map[string]bool{}
	for _, d := range s.Docs {
		always[filepath.Base(d.Path)] = d.Always
	}
	if always["api.md"] || !always["base.md"] {
		t.Fatalf("got %v", always)
	}
}

func TestImportsSkipCodeAndEmail(t *testing.T) {
	body := "mail me@rhy.io\n`@inline/x.md`\n```\n@fenced/x.md\n```\nreal @docs/a.md and @~/notes.md\n"
	var got []string
	for _, r := range Imports(body) {
		got = append(got, r.Target)
	}
	if strings.Join(got, ",") != "docs/a.md,~/notes.md" {
		t.Fatalf("got %v", got)
	}
}

func TestCommentsCostNothing(t *testing.T) {
	if StripComments("a<!-- long maintainer note -->b") != "ab" {
		t.Fatal("comments should be stripped")
	}
}

func TestBridgeFix(t *testing.T) {
	root := project(t, map[string]string{"CLAUDE.md": "x\n", "AGENTS.md": "rules\n"})
	s, _ := Load(root, "")
	for _, f := range Check(s) {
		if f.Rule == "agents/shadowed" {
			if f.Fix == nil || !f.Fix.Safe || !strings.HasPrefix(string(f.Fix.Edits[0].New), "@AGENTS.md\n") {
				t.Fatalf("bad fix %+v", f.Fix)
			}
			return
		}
	}
	t.Fatal("no finding")
}

func TestImportsSkipPackageNames(t *testing.T) {
	body := "npm @rhythmsh/echo and @types/node, decorator @Injectable, file @AGENTS.md and @docs/plan\n"
	var got []string
	for _, r := range Imports(body) {
		got = append(got, r.Target)
	}
	if strings.Join(got, ",") != "AGENTS.md" {
		t.Fatalf("got %v", got)
	}
}
