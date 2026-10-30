// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RhythmShahriar/tacet/skills"
)

const fakeSHA = "0123456789abcdef0123456789abcdef01234567"

type fakeFile struct{ mode, data string }

// fakeGitHub serves the commits, trees and raw endpoints for repo o/r at
// one commit, with ref "main" and "HEAD" resolving to it.
func fakeGitHub(t *testing.T, files map[string]fakeFile) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		p := r.URL.Path
		switch {
		case p == "/repos/o/r/commits/main" || p == "/repos/o/r/commits/HEAD":
			if r.Header.Get("Accept") != "application/vnd.github.sha" {
				http.Error(w, "accept", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(fakeSHA))
		case p == "/repos/o/r/git/trees/"+fakeSHA:
			var tree []map[string]any
			for path, f := range files {
				tree = append(tree, map[string]any{"path": path, "mode": f.mode, "type": "blob", "size": len(f.data)})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": fakeSHA, "tree": tree, "truncated": false})
		case strings.HasPrefix(p, "/o/r/"+fakeSHA+"/"):
			f, ok := files[strings.TrimPrefix(p, "/o/r/"+fakeSHA+"/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(f.data))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	api, raw := skills.GitHubAPI, skills.GitHubRaw
	skills.GitHubAPI, skills.GitHubRaw = srv.URL, srv.URL
	t.Cleanup(func() { skills.GitHubAPI, skills.GitHubRaw = api, raw })
	return &hits
}

const fakeSkill = "---\nname: pdf-tools\ndescription: Use when filling or merging PDF forms.\n---\n# PDF tools\n"

func runTacet(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errw bytes.Buffer
	c := run(args, &out, &errw)
	return c, out.String(), errw.String()
}

func TestSkillsAddWritesPinnedSkill(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	fakeGitHub(t, map[string]fakeFile{
		"skills/pdf/SKILL.md":     {"100644", fakeSkill},
		"skills/pdf/reference.md": {"100644", "# reference\n"},
		"skills/other/SKILL.md":   {"100644", "---\ndescription: other\n---\n"},
	})
	dir := t.TempDir()

	// --json without --yes shows the plan and writes nothing.
	c, out, errs := runTacet(t, "-C", dir, "--json", "skills", "add", "o/r/skills/pdf@main")
	if c != exitOK || !strings.Contains(out, `"written": false`) || !strings.Contains(out, fakeSHA) {
		t.Fatalf("json plan: %d %s %s", c, out, errs)
	}
	path := filepath.Join(dir, ".claude", "skills", "pdf-tools", "SKILL.md")
	if _, err := os.Stat(path); err == nil {
		t.Fatal("--json wrote the skill")
	}

	c, out, errs = runTacet(t, "-C", dir, "--yes", "skills", "add", "o/r/skills/pdf@main")
	if c != exitOK {
		t.Fatalf("exit %d: %s %s", c, out, errs)
	}
	for _, want := range []string{"github.com/o/r/skills/pdf", "0123456", "SKILL.md", "reference.md", "description: Use when filling", "tokens every session"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "other") {
		t.Errorf("imported a sibling skill:\n%s", out)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "---\n# imported by tacet skills add from github.com/o/r/skills/pdf at "+fakeSHA+"\nname: pdf-tools\n") {
		t.Fatalf("SKILL.md:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "pdf-tools", "reference.md")); err != nil {
		t.Fatal(err)
	}
}

func TestSkillsAddNeverOverwrites(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	fakeGitHub(t, map[string]fakeFile{"skills/pdf/SKILL.md": {"100644", fakeSkill}})
	dir := t.TempDir()
	mine := filepath.Join(dir, ".claude", "skills", "pdf-tools", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(mine), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, errs := runTacet(t, "-C", dir, "--yes", "skills", "add", "o/r/skills/pdf")
	if c == exitOK || !strings.Contains(errs, "already") && !strings.Contains(errs, "exists") {
		t.Fatalf("exit %d: %s", c, errs)
	}
	if b, _ := os.ReadFile(mine); string(b) != "mine" {
		t.Fatalf("overwrote the skill: %q", b)
	}
}

func TestSkillsAddRefusesScripts(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	fakeGitHub(t, map[string]fakeFile{
		"skills/pdf/SKILL.md":       {"100644", fakeSkill},
		"skills/pdf/scripts/run.py": {"100644", "print('hi')\n"},
		"skills/pdf/bin/tool":       {"100755", "\x7fELF\x00\x00"},
	})
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".claude", "skills", "pdf-tools")
	c, out, errs := runTacet(t, "-C", dir, "--yes", "skills", "add", "o/r/skills/pdf")
	if c == exitOK || !strings.Contains(errs, "scripts/run.py") || !strings.Contains(errs, "bin/tool") || !strings.Contains(errs, "--allow-scripts") {
		t.Fatalf("exit %d: %s %s", c, out, errs)
	}
	if _, err := os.Stat(skillDir); err == nil {
		t.Fatal("wrote a skill with scripts")
	}
	c, out, errs = runTacet(t, "-C", dir, "--yes", "skills", "add", "--allow-scripts", "o/r/skills/pdf")
	if c != exitOK {
		t.Fatalf("with --allow-scripts: %d %s %s", c, out, errs)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "scripts", "run.py")); err != nil {
		t.Fatal(err)
	}
}

func TestSkillsAddListFetchesNothing(t *testing.T) {
	hits := fakeGitHub(t, nil)
	c, out, _ := runTacet(t, "-C", t.TempDir(), "skills", "add", "--list")
	if c != exitOK || !strings.Contains(out, "examples, not endorsements") || !strings.Contains(out, "anthropics/skills") {
		t.Fatalf("exit %d:\n%s", c, out)
	}
	if hits.Load() != 0 {
		t.Fatalf("--list made %d requests", hits.Load())
	}
}

func TestSkillsAddMissingSkill(t *testing.T) {
	fakeGitHub(t, map[string]fakeFile{"README.md": {"100644", "hi"}})
	c, _, errs := runTacet(t, "-C", t.TempDir(), "--yes", "skills", "add", "o/r/nothing")
	if c == exitOK || !strings.Contains(errs, "no SKILL.md") {
		t.Fatalf("exit %d: %s", c, errs)
	}
}
