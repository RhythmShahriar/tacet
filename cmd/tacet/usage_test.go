// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RhythmShahriar/tacet/claude"
)

func TestCost(t *testing.T) {
	cases := []struct {
		model string
		u     claude.Usage
		want  float64
		ok    bool
	}{
		{"claude-opus-5-5", claude.Usage{Input: 1_000_000}, 4, true},
		{"claude-opus-5-5", claude.Usage{CacheWrite: 1_000_000, CacheRead: 1_000_000, Output: 1_000_000}, 5 + 0.2 + 20, true},
		{"claude-sonnet-4-5-20250929", claude.Usage{Input: 1_000_000}, 3, true},
		{"us.anthropic.claude-opus-4-5-20251101-v1:0", claude.Usage{Output: 1_000_000}, 25, true},
		{"claude-haiku-5-5", claude.Usage{Input: 50_000}, 0.005, true},
		{"claude-haiku-5-5", claude.Usage{Input: 200_000}, 0.1, true}, // past 100k: 5x
		{"claude-3-5-sonnet-20241022", claude.Usage{Input: 1_000_000}, 2, true},
		{"<synthetic>", claude.Usage{Input: 1_000_000}, 0, false},
		{"gpt-5", claude.Usage{Input: 1}, 0, false},
	}
	for _, c := range cases {
		got, ok := cost(c.model, c.u)
		if ok != c.ok || math.Abs(got-c.want) > 1e-9 {
			t.Errorf("cost(%s, %+v) = %v %v, want %v %v", c.model, c.u, got, ok, c.want, c.ok)
		}
	}
}

func TestModelName(t *testing.T) {
	for in, want := range map[string]string{
		"claude-opus-4-5-20251101": "opus-4-5",
		"claude-opus-5-5":          "opus-5-5",
		"":                         "unknown",
		"<synthetic>":              "<synthetic>",
	} {
		if got := modelName(in); got != want {
			t.Errorf("modelName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUSD(t *testing.T) {
	for v, want := range map[float64]string{0: "$0", 0.0042: "$0.0042", 1.234: "$1.23", 42.4: "$42"} {
		if got := usd(v); got != want {
			t.Errorf("usd(%v) = %q, want %q", v, got, want)
		}
	}
}

// turn is one assistant request with a tool call, and the user line
// returning a result of resultLen characters.
func turn(id, model string, at time.Time, in, write, read, out int, tool string, resultLen int) string {
	s := fmt.Sprintf(`{"type":"assistant","timestamp":%q,"sessionId":"s","message":{"id":%q,"role":"assistant","model":%q,"content":[{"type":"tool_use","id":"tu-%s","name":%q,"input":{}}],"usage":{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d}}}`,
		at.Format(time.RFC3339), id, model, id, tool, in, write, read, out) + "\n"
	s += fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu-%s","content":%q}]}}`,
		at.Add(time.Second).Format(time.RFC3339), id, strings.Repeat("x", resultLen)) + "\n"
	return s
}

// usageProject has a baseline three days ago, 5 expensive Sonnet
// requests before it and n cheaper Opus requests after it, plus a
// Haiku subagent.
func usageProject(t *testing.T, n int) string {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	base := time.Now().Add(-72 * time.Hour).UTC()
	writeFile(t, filepath.Join(root, ".tacet", "baseline.json"), fmt.Sprintf(
		`{"time":%q,"startup_tokens":4000,"per_request_tokens":40000,"cache_hit_rate":0.5,"sessions":1,"per_request_main":40000,"hit_rate_main":0.5,"requests_main":5,"requests":5}`,
		base.Format(time.RFC3339)))
	proj := claude.ProjectDir(root)
	var before, after, sub strings.Builder
	for i := 0; i < 5; i++ {
		before.WriteString(turn(fmt.Sprintf("b%d", i), "claude-sonnet-4-5-20250929", base.Add(-time.Hour+time.Duration(i)*time.Minute), 100, 20000, 20000, 500, "Read", 700))
	}
	for i := 0; i < n; i++ {
		tool := "Bash"
		if i%3 == 0 {
			tool = "Grep"
		}
		after.WriteString(turn(fmt.Sprintf("a%d", i), "claude-opus-5-5", base.Add(time.Hour+time.Duration(i)*time.Minute), 10, 1000, 30000, 200, tool, 350))
	}
	sub.WriteString(turn("x0", "claude-haiku-5-5", base.Add(2*time.Hour), 10, 5000, 0, 100, "Glob", 70))
	writeFile(t, filepath.Join(proj, "before.jsonl"), before.String())
	writeFile(t, filepath.Join(proj, "after.jsonl"), after.String())
	writeFile(t, filepath.Join(proj, "after", "subagents", "agent-a.jsonl"), sub.String())
	return root
}

func reportHTMLFor(t *testing.T, root string) string {
	t.Helper()
	var out, errw bytes.Buffer
	if c := run([]string{"-C", root, "report", "--no-open"}, &out, &errw); c != exitOK {
		t.Fatalf("exit %d: %s", c, errw.String())
	}
	b, err := os.ReadFile(filepath.Join(root, ".tacet", "report.html"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReportUsageTables(t *testing.T) {
	html := reportHTMLFor(t, usageProject(t, 24))
	for _, want := range []string{
		"Usage, last 14 days",
		"By day", "By model", "Top tools", "Main sessions and subagents",
		"opus-5-5", "sonnet-4-5", "haiku-5-5",
		`<td class="label mono">Bash</td><td class="num mono">16</td><td class="num mono">1.6k</td>`,
		`<td class="label mono">Grep</td><td class="num mono">8</td>`,
		"tacet on vs before", "estimated saving", "before init", "cost per request",
		"cost is an estimate", "list prices",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("report has no %q", want)
		}
	}
	if strings.Contains(html, "<script") || strings.Contains(html, "http://") {
		t.Error("the report must be self-contained")
	}
}

func TestReportOnVsBeforeNeedsData(t *testing.T) {
	html := reportHTMLFor(t, usageProject(t, 12))
	if !strings.Contains(html, "not enough data yet (12 of 20 requests)") {
		t.Error("no not-enough-data note under 20 requests")
	}
	if strings.Contains(html, "estimated saving") {
		t.Error("a saving shown on too little data")
	}
}

func TestReportNoUsage(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	html := reportHTMLFor(t, t.TempDir())
	if !strings.Contains(html, "No requests in the last 14 days.") || strings.Contains(html, "tacet on vs before") {
		t.Error("empty project should show no usage and no comparison")
	}
}
