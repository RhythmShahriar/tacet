// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package gen

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/docs/lint"
	"github.com/RhythmShahriar/tacet/fix"
)

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

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInitNeverOverwrites(t *testing.T) {
	root := project(t, map[string]string{"CLAUDE.md": "mine\n", "Makefile": "test: ## run the tests\n\tgo test\n"})
	edits := Init(root)
	names := map[string]bool{}
	for _, e := range edits {
		names[filepath.Base(e.Path)] = true
	}
	if names["CLAUDE.md"] || !names["AGENTS.md"] || !names["ARCHITECTURE.md"] {
		t.Fatalf("planned %v", names)
	}
	// A file that appears after planning makes the whole apply fail.
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("raced\n"), 0o644)
	if _, err := fix.Apply(root, []*core.Fix{{ID: "docs-init", Edits: edits}}); err == nil {
		t.Fatal("apply should refuse to overwrite a file that appeared")
	}
	if read(t, filepath.Join(root, "AGENTS.md")) != "raced\n" || read(t, filepath.Join(root, "CLAUDE.md")) != "mine\n" {
		t.Fatal("an existing file was overwritten")
	}
	if len(Init(project(t, map[string]string{"AGENTS.md": "a", "CLAUDE.md": "c", "ARCHITECTURE.md": "x"}))) != 0 {
		t.Fatal("nothing missing, nothing planned")
	}
}

func TestTemplatesLeanAndLoaded(t *testing.T) {
	root := project(t, map[string]string{
		"Makefile":            "build: ## build the binary\n\tgo build\ntest: ## run the tests\n\tgo test\n",
		"core/core.go":        "// Package core holds the shared types. More here.\npackage core\n",
		"web/package.json":    `{"description": "the dashboard"}`,
		"node_modules/x/y.js": "",
	})
	if _, err := fix.Apply(root, []*core.Fix{{ID: "docs-init", Edits: Init(root)}}); err != nil {
		t.Fatal(err)
	}
	agents := read(t, filepath.Join(root, "AGENTS.md"))
	if n := strings.Count(agents, "\n"); n >= 60 {
		t.Fatalf("AGENTS.md is %d lines", n)
	}
	for _, want := range []string{"<!-- tacet · ", "- `make build`: build the binary", "- `make test`: run the tests", "## Never"} {
		if !strings.Contains(agents, want) {
			t.Errorf("AGENTS.md lacks %q", want)
		}
	}
	arch := read(t, filepath.Join(root, "ARCHITECTURE.md"))
	for _, want := range []string{"- `core/`: Holds the shared types.", "- `web/`: The dashboard.", "## Invariants", "## Boundaries"} {
		if !strings.Contains(arch, want) {
			t.Errorf("ARCHITECTURE.md lacks %q", want)
		}
	}
	if strings.Contains(arch, "node_modules") || strings.Contains(arch, "](") {
		t.Error("codemap lists node_modules or has links")
	}
	for _, f := range []string{"AGENTS.md", "CLAUDE.md", "ARCHITECTURE.md"} {
		b := read(t, filepath.Join(root, f))
		if strings.Contains(b, "—") || strings.Contains(b, ", and ") || strings.Contains(b, ", but ") { // style:ignore
			t.Errorf("%s breaks the writing rules", f)
		}
	}
	// CLAUDE.md imports AGENTS.md, so lint finds nothing to fix in the pair.
	s, _ := lint.Load(root, "")
	for _, f := range lint.Check(s) {
		t.Errorf("fresh templates have a finding: %s %s", f.Rule, f.Message)
	}
	if len(s.All()) != 2 {
		t.Fatalf("CLAUDE.md should load AGENTS.md: %d docs", len(s.All()))
	}
}

func TestCommandsFromPackageJSON(t *testing.T) {
	root := project(t, map[string]string{"package.json": `{"scripts":{"dev":"next","test":"vitest","postinstall":"x"}}`, "pnpm-lock.yaml": ""})
	var got []string
	for _, c := range Commands(root) {
		got = append(got, c.Run)
	}
	if strings.Join(got, ",") != "pnpm dev,pnpm test" {
		t.Fatalf("got %v", got)
	}
	got = nil
	for _, c := range Commands(project(t, map[string]string{"go.mod": "module x\n"})) {
		got = append(got, c.Run)
	}
	if len(got) != 3 || got[1] != "go test ./..." {
		t.Fatalf("go fallback: %v", got)
	}
}

func TestSyncDriftAndTargets(t *testing.T) {
	root := project(t, map[string]string{"AGENTS.md": "<!-- tacet · header -->\n# Rules\n\n\n\n- run make test\n", "GEMINI.md": "hand written\n"})
	res, err := Sync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].Target.Name != "gemini" || res.Items[0].State != Manual {
		t.Fatalf("only the existing GEMINI.md, as manual: %+v", res.Items)
	}
	res, _ = Sync(root, []string{"cursor", "copilot"})
	if len(res.Items) != 3 {
		t.Fatalf("named targets join existing ones: %d", len(res.Items))
	}
	for _, it := range res.Items {
		os.MkdirAll(filepath.Dir(it.Path), 0o755)
		os.WriteFile(it.Path, it.New, 0o644)
	}
	cursor := read(t, filepath.Join(root, ".cursor", "rules", "agents.mdc"))
	if !strings.HasPrefix(cursor, "---\n") || !strings.Contains(cursor, "<!-- tacet sync: sha256:") || !strings.HasSuffix(cursor, "# Rules\n\n- run make test\n") {
		t.Fatalf("cursor file: %q", cursor)
	}
	if strings.Contains(cursor, "tacet · header") {
		t.Fatal("comments should not be copied")
	}
	res, _ = Sync(root, nil)
	for _, it := range res.Items {
		if it.State != Current {
			t.Fatalf("%s: %s, want current", it.Target.Name, it.State)
		}
	}
	// AGENTS.md changes: stale. A generated file edited by hand: drift.
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Rules\n- run make check\n"), 0o644)
	gem := filepath.Join(root, "GEMINI.md")
	os.WriteFile(gem, []byte(read(t, gem)+"- my own line\n"), 0o644)
	res, _ = Sync(root, nil)
	got := map[string]State{}
	for _, it := range res.Items {
		got[it.Target.Name] = it.State
	}
	if got["gemini"] != Drift || got["cursor"] != Stale || got["copilot"] != Stale {
		t.Fatalf("got %v", got)
	}
}

func TestSyncLimitsAndErrors(t *testing.T) {
	if _, err := Sync(t.TempDir(), nil); !errors.Is(err, ErrNoAgents) {
		t.Fatalf("got %v", err)
	}
	root := project(t, map[string]string{"AGENTS.md": strings.Repeat("x", lint.CodexLimit+1)})
	res, _ := Sync(root, nil)
	if !res.OverCodex || len(res.Items) != 0 {
		t.Fatalf("got %+v", res)
	}
	if _, err := Sync(root, []string{"vim"}); err == nil {
		t.Fatal("unknown target should fail")
	}
}
