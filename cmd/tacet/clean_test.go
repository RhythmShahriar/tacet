// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RhythmShahriar/tacet/claude"
)

// cleanRepo is a temp repository with one commit and n clean agent
// worktrees and the clock moved past the in-use guard.
func cleanRepo(t *testing.T, n int) (root string, wts []string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root, _ = filepath.EvalSymlinks(t.TempDir())
	g := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	g("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	g("add", ".")
	g("commit", "-q", "-m", "init")
	for i := range n {
		p := filepath.Join(root, ".claude", "worktrees", fmt.Sprintf("agent-%d", i))
		g("worktree", "add", "-q", p, "-b", fmt.Sprintf("worktree-agent-%d", i))
		wts = append(wts, p)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	old := cleanNow
	cleanNow = func() time.Time { return time.Now().Add(2 * time.Hour) }
	t.Cleanup(func() { cleanNow = old })
	return root, wts
}

func runOut(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	c := run(args, &out, &errb)
	return out.String() + errb.String(), c
}

func TestCleanDryRunChangesNothing(t *testing.T) {
	root, wts := cleanRepo(t, 2)
	reads := filepath.Join(root, ".tacet", "reads", "s-pdf.json")
	_ = os.MkdirAll(filepath.Dir(reads), 0o750)
	_ = os.WriteFile(reads, []byte("{}"), 0o600)
	// --dry-run wins over --yes; no terminal and no --yes is a dry run.
	for _, args := range [][]string{{"-C", root, "--yes", "clean", "--dry-run"}, {"-C", root, "clean"}} {
		out, c := runOut(t, args...)
		if c != exitOK || !strings.Contains(out, "2 safe to remove") || !strings.Contains(out, "nothing changed") {
			t.Fatalf("%v: exit %d\n%s", args, c, out)
		}
		for _, p := range append(wts, reads) {
			if _, err := os.Stat(p); err != nil {
				t.Fatalf("%v removed %s", args, p)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".tacet", "clean.json")); err == nil {
		t.Fatal("a dry run wrote the summary")
	}
}

func TestCleanYesRemovesAndReportsJSON(t *testing.T) {
	root, wts := cleanRepo(t, 2)
	if err := os.WriteFile(filepath.Join(wts[1], "a.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, c := runOut(t, "-C", root, "--json", "--yes", "clean")
	if c != exitOK {
		t.Fatalf("exit %d\n%s", c, out)
	}
	var rep cleanReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if rep.DryRun || rep.Totals.Removed != 1 || rep.Totals.Kept != 1 || rep.Totals.KeptBy["uncommitted changes"] != 1 {
		t.Fatalf("totals %+v", rep.Totals)
	}
	if _, err := os.Stat(wts[0]); err == nil {
		t.Error("safe worktree not removed")
	}
	if _, err := os.Stat(wts[1]); err != nil {
		t.Error("worktree with uncommitted changes removed")
	}
}

func TestCleanTranscriptsOnlyWithFlag(t *testing.T) {
	root, _ := cleanRepo(t, 0)
	pd := claude.ProjectDir(root)
	old := filepath.Join(pd, "old.jsonl")
	_ = os.MkdirAll(filepath.Join(pd, "old", "subagents"), 0o750)
	_ = os.WriteFile(old, []byte("{}\n"), 0o600)
	past := time.Now().Add(-120 * 24 * time.Hour)
	for _, p := range []string{old, filepath.Join(pd, "old", "subagents"), filepath.Join(pd, "old")} {
		_ = os.Chtimes(p, past, past)
	}
	for _, args := range [][]string{{"-C", root, "--yes", "clean"}, {"-C", root, "--yes", "clean", "--all"}, {"-C", root, "clean", "--transcripts"}} {
		if out, c := runOut(t, args...); c != exitOK {
			t.Fatalf("%v: exit %d\n%s", args, c, out)
		}
		if _, err := os.Stat(old); err != nil {
			t.Fatalf("%v deleted a transcript", args)
		}
	}
	out, c := runOut(t, "-C", root, "--yes", "clean", "--transcripts", "--older-than", "90d")
	if c != exitOK || !strings.Contains(out, "skill ideas read these transcripts") {
		t.Fatalf("exit %d\n%s", c, out)
	}
	if _, err := os.Stat(old); err == nil {
		t.Fatal("old transcript kept")
	}
	if _, err := os.Stat(filepath.Join(pd, "old")); err == nil {
		t.Fatal("its subagent folder kept")
	}
}

func TestCleanFlags(t *testing.T) {
	if _, c := runOut(t, "clean", "--force"); c != exitUsage {
		t.Errorf("--force without a name: exit %d, want usage", c)
	}
	if _, c := runOut(t, "clean", "--older-than", "soon"); c != exitUsage {
		t.Errorf("bad age: exit %d", c)
	}
	for in, want := range map[string]time.Duration{"7d": 7 * 24 * time.Hour, "2w": 14 * 24 * time.Hour, "12h": 12 * time.Hour, "1.5d": 36 * time.Hour} {
		if got, err := parseAge(in); err != nil || got != want {
			t.Errorf("parseAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
}

func TestCleanFinding(t *testing.T) {
	root, _ := cleanRepo(t, 11)
	refreshed := ""
	old := refreshClean
	refreshClean = func(r string) { refreshed = r }
	t.Cleanup(func() { refreshClean = old })
	fs := cleanFinding(root)
	if len(fs) != 1 || fs[0].Rule != "clean/worktrees" || fs[0].Fix == nil || fs[0].Fix.Safe || !strings.Contains(fs[0].Message, "11 agent worktrees") {
		t.Fatalf("got %+v", fs)
	}
	if refreshed != root {
		t.Errorf("no background refresh of the summary: %q", refreshed)
	}
	// With a fresh summary the finding gives the size.
	_ = os.MkdirAll(filepath.Join(root, ".tacet"), 0o750)
	if out, c := runOut(t, "-C", root, "clean", "--summary"); c != exitOK {
		t.Fatalf("exit %d\n%s", c, out)
	}
	fs = cleanFinding(root)
	if len(fs) != 1 || !strings.Contains(fs[0].Message, "(11 safe to remove); run tacet clean") {
		t.Fatalf("got %+v", fs)
	}
	// /tacet's yes runs tacet clean --yes.
	if out, c := runOut(t, "-C", root, "apply", cleanFixID); c != exitOK || !strings.Contains(out, "removed 11 worktrees") {
		t.Fatalf("exit %d\n%s", c, out)
	}
	if fs := cleanFinding(root); fs != nil {
		t.Fatalf("still suggested after cleaning: %+v", fs)
	}
}

func TestCleanFindingQuietBelowThresholds(t *testing.T) {
	root, _ := cleanRepo(t, 3)
	old := refreshClean
	refreshClean = func(string) {}
	t.Cleanup(func() { refreshClean = old })
	if fs := cleanFinding(root); fs != nil {
		t.Fatalf("3 small worktrees should not be a finding: %+v", fs)
	}
}
