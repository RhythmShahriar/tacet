// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsNewJSONThenYes(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	var out, errw bytes.Buffer
	args := []string{"-C", dir, "--json", "skills", "new", "run the e2e tests against staging; then post the summary"}
	if c := run(args, &out, &errw); c != exitOK {
		t.Fatalf("exit %d: %s", c, errw.String())
	}
	var got struct {
		Draft struct {
			Name, Description, Path, Skill string
			Steps                          []string
		} `json:"draft"`
		Written bool `json:"written"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if got.Written || got.Draft.Name != "run-e2e-tests-against" || len(got.Draft.Steps) != 2 ||
		!strings.HasPrefix(got.Draft.Description, "Use when asked to run the e2e tests") {
		t.Fatalf("draft %+v", got)
	}
	path := filepath.Join(dir, ".claude", "skills", got.Draft.Name, "SKILL.md")
	if _, err := os.Stat(path); err == nil {
		t.Fatal("--json wrote the skill")
	}

	// Not a terminal and no --yes: shows the draft, writes nothing.
	out.Reset()
	if c := run([]string{"-C", dir, "skills", "new", "--name", "e2e", "run the e2e tests"}, &out, &errw); c != exitOK {
		t.Fatalf("exit %d: %s", c, errw.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "e2e", "SKILL.md")); err == nil {
		t.Fatal("wrote without a yes")
	}

	out.Reset()
	args[2] = "--yes"
	if c := run(args, &out, &errw); c != exitOK {
		t.Fatalf("exit %d: %s", c, errw.String())
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "<!-- tacet · drafted from a description -->") {
		t.Fatalf("written %q %v", b, err)
	}

	// The same --name again never overwrites it.
	errw.Reset()
	if c := run([]string{"-C", dir, "--yes", "skills", "new", "--name", got.Draft.Name, "something else"}, &out, &errw); c == exitOK {
		t.Fatal("overwrote an existing skill")
	}
	if b2, _ := os.ReadFile(path); !bytes.Equal(b, b2) {
		t.Fatal("existing skill changed")
	}
}

func TestSkillsCheckJSON(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	for name, desc := range map[string]string{
		"test-fix":   "Use when asked to run the unit tests and fix every failure before a commit.",
		"test-fix-2": "Use when asked to run the unit tests and fix every failure before you push.",
		"long":       "Use when asked to review the code. " + strings.Repeat("It reads the diff and the tests before it comments on them. ", 12),
	} {
		p := filepath.Join(dir, ".claude", "skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: "+name+"\ndescription: "+desc+"\n---\nbody\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out, errw bytes.Buffer
	if c := run([]string{"-C", dir, "--json", "skills", "check"}, &out, &errw); c != exitOK {
		t.Fatalf("exit %d: %s", c, errw.String())
	}
	var got struct {
		Findings []struct {
			Rule, Fix, Path string
			Tokens          int `json:"startup_tokens_saved"`
		} `json:"findings"`
		Total int `json:"startup_tokens_saved"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	rules := map[string]string{}
	sum := 0
	for _, f := range got.Findings {
		rules[f.Rule] = f.Fix
		sum += f.Tokens
		if f.Tokens <= 0 || filepath.IsAbs(f.Path) {
			t.Errorf("finding %+v", f)
		}
	}
	if rules["skills/overlap"] == "" || rules["skills/description"] != "skill-shorten-long" || got.Total != sum {
		t.Fatalf("got %+v", got)
	}

	out.Reset()
	if c := run([]string{"-C", dir, "skills", "check"}, &out, &errw); c != exitOK || !strings.Contains(out.String(), "startup tokens") {
		t.Fatalf("exit %d:\n%s", c, out.String())
	}
}
