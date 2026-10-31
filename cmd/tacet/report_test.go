// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/claude"
)

// A session after the baseline: one warm request, then one ten minutes
// later that rewrote the cache (an idle miss), plus a subagent.
const reportSession = `{"type":"user","timestamp":"2026-10-29T01:00:00Z","sessionId":"s1","origin":{"kind":"human"},"message":{"role":"user","content":[{"type":"text","text":"read the spec"}]}}
{"type":"assistant","timestamp":"2026-10-29T01:00:05Z","message":{"id":"m1","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":2,"cache_creation_input_tokens":1000,"cache_read_input_tokens":20000,"output_tokens":50}}}
{"type":"assistant","timestamp":"2026-10-29T01:10:05Z","message":{"id":"m2","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":2,"cache_creation_input_tokens":21000,"cache_read_input_tokens":0,"output_tokens":50}}}
`

const reportSub = `{"type":"assistant","timestamp":"2026-10-29T01:01:00Z","message":{"id":"x1","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":10,"cache_creation_input_tokens":500,"cache_read_input_tokens":0,"output_tokens":40}}}
`

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "CLAUDE.md"), "# Project\n\nRun make test before a commit.\n")
	writeFile(t, filepath.Join(root, ".tacet", "baseline.json"),
		`{"time":"2026-10-28T00:00:00Z","startup_tokens":4000,"per_request_tokens":40000,"cache_hit_rate":0.5,"sessions":3}`)
	writeFile(t, filepath.Join(root, ".tacet", "savings.jsonl"),
		`{"time":"2026-10-29T01:00:01Z","file":"spec.pdf","full_tokens":30000,"read_tokens":4000,"saved_tokens":26000,"method":"focus"}
{"time":"2026-10-29T01:00:02Z","file":"spec.pdf","read_tokens":1000,"saved_tokens":-1000,"method":"follow-up"}
`)
	proj := claude.ProjectDir(root)
	writeFile(t, filepath.Join(proj, "s1.jsonl"), reportSession)
	writeFile(t, filepath.Join(proj, "s1", "subagents", "agent-a.jsonl"), reportSub)

	var out, errw bytes.Buffer
	if c := run([]string{"-C", root, "report", "--no-open"}, &out, &errw); c != exitOK {
		t.Fatalf("exit %d: %s", c, errw.String())
	}
	b, err := os.ReadFile(filepath.Join(root, ".tacet", "report.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{
		"<!-- tacet · github.com/RhythmShahriar/tacet -->",
		"Made by Rhythm Shahriar",
		"CLAUDE.md",               // startup breakdown
		"at init (",               // baseline comparison
		"idle for over 5 minutes", // the miss cause
		"spec.pdf",                // document savings
		"25k",                     // net: 26000 - 1000
		"1 subagent made 1 request",
		"prefers-color-scheme: dark",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("report has no %q", want)
		}
	}
	if strings.Contains(html, "<script") || strings.Contains(html, "http://") {
		t.Error("the report must be self-contained")
	}
}

func TestReportUsage(t *testing.T) {
	var out, errw bytes.Buffer
	if c := run([]string{"-C", t.TempDir(), "report", "--bogus"}, &out, &errw); c != exitUsage {
		t.Fatalf("exit %d, want %d", c, exitUsage)
	}
}

func TestPlural(t *testing.T) {
	for in, want := range map[string]string{"merged branch": "2 merged branches", "stale entry": "2 stale entries", "worktree": "2 worktrees", "day": "2 days", "file": "2 files", "class": "2 classes"} {
		if got := plural(2, in); got != want {
			t.Errorf("plural(2, %q) = %q, want %q", in, got, want)
		}
	}
	if plural(1, "stale entry") != "1 stale entry" {
		t.Error("singular")
	}
}
