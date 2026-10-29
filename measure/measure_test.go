// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package measure

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RhythmShahriar/tacet/claude"
)

var t0 = time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC)

// turn is an assistant request at t0 plus min minutes.
func turn(min int, write, read int) claude.Turn {
	return claude.Turn{Role: "assistant", Time: t0.Add(time.Duration(min) * time.Minute), Model: "m",
		Usage: claude.Usage{Input: 0, CacheWrite: write, CacheRead: read, Output: 10}}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestSumSplitsMainAndSubagents(t *testing.T) {
	main := &claude.Session{ID: "s1", Turns: []claude.Turn{
		turn(0, 10_000, 90_000), // warm: 100k, 90% read
		turn(1, 0, 100_000),     // warm: 100k, all read
	}}
	sub := &claude.Session{ID: "a1", Subagent: true, Turns: []claude.Turn{
		turn(0, 20_000, 0),
		turn(1, 0, 20_000),
	}}
	u := Sum([]*claude.Session{main, sub}, time.Time{})
	if u.Requests != 4 || u.MainRequests != 2 || u.SubRequests != 2 {
		t.Fatalf("requests %d main %d sub %d", u.Requests, u.MainRequests, u.SubRequests)
	}
	if u.PerRequestMain() != 100_000 {
		t.Fatalf("per request main %d", u.PerRequestMain())
	}
	if u.PerRequest() != 60_000 {
		t.Fatalf("per request all %d", u.PerRequest())
	}
	if !near(u.HitRateMain(), 0.95) {
		t.Fatalf("hit rate main %v", u.HitRateMain())
	}
	if u.MainCacheWrite != 10_000 || u.MainCacheRead != 190_000 || u.MainInput != 0 {
		t.Fatalf("main totals %+v", u)
	}
}

func TestSumIdleExcluded(t *testing.T) {
	s := &claude.Session{ID: "s1", Turns: []claude.Turn{
		turn(0, 0, 100_000),
		turn(1, 0, 100_000),
		turn(200, 100_000, 0), // back after 3h: the cache expired
		turn(201, 0, 100_000),
		turn(202, 60_000, 40_000), // rewrote most of it with no gap: not idle
	}}
	u := Sum([]*claude.Session{s}, time.Time{})
	if len(u.Misses) != 2 || u.Misses[0].Cause != "idle" || u.Misses[1].Cause != "other" {
		t.Fatalf("misses %+v", u.Misses)
	}
	if u.MainIdle != 1 || u.MainIdleIn != 100_000 || u.MainIdleRead != 0 {
		t.Fatalf("idle %d in %d read %d", u.MainIdle, u.MainIdleIn, u.MainIdleRead)
	}
	if !near(u.HitRateMain(), 340_000.0/500_000) {
		t.Fatalf("hit rate %v", u.HitRateMain())
	}
	if !near(u.HitRateMainActive(), 340_000.0/400_000) {
		t.Fatalf("hit rate without idle %v", u.HitRateMainActive())
	}
	// A subagent's idle miss is listed but leaves the main numbers alone.
	sub := &claude.Session{ID: "a1", Subagent: true, Turns: []claude.Turn{turn(0, 0, 50_000), turn(30, 50_000, 0)}}
	u = Sum([]*claude.Session{s, sub}, time.Time{})
	if len(u.Misses) != 3 || !u.Misses[2].Subagent || u.MainIdle != 1 {
		t.Fatalf("misses %+v main idle %d", u.Misses, u.MainIdle)
	}
}

func TestSumRange(t *testing.T) {
	s := &claude.Session{ID: "s1", Turns: []claude.Turn{turn(0, 0, 10_000), turn(10, 0, 20_000), turn(20, 0, 30_000)}}
	u := SumRange([]*claude.Session{s}, t0.Add(5*time.Minute), t0.Add(20*time.Minute))
	if u.MainRequests != 1 || u.PerRequestMain() != 20_000 {
		t.Fatalf("requests %d per request %d", u.MainRequests, u.PerRequestMain())
	}
}

func TestMeasureUsageBefore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	root := t.TempDir()
	proj := claude.ProjectDir(root)
	line := func(id string, at time.Time, write, read int) string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":%q,"role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":0,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":5}}}`,
			at.Format(time.RFC3339), id, write, read) + "\n"
	}
	write := func(p, s string, mtime time.Time) {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	until := t0.Add(time.Hour)
	// Before init: one main session of 50k requests and a subagent.
	write(filepath.Join(proj, "old.jsonl"), line("m1", t0, 0, 50_000)+line("m2", t0.Add(time.Minute), 0, 50_000), t0.Add(2*time.Minute))
	write(filepath.Join(proj, "old", "subagents", "agent-a.jsonl"), line("x1", t0, 5_000, 0), t0.Add(2*time.Minute))
	// The session init ran in kept going: only its part before until counts.
	write(filepath.Join(proj, "init.jsonl"), line("i1", until.Add(-time.Minute), 0, 80_000)+line("i2", until.Add(time.Minute), 0, 999_000), until.Add(2*time.Minute))
	// A later session is left out.
	write(filepath.Join(proj, "new.jsonl"), line("n1", until.Add(time.Hour), 0, 999_000), until.Add(time.Hour))

	u := MeasureUsageBefore(root, until, 30)
	if u.MainRequests != 3 || u.SubRequests != 1 {
		t.Fatalf("main %d sub %d", u.MainRequests, u.SubRequests)
	}
	if u.PerRequestMain() != 60_000 {
		t.Fatalf("per request main %d", u.PerRequestMain())
	}
}
