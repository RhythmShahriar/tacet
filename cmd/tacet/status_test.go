// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RhythmShahriar/tacet/claude"
)

var baseTime = time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)

// request is one assistant line of a synthetic transcript.
func request(id string, at time.Time, write, read int) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"sessionId":"s","message":{"id":%q,"role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":0,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":5}}}`,
		at.Format(time.RFC3339), id, write, read) + "\n"
}

// requests is n warm requests a minute apart from start.
func requests(prefix string, start time.Time, n, size int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(request(fmt.Sprintf("%s%d", prefix, i), start.Add(time.Duration(i)*time.Minute), size/10, size-size/10))
	}
	return b.String()
}

// statusProject is an initialized project with a baseline and main
// sessions of n requests after it, plus a large subagent.
func statusProject(t *testing.T, baseline string, n int) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".tacet", "config.yaml"), "features:\n  speak: true\n")
	writeFile(t, filepath.Join(root, ".tacet", "baseline.json"), baseline)
	proj := claude.ProjectDir(root)
	writeFile(t, filepath.Join(proj, "s1.jsonl"), requests("m", baseTime.Add(24*time.Hour), n, 100_000))
	writeFile(t, filepath.Join(proj, "s1", "subagents", "agent-a.jsonl"), requests("x", baseTime.Add(24*time.Hour), 30, 20_000))
	return root
}

func status(t *testing.T, root string) (statusJSON, string) {
	t.Helper()
	var out, errw bytes.Buffer
	if c := run([]string{"-C", root, "status", "--json"}, &out, &errw); c != exitOK {
		t.Fatalf("exit %d: %s", c, errw.String())
	}
	var st statusJSON
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if c := run([]string{"-C", root, "status"}, &out, &errw); c != exitOK {
		t.Fatalf("exit %d: %s", c, errw.String())
	}
	return st, out.String()
}

const newBaseline = `{"time":"2026-10-28T00:00:00Z","startup_tokens":4000,"per_request_tokens":60000,"cache_hit_rate":0.5,"sessions":3,
"per_request_main":200000,"hit_rate_main":0.8,"requests_main":40,"requests":100,"subagent_requests":60}`

func TestStatusNotEnoughData(t *testing.T) {
	root := statusProject(t, newBaseline, 19)
	st, text := status(t, root)
	if st.EnoughData || st.RequestsMain != 19 || st.MinRequests != 20 || st.SubRequests != 30 {
		t.Fatalf("enough %v main %d min %d sub %d", st.EnoughData, st.RequestsMain, st.MinRequests, st.SubRequests)
	}
	if !strings.Contains(text, "not enough data yet (19 of 20 requests)") {
		t.Fatalf("status:\n%s", text)
	}
	if strings.Contains(text, "-50%") {
		t.Fatal("a percentage shown without enough data")
	}
	if _, ok := st.Features["speak"]; ok {
		t.Fatal("an old config's speak feature came back")
	}
}

func TestStatusComparesMainOnly(t *testing.T) {
	root := statusProject(t, newBaseline, 20)
	st, text := status(t, root)
	if !st.EnoughData || st.PerRequestMain != 100_000 {
		t.Fatalf("enough %v per request main %d", st.EnoughData, st.PerRequestMain)
	}
	// The subagent's small requests pull the overall average down; the
	// comparison must not use it.
	if st.PerRequest >= st.PerRequestMain {
		t.Fatalf("overall %d main %d", st.PerRequest, st.PerRequestMain)
	}
	for _, want := range []string{"200k → 100k", "-50%", "20 main-session requests since init", "subagents", "30 requests", "not in the comparison"} {
		if !strings.Contains(text, want) {
			t.Errorf("status has no %q:\n%s", want, text)
		}
	}
}

func TestStatusRebuildsOldBaseline(t *testing.T) {
	old := `{"time":"2026-10-28T00:00:00Z","startup_tokens":4000,"per_request_tokens":60000,"cache_hit_rate":0.5,"sessions":3}`
	root := statusProject(t, old, 20)
	// A session before init: 400k per main request, and a subagent.
	proj := claude.ProjectDir(root)
	before := baseTime.Add(-2 * time.Hour)
	writeFile(t, filepath.Join(proj, "s0.jsonl"), requests("o", before, 5, 400_000))
	writeFile(t, filepath.Join(proj, "s0", "subagents", "agent-b.jsonl"), requests("y", before, 5, 10_000))
	for _, p := range []string{filepath.Join(proj, "s0.jsonl"), filepath.Join(proj, "s0", "subagents", "agent-b.jsonl")} {
		if err := os.Chtimes(p, before.Add(time.Hour), before.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	bpath := filepath.Join(root, ".tacet", "baseline.json")
	was, _ := os.ReadFile(bpath)

	st, text := status(t, root)
	b := st.Baseline
	if b == nil || b.RequestsMain != 5 || b.PerRequestMain != 400_000 || b.SubRequests != 5 || b.Requests != 10 {
		t.Fatalf("baseline %+v", b)
	}
	if b.PerRequest != 60_000 || b.Sessions != 3 {
		t.Fatalf("stored numbers changed: %+v", b)
	}
	if !st.EnoughData || !strings.Contains(text, "400k → 100k") {
		t.Fatalf("enough %v:\n%s", st.EnoughData, text)
	}
	if now, _ := os.ReadFile(bpath); !bytes.Equal(now, was) {
		t.Fatal("status rewrote the baseline")
	}
}
