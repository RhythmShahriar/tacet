// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package clean

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// later is past the one-hour in-use guard for worktrees made in a test.
func later() Options {
	return Options{Now: time.Now().Add(2 * time.Hour), Cwd: os.TempDir(), SizeLimit: 5 * time.Second}
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// repo is a temp repository with one commit on main.
func repo(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run(t, root, "init", "-q", "-b", "main")
	write(t, filepath.Join(root, "a.txt"), "a\n")
	write(t, filepath.Join(root, ".gitignore"), "node_modules/\n")
	run(t, root, "add", ".")
	run(t, root, "commit", "-q", "-m", "init")
	return root
}

// agent adds an agent worktree the way Claude Code does.
func agent(t *testing.T, root, id string) string {
	t.Helper()
	p := filepath.Join(root, ".claude", "worktrees", "agent-"+id)
	run(t, root, "worktree", "add", "-q", p, "-b", "worktree-agent-"+id)
	return p
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func branchExists(t *testing.T, root, b string) bool {
	t.Helper()
	return strings.TrimSpace(run(t, root, "branch", "--list", b)) != ""
}

func find(r *Repo, path string) *Worktree {
	for _, w := range append(append(append([]*Worktree{}, r.Worktrees...), r.Stale...), r.Other...) {
		if w.Path == path {
			return w
		}
	}
	return nil
}

func inspect(t *testing.T, root string, o Options) *Repo {
	t.Helper()
	r, err := Inspect(root, o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCleanMergedAgentWorktreeIsRemoved(t *testing.T) {
	root := repo(t)
	p := agent(t, root, "a1")
	write(t, filepath.Join(p, "node_modules", "big.js"), strings.Repeat("x", 64<<10)) // ignored: not work
	r := inspect(t, root, later())
	w := find(r, p)
	if w == nil || !w.Safe || !w.Agent {
		t.Fatalf("want a safe agent worktree, got %+v", w)
	}
	if w.Bytes < 64<<10 {
		t.Errorf("size %d, want at least the 64 KB file", w.Bytes)
	}
	r.Remove()
	if exists(p) || !w.Removed {
		t.Fatalf("worktree still there: %+v", w)
	}
	if !w.BranchDeleted || branchExists(t, root, "worktree-agent-a1") {
		t.Errorf("merged agent branch should be deleted: %+v", w)
	}
	if !exists(filepath.Join(root, "a.txt")) {
		t.Fatal("main worktree touched")
	}
}

func TestKeptWorktrees(t *testing.T) {
	root := repo(t)
	dirty := agent(t, root, "dirty")
	write(t, filepath.Join(dirty, "a.txt"), "changed\n")
	untracked := agent(t, root, "untracked")
	write(t, filepath.Join(untracked, "new.txt"), "new\n")
	unique := agent(t, root, "unique")
	write(t, filepath.Join(unique, "b.txt"), "b\n")
	run(t, unique, "add", "b.txt")
	run(t, unique, "commit", "-q", "-m", "unpushed work")
	locked := agent(t, root, "locked")
	run(t, root, "worktree", "lock", locked)

	r := inspect(t, root, later())
	cases := map[string]string{dirty: ReasonChanges, untracked: ReasonUntracked, unique: ReasonUnique, locked: ReasonLocked}
	for p, why := range cases {
		w := find(r, p)
		if w == nil || w.Safe || !slices.Contains(w.Reasons, why) {
			t.Errorf("%s: want kept for %q, got %+v", filepath.Base(p), why, w)
		}
	}
	r.Remove()
	for p := range cases {
		if !exists(p) {
			t.Errorf("%s was removed", filepath.Base(p))
		}
	}
	if !branchExists(t, root, "worktree-agent-unique") {
		t.Error("branch with unique commits deleted")
	}
	if b, _ := os.ReadFile(filepath.Join(dirty, "a.txt")); string(b) != "changed\n" {
		t.Error("uncommitted change lost")
	}
}

func TestNonAgentWorktreeUntouched(t *testing.T) {
	root := repo(t)
	other := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-feature")
	run(t, root, "worktree", "add", "-q", other, "-b", "feature")
	t.Cleanup(func() { _ = os.RemoveAll(other) })
	r := inspect(t, root, later())
	if w := find(r, other); w == nil || !slices.Contains(r.Other, w) {
		t.Fatalf("want it under other worktrees, got %+v", w)
	}
	r.Remove()
	if !exists(other) || !branchExists(t, root, "feature") {
		t.Fatal("non-agent worktree touched")
	}
	// Named, it becomes a candidate and goes when it is safe.
	o := later()
	o.Named = []string{other}
	r = inspect(t, root, o)
	w := find(r, other)
	if w == nil || !w.Named || !w.Safe {
		t.Fatalf("named clean worktree should be safe: %+v", w)
	}
	r.Remove()
	if exists(other) {
		t.Fatal("named safe worktree not removed")
	}
}

func TestStaleEntryPruned(t *testing.T) {
	root := repo(t)
	p := agent(t, root, "gone")
	if err := os.RemoveAll(p); err != nil {
		t.Fatal(err)
	}
	r := inspect(t, root, later())
	w := find(r, p)
	if w == nil || !w.Stale || !w.Safe {
		t.Fatalf("want a safe stale entry, got %+v", w)
	}
	r.Remove()
	if strings.Contains(run(t, root, "worktree", "list", "--porcelain"), p) {
		t.Fatal("stale entry not pruned")
	}
	if !r.Pruned {
		t.Error("git worktree prune did not run")
	}
}

func TestStaleOtherEntryBlocksPrune(t *testing.T) {
	root := repo(t)
	other := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-gone")
	run(t, root, "worktree", "add", "-q", other, "-b", "feature")
	if err := os.RemoveAll(other); err != nil {
		t.Fatal(err)
	}
	r := inspect(t, root, later())
	r.Remove()
	if r.Pruned || !strings.Contains(run(t, root, "worktree", "list", "--porcelain"), other) {
		t.Fatal("a stale entry outside the agent patterns was pruned")
	}
}

func TestMainWorktreeNeverTouched(t *testing.T) {
	root := repo(t)
	agent(t, root, "m1")
	o := later()
	o.Named = []string{root}
	o.Force = true
	if _, err := Inspect(root, o); !errors.Is(err, ErrMain) {
		t.Fatalf("naming the main worktree: got %v, want ErrMain", err)
	}
	// Run from inside an agent worktree, the main one is still not a
	// candidate and the current one is kept.
	p := filepath.Join(root, ".claude", "worktrees", "agent-m1")
	o = later()
	o.Cwd = p
	r := inspect(t, p, o)
	if r.Root != root || find(r, root) != nil {
		t.Fatalf("main worktree listed as a candidate: %+v", r)
	}
	if w := find(r, p); w == nil || w.Safe || !slices.Contains(w.Reasons, ReasonHere) {
		t.Fatalf("the current worktree must be kept: %+v", w)
	}
	r.Remove()
	if !exists(filepath.Join(root, "a.txt")) || !exists(p) {
		t.Fatal("removed the main or the current worktree")
	}
}

func TestOlderThanAndRecent(t *testing.T) {
	root := repo(t)
	p := agent(t, root, "age")
	o := Options{Now: time.Now(), Cwd: os.TempDir()}
	if w := find(inspect(t, root, o), p); w.Safe || !slices.Contains(w.Reasons, ReasonRecent) {
		t.Fatalf("a worktree changed minutes ago must be kept: %+v", w)
	}
	o.Now, o.OlderThan = time.Now().Add(48*time.Hour), 7*24*time.Hour
	if w := find(inspect(t, root, o), p); w.Safe || !slices.Contains(w.Reasons, ReasonNewer) {
		t.Fatalf("2 days old with --older-than 7d must be kept: %+v", w)
	}
	o.Now = time.Now().Add(8 * 24 * time.Hour)
	if w := find(inspect(t, root, o), p); !w.Safe {
		t.Fatalf("8 days old with --older-than 7d should be safe: %+v", w)
	}
}

func TestBranchDeletedOnlyWhenMerged(t *testing.T) {
	root := repo(t)
	p := agent(t, root, "side")
	write(t, filepath.Join(p, "c.txt"), "c\n")
	run(t, p, "add", "c.txt")
	run(t, p, "commit", "-q", "-m", "side work")
	// Another branch holds the commit, so nothing is unique but main
	// does not have it: the worktree goes and the branch stays.
	run(t, root, "branch", "keep-side", "worktree-agent-side")
	r := inspect(t, root, later())
	w := find(r, p)
	if !w.Safe {
		t.Fatalf("no unique commits and clean: want safe, got %+v", w)
	}
	r.Remove()
	if !w.Removed || w.BranchDeleted || !branchExists(t, root, "worktree-agent-side") {
		t.Fatalf("unmerged branch deleted: %+v", w)
	}
}

func TestHumanBranchNotDeleted(t *testing.T) {
	root := repo(t)
	p := filepath.Join(root, ".claude", "worktrees", "agent-feat")
	run(t, root, "worktree", "add", "-q", p, "-b", "feat/thing")
	r := inspect(t, root, later())
	r.Remove()
	if exists(p) || !branchExists(t, root, "feat/thing") {
		t.Fatal("want the worktree removed and the branch an agent checked out kept")
	}
}

func TestForceOnlyForNamed(t *testing.T) {
	root := repo(t)
	a := agent(t, root, "fa")
	b := agent(t, root, "fb")
	write(t, filepath.Join(a, "a.txt"), "dirty\n")
	write(t, filepath.Join(b, "a.txt"), "dirty\n")
	o := later()
	o.Force, o.Named = true, []string{"agent-fa"}
	r := inspect(t, root, o)
	r.Remove()
	if exists(a) {
		t.Error("named worktree with --force not removed")
	}
	if !exists(b) {
		t.Error("--force removed a worktree that was not named")
	}
}

func TestDetachedUnique(t *testing.T) {
	root := repo(t)
	p := filepath.Join(root, ".claude", "worktrees", "agent-det")
	run(t, root, "worktree", "add", "-q", "--detach", p)
	write(t, filepath.Join(p, "d.txt"), "d\n")
	run(t, p, "add", "d.txt")
	run(t, p, "commit", "-q", "-m", "detached work")
	w := find(inspect(t, root, later()), p)
	if w.Safe || w.Unique != 1 {
		t.Fatalf("detached HEAD with a commit on no branch must be kept: %+v", w)
	}
}

func TestCaches(t *testing.T) {
	root := repo(t)
	now := time.Now()
	old := filepath.Join(root, ".tacet", "out", "old.txt")
	fresh := filepath.Join(root, ".tacet", "out", "fresh.txt")
	write(t, old, "x")
	write(t, fresh, "x")
	past := now.Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "kept.pdf"), "pdf")
	keptDoc := filepath.Join(root, ".tacet", "docs", "kept-abc.md")
	write(t, keptDoc, "<!-- tacet converted kept.pdf to Markdown to save tokens. -->\n\nbody\n")
	goneDoc := filepath.Join(root, ".tacet", "docs", "gone-abc.md")
	write(t, goneDoc, "<!-- tacet converted docs/gone file.pdf to Markdown to save tokens. -->\n\nbody\n")
	goneOutline := filepath.Join(root, ".tacet", "docs", "gone-abc.outline.md")
	write(t, goneOutline, "outline")
	goneTalk := filepath.Join(root, ".tacet", "docs", "talk-abc.transcript.md")
	write(t, goneTalk, "<!-- tacet transcribed talk.mp3 (language en) with whisper.cpp. -->\n")
	scanned := filepath.Join(root, ".tacet", "docs", "scan-abc.md")
	write(t, scanned, "")
	reads := filepath.Join(root, ".tacet", "reads", "s1-pdf.json")
	write(t, reads, "{}")
	config := filepath.Join(root, ".tacet", "config.yaml")
	write(t, config, "x")

	cs := Caches(root, now)
	var got []string
	for _, c := range cs {
		got = append(got, c.Path)
	}
	want := []string{old, goneDoc, goneTalk, goneOutline, reads}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("caches:\n got %v\nwant %v", got, want)
	}
	if _, err := RemoveCaches(cs); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{fresh, keptDoc, scanned, config} {
		if !exists(p) {
			t.Errorf("%s removed", p)
		}
	}
}

func TestTranscripts(t *testing.T) {
	pd := t.TempDir()
	now := time.Now()
	past := now.Add(-100 * 24 * time.Hour)
	old := filepath.Join(pd, "old.jsonl")
	write(t, old, `{"cwd":"/x"}`+"\n")
	sub := filepath.Join(pd, "old", "subagents", "agent-1.jsonl")
	write(t, sub, "{}\n")
	newer := filepath.Join(pd, "new.jsonl")
	write(t, newer, "{}\n")
	mem := filepath.Join(pd, "memory", "MEMORY.md")
	write(t, mem, "keep")
	for _, p := range []string{old, sub, filepath.Join(pd, "old", "subagents"), filepath.Join(pd, "old"), mem} {
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
	}
	ts := OldTranscripts(pd, 90*24*time.Hour, now)
	if len(ts) != 1 || ts[0].Path != old || ts[0].Folder != filepath.Join(pd, "old") {
		t.Fatalf("got %+v", ts)
	}
	if _, err := RemoveTranscripts(ts); err != nil {
		t.Fatal(err)
	}
	if exists(old) || exists(sub) || !exists(newer) || !exists(mem) {
		t.Fatal("wrong files deleted")
	}
}

func TestReposFromTranscriptCwd(t *testing.T) {
	root := repo(t)
	p := agent(t, root, "r1")
	home := t.TempDir()
	// The folder name decodes to nothing real; the cwd inside wins. A
	// session run in an agent worktree resolves to the same repo.
	write(t, filepath.Join(home, "projects", "-no-such-dir", "s.jsonl"), `{"type":"user","cwd":"`+root+`"}`+"\n")
	write(t, filepath.Join(home, "projects", "-other", "s.jsonl"), `{"type":"x"}`+"\n"+`{"cwd":"`+p+`"}`+"\n")
	got := Repos(home)
	if len(got) != 1 || got[0] != root {
		t.Fatalf("got %v, want [%s]", got, root)
	}
}

func TestSummary(t *testing.T) {
	root := repo(t)
	agent(t, root, "s1")
	r := inspect(t, root, later())
	s := r.Summarize(time.Now())
	if err := SaveSummary(root, s); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadSummary(root, time.Now()); ok {
		t.Fatal("saved a summary without a .tacet folder")
	}
	if err := os.Mkdir(filepath.Join(root, ".tacet"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := SaveSummary(root, s); err != nil {
		t.Fatal(err)
	}
	got, ok := LoadSummary(root, time.Now())
	if !ok || got.Agent != 1 || got.Safe != 1 {
		t.Fatalf("got %+v %v", got, ok)
	}
	if _, ok := LoadSummary(root, time.Now().Add(25*time.Hour)); ok {
		t.Fatal("a day-old summary should be stale")
	}
	if main, n, err := Count(root); err != nil || n != 1 || main != root {
		t.Fatalf("Count: %s %d %v", main, n, err)
	}
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 999: "999 B", 1500: "1.5 KB", 32_100_000_000: "32.1 GB"} {
		if got := HumanBytes(n); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestAgentLocks(t *testing.T) {
	root := repo(t)
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Skip("no true command")
	}
	dead := agent(t, root, "dead")
	run(t, root, "worktree", "lock", "--reason", "claude agent agent-dead (pid "+strconv.Itoa(gone.Process.Pid)+")", dead)
	live := agent(t, root, "live")
	run(t, root, "worktree", "lock", "--reason", "claude agent agent-live (pid "+strconv.Itoa(os.Getpid())+")", live)
	mine := agent(t, root, "mine")
	run(t, root, "worktree", "lock", "--reason", "on a usb disk", mine)
	r := inspect(t, root, later())
	if w := find(r, dead); !w.Safe || !w.DeadLock {
		t.Fatalf("lock of an exited agent: want safe, got %+v", w)
	}
	for _, p := range []string{live, mine} {
		if w := find(r, p); w.Safe || !slices.Contains(w.Reasons, ReasonLocked) {
			t.Fatalf("%s: want kept as locked, got %+v", filepath.Base(p), w)
		}
	}
	r.Remove()
	if exists(dead) || !exists(live) || !exists(mine) {
		t.Fatal("want only the worktree of the exited agent removed")
	}
}

// ignoredRepo is a repo whose .gitignore also hides local env files and
// test logins, the kinds of files agents leave in ignored folders.
func ignoredRepo(t *testing.T) string {
	t.Helper()
	root := repo(t)
	write(t, filepath.Join(root, ".gitignore"), "node_modules/\n.env.local\ne2e/.auth/\n.next/\n")
	run(t, root, "commit", "-q", "-am", "ignore more")
	return root
}

func TestIgnoredFilesKeepAWorktree(t *testing.T) {
	root := ignoredRepo(t)
	p := agent(t, root, "i1")
	write(t, filepath.Join(p, "e2e", ".auth", "admin-totp.txt"), "JBSWY3DPEHPK3PXP\n")
	write(t, filepath.Join(p, "e2e", ".auth", "gate.py"), "print('only here')\n")
	r := inspect(t, root, later())
	w := find(r, p)
	if w.Safe || !slices.Contains(w.Reasons, ReasonIgnored) {
		t.Fatalf("ignored files only in the worktree must keep it: %+v", w)
	}
	if len(w.Ignored) != 2 {
		t.Fatalf("ignored files %v", w.Ignored)
	}
	r.Remove()
	if !exists(p) {
		t.Fatal("a kept worktree was removed")
	}
}

func TestIgnoredFilesSameAsMainOrRebuildable(t *testing.T) {
	root := ignoredRepo(t)
	write(t, filepath.Join(root, ".env.local"), "A=1\n")
	p := agent(t, root, "i2")
	write(t, filepath.Join(p, ".env.local"), "A=1\n")                        // the same as the main checkout
	write(t, filepath.Join(p, "node_modules", "x", "index.js"), "x\n")       // rebuildable
	write(t, filepath.Join(p, ".next", "cache", "build.json"), "{}\n")       // rebuildable
	write(t, filepath.Join(p, "app.tsbuildinfo"), "{}\n")                    // rebuildable and untracked
	run(t, p, "rm", "-q", "--cached", "--ignore-unmatch", "app.tsbuildinfo") // stays untracked
	os.Remove(filepath.Join(p, "app.tsbuildinfo"))
	r := inspect(t, root, later())
	if w := find(r, p); !w.Safe || len(w.Ignored) != 0 {
		t.Fatalf("only rebuildable or identical ignored files: want safe, got %+v", w)
	}
}

func TestIgnoredEnvThatDiffersKeeps(t *testing.T) {
	root := ignoredRepo(t)
	write(t, filepath.Join(root, ".env.local"), "A=1\n")
	p := agent(t, root, "i3")
	write(t, filepath.Join(p, ".env.local"), "A=2\n")
	if w := find(inspect(t, root, later()), p); w.Safe || len(w.Ignored) != 1 || w.Ignored[0] != ".env.local" {
		t.Fatalf("a different .env.local must keep the worktree: %+v", w)
	}
}

func TestRescueCopiesThenRemoves(t *testing.T) {
	root := ignoredRepo(t)
	p := agent(t, root, "i4")
	write(t, filepath.Join(p, "e2e", ".auth", "admin-totp.txt"), "SECRET\n")
	o := later()
	o.Rescue = true
	r := inspect(t, root, o)
	w := find(r, p)
	if !w.Safe || !w.Rescued {
		t.Fatalf("with --rescue the worktree is removable after copying: %+v", w)
	}
	r.Remove()
	if exists(p) {
		t.Fatal("worktree not removed")
	}
	got, err := os.ReadFile(filepath.Join(RescueDir(root, w), "e2e", ".auth", "admin-totp.txt"))
	if err != nil || string(got) != "SECRET\n" {
		t.Fatalf("rescued file: %q %v", got, err)
	}
	if fi, _ := os.Stat(filepath.Join(RescueDir(root, w), "e2e", ".auth", "admin-totp.txt")); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("rescued file mode %v, want the original 0600", fi.Mode().Perm())
	}
	if out := run(t, root, "status", "--porcelain"); strings.Contains(out, ".tacet") {
		t.Fatalf("rescued files show in git status:\n%s", out)
	}
}

func TestRescueFailureKeeps(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs a folder the user can't write")
	}
	root := ignoredRepo(t)
	p := agent(t, root, "i5")
	write(t, filepath.Join(p, "e2e", ".auth", "admin.json"), "{}\n")
	write(t, filepath.Join(root, ".tacet", "keep"), "x")
	if err := os.Chmod(filepath.Join(root, ".tacet"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, ".tacet"), 0o700) })
	o := later()
	o.Rescue = true
	r := inspect(t, root, o)
	r.Remove()
	w := find(r, p)
	if !exists(p) || w.Removed || !strings.Contains(w.Error, "rescue failed") {
		t.Fatalf("a failed rescue must keep the worktree: %+v", w)
	}
}
