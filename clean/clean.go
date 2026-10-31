// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package clean

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Why a worktree is kept. A worktree with no reason is safe to remove.
const (
	ReasonChanges   = "uncommitted changes"
	ReasonUntracked = "untracked files"
	ReasonUnique    = "unique commits"
	ReasonLocked    = "locked"
	ReasonHere      = "current directory"
	ReasonRecent    = "changed in the last hour"
	ReasonNewer     = "newer than --older-than"
	ReasonUnchecked = "could not check"
	ReasonOther     = "not an agent worktree"
)

// recent is how long after its last change a worktree may still be in
// use by a running agent, so it is kept whatever its state.
const recent = time.Hour

// forceable are the reasons --force overrides for a worktree the person
// named. Locks, the current directory and recent use still keep it.
var forceable = []string{ReasonChanges, ReasonUntracked, ReasonUnique, ReasonNewer, ReasonIgnored}

// Options tunes Inspect and Remove.
type Options struct {
	// OlderThan keeps worktrees changed more recently than this.
	OlderThan time.Duration
	// Rescue copies a worktree's unique ignored files into the main
	// checkout's .tacet/rescued before removing it, so they no longer
	// keep it.
	Rescue bool
	// Named are worktrees the person named by path, folder or branch.
	// A named worktree outside the agent patterns becomes a candidate.
	Named []string
	// Force removes named worktrees with git worktree remove --force
	// even with uncommitted changes. It never applies to unnamed ones.
	Force bool
	// SizeLimit caps the time spent sizing one worktree; past it the
	// size is a lower bound. 0 means 20 seconds.
	SizeLimit time.Duration
	// Workers size this many worktrees at once; 0 means 4.
	Workers int
	// Progress, when set, hears about sizing as it goes.
	Progress func(Progress)
	// Now is the time ages are measured from; zero means time.Now().
	Now time.Time
	// Cwd is never removed, nor anything that contains it; "" means
	// the process's working directory.
	Cwd string
	// NoSize skips sizing, for a quick count.
	NoSize bool
}

// Progress is one update while worktrees are sized.
type Progress struct {
	Done, Total int
	Path        string
	Bytes       int64
}

// Worktree is one worktree and what tacet found about it.
type Worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	Head   string `json:"head,omitempty"`
	Agent  bool   `json:"agent"`
	Named  bool   `json:"named,omitempty"`
	Stale  bool   `json:"stale,omitempty"`
	Locked bool   `json:"locked,omitempty"`
	// DeadLock is a lock Claude Code took for an agent whose process
	// has exited; Remove unlocks such a worktree before removing it.
	DeadLock   bool `json:"lock_of_exited_agent,omitempty"`
	lockReason string
	// Bytes is the disk space removal frees: files with a single link.
	Bytes int64 `json:"bytes"`
	// Shared is space in files hard-linked elsewhere, such as a pnpm
	// store, which removal may not free.
	Shared int64 `json:"shared_bytes,omitempty"`
	// Capped means sizing ran out of time; Bytes is a lower bound.
	Capped     bool      `json:"size_capped,omitempty"`
	Modified   time.Time `json:"modified,omitzero"`
	LastCommit time.Time `json:"last_commit,omitzero"`
	Changes    int       `json:"uncommitted,omitempty"`
	Untracked  int       `json:"untracked,omitempty"`
	Unique     int       `json:"unique_commits,omitempty"`
	// Ignored are git-ignored files the main checkout lacks or has with
	// other content; Rescued means Remove copies them out first.
	Ignored []string `json:"ignored_files,omitempty"`
	Rescued bool     `json:"rescue,omitempty"`
	Safe    bool     `json:"safe"`
	Forced  bool     `json:"forced,omitempty"`
	Reasons []string `json:"kept_because,omitempty"`

	Removed       bool   `json:"removed,omitempty"`
	BranchDeleted bool   `json:"branch_deleted,omitempty"`
	BranchKept    string `json:"branch_kept,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Remove reports whether Remove would take the worktree away.
func (w *Worktree) Remove() bool { return w.Safe || w.Forced }

// Repo is one repository's worktrees.
type Repo struct {
	Root      string      `json:"root"`
	Worktrees []*Worktree `json:"worktrees"`
	Stale     []*Worktree `json:"stale,omitempty"`
	Other     []*Worktree `json:"other,omitempty"`
	Caches    []Cache     `json:"caches,omitempty"`
	Pruned    bool        `json:"pruned,omitempty"`
}

// ErrMain means the person named the main worktree.
var ErrMain = errors.New("the main worktree is never removed")

// List reads the repository's worktrees and sorts them into agent
// candidates, stale entries and other worktrees, without checking any.
func List(dir string, o Options) (*Repo, error) {
	out, err := git(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	es := ParsePorcelain(out)
	if len(es) == 0 {
		return nil, fmt.Errorf("%s: no worktrees listed", dir)
	}
	r := &Repo{Root: filepath.Clean(es[0].Path)}
	for _, e := range es[1:] {
		w := &Worktree{Path: filepath.Clean(e.Path), Branch: e.Branch, Head: e.Head, Locked: e.Locked}
		w.DeadLock, w.lockReason = e.Locked && deadAgentLock(e.LockReason), e.LockReason
		w.Agent = IsAgent(w.Path, w.Branch)
		w.Named = named(o.Named, w)
		if _, err := os.Stat(w.Path); e.Prunable || errors.Is(err, os.ErrNotExist) {
			w.Stale = true
			r.Stale = append(r.Stale, w)
			continue
		}
		if w.Agent || w.Named {
			r.Worktrees = append(r.Worktrees, w)
		} else {
			r.Other = append(r.Other, w)
		}
	}
	for _, n := range o.Named {
		if matches(n, r.Root, "") {
			return nil, fmt.Errorf("%s: %w", n, ErrMain)
		}
	}
	return r, nil
}

// Inspect lists the repository's worktrees and checks each candidate:
// its size, age, uncommitted changes and commits found on no other
// branch. Only a clean worktree with no unique commits is Safe.
func Inspect(dir string, o Options) (*Repo, error) {
	r, err := List(dir, o)
	if err != nil {
		return nil, err
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	cwd := o.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if c, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = c
	}
	// Stamps first: git status may refresh the index and change them.
	for _, w := range r.Worktrees {
		w.Modified = stamps(w.Path)
	}
	for _, w := range r.Worktrees {
		r.check(w, cwd)
	}
	if !o.NoSize {
		sizeAll(r.Worktrees, o)
	}
	for _, w := range r.Worktrees {
		act := w.Modified
		if w.LastCommit.After(act) {
			act = w.LastCommit
		}
		switch {
		case now.Sub(act) < recent:
			w.Reasons = append(w.Reasons, ReasonRecent)
		case o.OlderThan > 0 && now.Sub(act) < o.OlderThan:
			w.Reasons = append(w.Reasons, ReasonNewer)
		}
		if o.Rescue && len(w.Ignored) > 0 && slices.Contains(w.Reasons, ReasonIgnored) {
			w.Reasons = slices.DeleteFunc(w.Reasons, func(x string) bool { return x == ReasonIgnored })
			w.Rescued = true
		}
		w.Safe = len(w.Reasons) == 0
		w.Forced = !w.Safe && w.Named && o.Force && (!w.Locked || w.DeadLock) && allForceable(w.Reasons)
	}
	for _, w := range r.Stale {
		switch {
		case w.Locked && !w.DeadLock:
			w.Reasons = []string{ReasonLocked}
		case !w.Agent && !w.Named:
			w.Reasons = []string{ReasonOther}
		}
		w.Safe = len(w.Reasons) == 0
	}
	r.Caches = Caches(r.Root, now)
	return r, nil
}

func allForceable(rs []string) bool {
	for _, x := range rs {
		if !slices.Contains(forceable, x) {
			return false
		}
	}
	return true
}

// check fills in the git state of one candidate.
func (r *Repo) check(w *Worktree, cwd string) {
	if w.Locked && !w.DeadLock {
		w.Reasons = append(w.Reasons, ReasonLocked)
	}
	real, err := filepath.EvalSymlinks(w.Path)
	if err != nil {
		real = w.Path
	}
	if within(cwd, real) || within(cwd, w.Path) {
		w.Reasons = append(w.Reasons, ReasonHere)
	}
	if w.Head != "" {
		if out, err := git(r.Root, "log", "-1", "--format=%ct", w.Head); err == nil {
			if sec, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64); err == nil {
				w.LastCommit = time.Unix(sec, 0)
			}
		}
	}
	ok := true
	out, err := git(w.Path, "status", "--porcelain", "--untracked-files=normal", "--ignore-submodules=none")
	if err != nil {
		ok = false
	}
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "??"):
			w.Untracked++
		case strings.TrimSpace(l) != "":
			w.Changes++
		}
	}
	if w.Changes > 0 {
		w.Reasons = append(w.Reasons, ReasonChanges)
	}
	if w.Untracked > 0 {
		w.Reasons = append(w.Reasons, ReasonUntracked)
	}
	if files, iok := ignoredUnique(w.Path, r.Root); !iok {
		ok = false
	} else if w.Ignored = files; len(files) > 0 {
		w.Reasons = append(w.Reasons, ReasonIgnored)
	}
	n, err := r.unique(w)
	if err != nil {
		ok = false
	}
	if w.Unique = n; n > 0 {
		w.Reasons = append(w.Reasons, ReasonUnique)
	}
	if !ok {
		w.Reasons = append(w.Reasons, ReasonUnchecked)
	}
}

// unique counts the commits on the worktree's branch, or its detached
// HEAD, that no other local branch and no remote-tracking branch has.
func (r *Repo) unique(w *Worktree) (int, error) {
	args := []string{"rev-list", "--count"}
	switch {
	case w.Branch != "":
		args = append(args, "refs/heads/"+w.Branch, "--not", "--exclude="+w.Branch, "--branches", "--remotes")
	case w.Head != "":
		args = append(args, w.Head, "--not", "--branches", "--remotes")
	default:
		return 0, errors.New("no HEAD")
	}
	out, err := git(r.Root, args...)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

// Remove takes away every Safe worktree and the Forced ones, with git
// worktree remove; deletes each agent branch only when git branch -d
// agrees it is merged; then prunes stale entries. It returns the bytes
// freed.
func (r *Repo) Remove() int64 {
	var freed int64
	for _, w := range r.Worktrees {
		if !w.Remove() {
			continue
		}
		if w.Rescued {
			if err := r.rescue(w); err != nil {
				w.Error = "rescue failed, kept: " + err.Error()
				continue
			}
		}
		if !r.unlock(w) {
			continue
		}
		args := []string{"worktree", "remove"}
		if w.Forced {
			args = append(args, "--force")
		}
		if _, err := git(r.Root, append(args, w.Path)...); err != nil {
			w.Error = err.Error()
			r.relock(w)
			continue
		}
		w.Removed = true
		freed += w.Bytes
		r.deleteBranch(w)
	}
	prune := true
	for _, w := range r.Stale {
		if !w.Safe {
			// git worktree prune would drop this entry too; locked ones
			// it leaves, so only an unnamed other entry stops it.
			prune = prune && w.Locked
			continue
		}
		if !r.unlock(w) {
			continue
		}
		if _, err := git(r.Root, "worktree", "remove", w.Path); err != nil {
			w.Error = err.Error()
			r.relock(w)
			continue
		}
		w.Removed = true
		r.deleteBranch(w)
	}
	if prune {
		if _, err := git(r.Root, "worktree", "prune"); err == nil {
			r.Pruned = true
		}
	}
	return freed
}

// unlock lifts the lock of an agent that has exited, the one lock tacet
// ever lifts; it reports whether the worktree may now be removed.
func (r *Repo) unlock(w *Worktree) bool {
	if !w.Locked {
		return true
	}
	if !w.DeadLock {
		return false
	}
	if _, err := git(r.Root, "worktree", "unlock", w.Path); err != nil {
		w.Error = err.Error()
		return false
	}
	return true
}

// relock puts back a lock unlock lifted when the removal then failed.
func (r *Repo) relock(w *Worktree) {
	if w.DeadLock {
		_, _ = git(r.Root, "worktree", "lock", "--reason", w.lockReason, w.Path)
	}
}

// agentLockRe is the reason Claude Code gives the lock it holds on an
// agent's worktree while the agent runs.
var agentLockRe = regexp.MustCompile(`^claude agent \S+ \(pid (\d+)\)$`)

// deadAgentLock reports whether a lock reason is Claude Code's and the
// process that took it is gone.
func deadAgentLock(reason string) bool {
	m := agentLockRe.FindStringSubmatch(strings.TrimSpace(reason))
	if m == nil {
		return false
	}
	pid, err := strconv.Atoi(m[1])
	return err == nil && pid > 0 && !alive(pid)
}

// deleteBranch deletes the branch of a removed worktree when Claude
// Code named it, or the person named the worktree and only through
// git branch -d, which refuses a branch that is not fully merged.
func (r *Repo) deleteBranch(w *Worktree) {
	if w.Branch == "" || (!AgentBranch(w.Branch) && !w.Named) {
		return
	}
	if _, err := git(r.Root, "branch", "-d", w.Branch); err != nil {
		w.BranchKept = "not fully merged"
		return
	}
	w.BranchDeleted = true
}

// named reports whether the person named w: by path, folder name or
// branch.
func named(names []string, w *Worktree) bool {
	for _, n := range names {
		if matches(n, w.Path, w.Branch) {
			return true
		}
	}
	return false
}

func matches(name, path, branch string) bool {
	if name == "" {
		return false
	}
	if branch != "" && name == branch {
		return true
	}
	if !strings.ContainsAny(name, `/\`) {
		return name == filepath.Base(path)
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return false
	}
	return filepath.Clean(abs) == filepath.Clean(path)
}

// within reports whether p is dir or inside it.
func within(p, dir string) bool {
	if p == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// stamps is the newest of the times git touches when a worktree is
// used: the folder and its index, HEAD and HEAD log in the git dir.
func stamps(path string) time.Time {
	var t time.Time
	see := func(p string) {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().After(t) { // #nosec G703 -- paths of a listed worktree and its git dir
			t = fi.ModTime()
		}
	}
	see(path)
	b, err := os.ReadFile(filepath.Join(path, ".git")) // #nosec G304 -- the .git file of a listed worktree
	if err != nil {
		return t
	}
	gd, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
	if !ok {
		return t
	}
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(path, gd)
	}
	for _, f := range []string{"index", "HEAD", filepath.Join("logs", "HEAD")} {
		see(filepath.Join(gd, f))
	}
	return t
}

// sizeAll sizes the worktrees, a few at a time.
func sizeAll(ws []*Worktree, o Options) {
	limit := o.SizeLimit
	if limit <= 0 {
		limit = 20 * time.Second
	}
	workers := o.Workers
	if workers <= 0 {
		workers = 4
	}
	var mu sync.Mutex
	done := 0
	report := func(p string, n int64, finished bool) {
		if o.Progress == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if finished {
			done++
		}
		o.Progress(Progress{Done: done, Total: len(ws), Path: p, Bytes: n})
	}
	jobs := make(chan *Worktree)
	var wg sync.WaitGroup
	for range min(workers, max(len(ws), 1)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range jobs {
				s := Measure(w.Path, limit, func(n int64) { report(w.Path, n, false) })
				w.Bytes, w.Shared, w.Capped = s.Bytes, s.Shared, s.Capped
				if s.Newest.After(w.Modified) {
					w.Modified = s.Newest
				}
				report(w.Path, s.Bytes+s.Shared, true)
			}
		}()
	}
	for _, w := range ws {
		jobs <- w
	}
	close(jobs)
	wg.Wait()
}

// git runs one git command in dir. Optional locks are off so reading a
// worktree's status never rewrites its index.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- fixed git subcommands
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return string(out), fmt.Errorf("git %s: %s", args[0], msg)
	}
	return string(out), nil
}
