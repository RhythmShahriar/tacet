// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/fix"
)

func TestNewRules(t *testing.T) {
	scoped := "# Repo\n\n## Web app\n- the UI lives in `web/src`\n- run tests with `cd web/ && npm test`\n- components in web/components use hooks\n"
	cases := []struct {
		rule string
		pos  map[string]string
		neg  map[string]string
	}{
		{"move/notes", map[string]string{"CLAUDE.md": "Run make test.\n\nTODO: rewrite this section\n"},
			map[string]string{"CLAUDE.md": "Run make test.\n\n<!-- TODO: rewrite this section -->\n```\nTODO: in code\n```\n"}},
		{"move/scoped", map[string]string{"CLAUDE.md": scoped, "web/src/a.ts": ""},
			map[string]string{"CLAUDE.md": scoped + "- shares types with `api/types`\n", "web/src/a.ts": "", "api/types/a.ts": ""}},
	}
	for _, c := range cases {
		t.Run(c.rule, func(t *testing.T) {
			if rules(t, c.pos)[c.rule] == 0 {
				t.Errorf("positive fixture: %s not reported", c.rule)
			}
			if n := rules(t, c.neg)[c.rule]; n != 0 {
				t.Errorf("negative fixture: %s reported %d times", c.rule, n)
			}
		})
	}
}

func TestScopedNeedsRealFolderAndSize(t *testing.T) {
	// A folder that doesn't exist and a short section aren't moved.
	if n := rules(t, map[string]string{"CLAUDE.md": "## Web\n- in `web/src`\n- a\n- b\n"})["move/scoped"]; n != 0 {
		t.Fatalf("missing folder: reported %d", n)
	}
	if n := rules(t, map[string]string{"CLAUDE.md": "## Web\n- in `web/src`\n", "web/src/a": ""})["move/scoped"]; n != 0 {
		t.Fatalf("short section: reported %d", n)
	}
	// A heading that doesn't name the folder: a general checklist that
	// happens to point at one file there.
	checklist := "## Ship checklist\n- tests pass\n- docs updated\n- knowledge in `packages/bot/kb.ts` updated\n"
	if n := rules(t, map[string]string{"CLAUDE.md": checklist, "packages/bot/kb.ts": ""})["move/scoped"]; n != 0 {
		t.Fatalf("checklist: reported %d", n)
	}
	// Pointers to docs/ are for reading, wherever the agent works.
	refs := "## Reference docs\n- `docs/api.md`\n- `docs/db.md`\n- `docs/ui.md`\n"
	if n := rules(t, map[string]string{"CLAUDE.md": refs, "docs/api.md": ""})["move/scoped"]; n != 0 {
		t.Fatalf("docs pointers: reported %d", n)
	}
}

// fixed applies the fixes for rule to the files and returns them all.
func fixed(t *testing.T, files map[string]string, rule string) (map[string]string, []core.Finding) {
	t.Helper()
	root := project(t, files)
	s, err := Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	var fs []*core.Fix
	var found []core.Finding
	for _, f := range Check(s) {
		if f.Rule == rule {
			found = append(found, f)
			if f.Fix != nil {
				fs = append(fs, f.Fix)
			}
		}
	}
	plan, err := Plan(fs)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for p, b := range files {
		out[p] = b
	}
	for _, c := range plan {
		r, _ := filepath.Rel(root, c.Path)
		out[filepath.ToSlash(r)] = string(c.New)
	}
	return out, found
}

func TestFixGolden(t *testing.T) {
	cases := []struct {
		name, rule string
		safe       bool
		before     map[string]string
		after      map[string]string // only the files that change
	}{
		{
			name: "tree with its heading", rule: "derivable/tree",
			before: map[string]string{"CLAUDE.md": "# App\n\n## Layout\n\n```\nsrc/\n├── a.go\n└── b.go\n```\n\n## Rules\n- run make test\n"},
			after:  map[string]string{"CLAUDE.md": "# App\n\n## Rules\n- run make test\n"},
		},
		{
			name: "tree inside prose keeps the heading", rule: "derivable/tree",
			before: map[string]string{"CLAUDE.md": "## Layout\nThe code is small.\n\n```\nsrc/\n└── a.go\n```\n\nEnd.\n"},
			after:  map[string]string{"CLAUDE.md": "## Layout\nThe code is small.\n\nEnd.\n"},
		},
		{
			name: "todo is safe", rule: "move/notes", safe: true,
			before: map[string]string{"CLAUDE.md": "Run make test.\n\n- TODO: split this file\n  once it grows\n- keep tests fast\n"},
			after:  map[string]string{"CLAUDE.md": "Run make test.\n\n<!-- - TODO: split this file\n  once it grows -->\n- keep tests fast\n"},
		},
		{
			name: "plain note is a suggestion", rule: "move/notes",
			before: map[string]string{"CLAUDE.md": "**Note:** the API docs are\nout of date.\n\nRun make test.\n"},
			after:  map[string]string{"CLAUDE.md": "<!-- **Note:** the API docs are\nout of date. -->\n\nRun make test.\n"},
		},
		{
			name: "duplicate removed from CLAUDE.md, not AGENTS.md", rule: "dup/line",
			before: map[string]string{
				"CLAUDE.md": "- Every public function needs a doc comment in this repo\n@AGENTS.md\n",
				"AGENTS.md": "# Agents\n- Every public function needs a doc comment in this repo\n",
			},
			after: map[string]string{"CLAUDE.md": "@AGENTS.md\n"},
		},
		{
			name: "later duplicate in one file", rule: "dup/line",
			before: map[string]string{"CLAUDE.md": "- Every public function needs a doc comment in this repo\n- b\n- Every public function needs a doc comment in this repo\n- c\n"},
			after:  map[string]string{"CLAUDE.md": "- Every public function needs a doc comment in this repo\n- b\n- c\n"},
		},
		{
			name: "monorepo package scope", rule: "move/scoped",
			before: map[string]string{
				"CLAUDE.md":                  "## Pipeline worker\n- code in `apps/pipeline-worker/src`\n- deploy with fly\n- logs in apps/pipeline-worker/logs\n",
				"apps/pipeline-worker/src/a": "",
			},
			after: map[string]string{
				"CLAUDE.md":                             "",
				".claude/rules/apps-pipeline-worker.md": "---\npaths:\n  - \"apps/pipeline-worker/**\"\n---\n\n## Pipeline worker\n- code in `apps/pipeline-worker/src`\n- deploy with fly\n- logs in apps/pipeline-worker/logs\n",
			},
		},
		{
			name: "scoped section moves to a path rule", rule: "move/scoped",
			before: map[string]string{
				"CLAUDE.md":    "# Repo\n\n## Web app\n- the UI lives in `web/src`\n- test with `cd web/ && npm test`\n- web/components use hooks\n\n## All\n- run make check\n",
				"web/src/a.ts": "",
			},
			after: map[string]string{
				"CLAUDE.md":            "# Repo\n\n## All\n- run make check\n",
				".claude/rules/web.md": "---\npaths:\n  - \"web/**\"\n---\n\n## Web app\n- the UI lives in `web/src`\n- test with `cd web/ && npm test`\n- web/components use hooks\n",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, found := fixed(t, c.before, c.rule)
			if len(found) == 0 || found[0].Fix == nil {
				t.Fatalf("no fixable %s finding", c.rule)
			}
			if found[0].Fix.Safe != c.safe {
				t.Errorf("safe = %v, want %v", found[0].Fix.Safe, c.safe)
			}
			if found[0].Fix.Tokens <= 0 {
				t.Errorf("startup delta %d, want a saving", found[0].Fix.Tokens)
			}
			for p, want := range c.after {
				if got[p] != want {
					t.Errorf("%s:\n got %q\nwant %q", p, got[p], want)
				}
			}
		})
	}
}

func TestNoFixOutsideProject(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "CLAUDE.md"), []byte("TODO: tidy my global rules\n"), 0o644)
	s, _ := Load(project(t, map[string]string{"CLAUDE.md": "x\n"}), home)
	for _, f := range Check(s) {
		if f.Rule == "move/notes" && f.Fix != nil {
			t.Fatal("must not plan edits to the user's own CLAUDE.md")
		}
	}
}

func TestFixesComposeAndUndo(t *testing.T) {
	before := "# App\n\n## Layout\n\n```\nsrc/\n└── a.go\n```\n\nTODO: tidy\n\n- Every public function needs a doc comment in this repo\n- Every public function needs a doc comment in this repo\n"
	root := project(t, map[string]string{"CLAUDE.md": before})
	s, _ := Load(root, "")
	var fs []*core.Fix
	for _, f := range Check(s) {
		if f.Fix != nil {
			fs = append(fs, f.Fix)
		}
	}
	if len(fs) != 3 {
		t.Fatalf("want 3 fixes, got %d", len(fs))
	}
	total, err := s.Saving(fs...)
	if err != nil || total <= 0 {
		t.Fatalf("saving %d, %v", total, err)
	}
	if _, err := fix.Apply(root, fs); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	want := "# App\n\n## Layout\n\n<!-- TODO: tidy -->\n\n- Every public function needs a doc comment in this repo\n"
	if string(b) != want {
		t.Fatalf("got %q\nwant %q", b, want)
	}
	if _, err := fix.Undo(root); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if string(b) != before {
		t.Fatal("undo didn't restore the file")
	}
}

func TestDiff(t *testing.T) {
	d := Diff("CLAUDE.md", []byte("a\nb\nc\nd\ne\nf\ng\n"), []byte("a\nb\nc\nX\ne\nf\ng\n"))
	want := "--- a/CLAUDE.md\n+++ b/CLAUDE.md\n@@ -2,5 +2,5 @@\n b\n c\n-d\n+X\n e\n f\n"
	if d != want {
		t.Fatalf("got\n%s\nwant\n%s", d, want)
	}
	if Diff("x", []byte("same\n"), []byte("same\n")) != "" {
		t.Fatal("no change, no diff")
	}
	if !strings.HasPrefix(Diff("n.md", nil, []byte("new\n")), "--- /dev/null\n") {
		t.Fatal("a new file diffs from /dev/null")
	}
}

func TestAfterSeesNewImports(t *testing.T) {
	root := project(t, map[string]string{"CLAUDE.md": "x\n", "AGENTS.md": strings.Repeat("rule line\n", 50)})
	s, _ := Load(root, "")
	for _, f := range Check(s) {
		if f.Rule == "agents/shadowed" {
			n, err := s.Saving(f.Fix)
			if err != nil || n >= 0 {
				t.Fatalf("the bridge loads AGENTS.md, so startup grows: %d %v", n, err)
			}
			return
		}
	}
	t.Fatal("no bridge finding")
}
