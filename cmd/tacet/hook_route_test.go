// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RhythmShahriar/tacet/codegraph"
	"github.com/RhythmShahriar/tacet/internal/config"
)

func TestSearchName(t *testing.T) {
	for pattern, want := range map[string]string{
		// positive: one name, a declaration or a call
		"Tally":                     "Tally",
		`\bTally\b`:                 "Tally",
		"func Tally":                "Tally",
		`func\s+Tally`:              "Tally",
		`func \(r \*Report\) Tally`: "Tally",
		"def parse_args":            "parse_args",
		"class UserStore":           "UserStore",
		`Tally\(`:                   "Tally",
		"Tally(":                    "Tally",
		"^type Report$":             "Report",
		// negative: text searches
		"TODO.*fix":       "",
		"Tally|Report":    "",
		"error":           "",
		"import os":       "",
		"failed to parse": "",
		"[A-Z]+Report":    "",
		"ab":              "",
		"":                "",
		"foo.bar":         "",
	} {
		if got := searchName(pattern); got != want {
			t.Errorf("searchName(%q) = %q, want %q", pattern, got, want)
		}
	}
	for pattern, want := range map[string]string{
		"**/*Tally*":     "Tally",
		"**/report.go":   "",
		"src/**/*.ts":    "",
		"**/*Tal?y*.go":  "",
		"**/UserStore*":  "UserStore",
		"**/[ab]Report*": "",
	} {
		if got := globName(pattern); got != want {
			t.Errorf("globName(%q) = %q, want %q", pattern, got, want)
		}
	}
}

// indexedProject is a project with a code index built, as tacet map
// leaves it.
func indexedProject(t testing.TB) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "report"), 0o750); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("report/report.go", "package report\n\n// Tally adds it up.\nfunc Tally(xs []int) int {\n\treturn len(xs)\n}\n\nfunc Lonely() {}\n")
	write("main.go", "package main\n\nimport \"x/report\"\n\nfunc main() {\n\treport.Tally(nil)\n\treport.Tally([]int{1})\n}\n")
	ix, err := codegraph.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	return root
}

func searchHook(t *testing.T, root string, cfg *config.Config, tool, input string) string {
	t.Helper()
	out := preSearch(root, cfg, &hookInput{ToolName: tool, ToolInput: json.RawMessage(input)})
	if out == nil {
		return ""
	}
	b, _ := json.Marshal(out)
	var got struct {
		H struct {
			Event   string `json:"hookEventName"`
			Context string `json:"additionalContext"`
			Perm    string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.H.Event != "PreToolUse" || got.H.Perm != "" {
		t.Fatalf("the note must only add context: %s", b)
	}
	return got.H.Context
}

func TestPreSearchPointsAtTheDefinition(t *testing.T) {
	root := indexedProject(t)
	cfg := config.Load(root)

	got := searchHook(t, root, cfg, "Grep", `{"pattern":"func Tally"}`)
	want := "tacet: `Tally` is defined at report/report.go:4 (2 callers). The tacet callers tool lists them without a grep."
	if got != want {
		t.Fatalf("note:\n got %q\nwant %q", got, want)
	}
	if len(got) > maxRouteNote {
		t.Errorf("note is %d chars, over %d", len(got), maxRouteNote)
	}
	if got := searchHook(t, root, cfg, "Glob", `{"pattern":"**/*Tally*"}`); !strings.Contains(got, "report/report.go:4") {
		t.Errorf("glob note: %q", got)
	}
	if got := searchHook(t, root, cfg, "Grep", `{"pattern":"Lonely"}`); !strings.Contains(got, "(no callers).") || strings.Contains(got, "callers tool") {
		t.Errorf("no-caller note: %q", got)
	}

	for _, in := range []string{
		`{"pattern":"Unknown"}`,           // not in the index
		`{"pattern":"TODO.*fix"}`,         // a text search
		`{"pattern":"tally"}`,             // another case is another name
		`{"pattern":"Tally","path":"/x"}`, // outside the project
		`not json`,
	} {
		if got := searchHook(t, root, cfg, "Grep", in); got != "" {
			t.Errorf("%s: want no note, got %q", in, got)
		}
	}

	_ = cfg.Set("map", false)
	if got := searchHook(t, root, cfg, "Grep", `{"pattern":"Tally"}`); got != "" {
		t.Errorf("map off: %q", got)
	}
}

func TestPreSearchQuietWithoutAFreshIndex(t *testing.T) {
	root := t.TempDir()
	cfg := config.Load(root)
	if got := searchHook(t, root, cfg, "Grep", `{"pattern":"Tally"}`); got != "" {
		t.Errorf("no index: %q", got)
	}
	if _, err := os.Stat(codegraph.DBPath(root)); !os.IsNotExist(err) {
		t.Errorf("the hook must not create an index: %v", err)
	}

	root = indexedProject(t)
	p := filepath.Join(root, "report", "report.go")
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	if got := searchHook(t, root, cfg, "Grep", `{"pattern":"Tally"}`); got != "" {
		t.Errorf("changed file: %q", got)
	}
}

func BenchmarkPreSearch(b *testing.B) {
	root := indexedProject(b)
	cfg := config.Load(root)
	in := &hookInput{ToolName: "Grep", ToolInput: json.RawMessage(`{"pattern":"func Tally"}`)}
	for b.Loop() {
		if preSearch(root, cfg, in) == nil {
			b.Fatal("no note")
		}
	}
}
