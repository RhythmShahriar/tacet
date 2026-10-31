// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/claude"
	"github.com/RhythmShahriar/tacet/clean"
	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/term"
)

// cleanFixID is the /tacet suggestion that runs tacet clean.
const cleanFixID = "clean-worktrees"

// Thresholds for the clean/worktrees finding.
const (
	cleanMinBytes  = 1_000_000_000
	cleanMinCount  = 10
	cleanBigBytes  = 10_000_000_000 // medium from here
	transcriptsAge = 90 * 24 * time.Hour
)

// cleanNow is the clock ages are measured on; tests move it.
var cleanNow = time.Now

type cleanFlags struct {
	all, dry, force, transcripts bool
	summary                      bool // hidden: write .tacet/clean.json and stop
	older                        time.Duration
	names                        []string
}

func parseCleanFlags(args []string) (cleanFlags, error) {
	var f cleanFlags
	for i := 0; i < len(args); i++ {
		switch x := args[i]; x {
		case "--all":
			f.all = true
		case "--dry-run", "-n":
			f.dry = true
		case "--force":
			f.force = true
		case "--transcripts":
			f.transcripts = true
		case "--summary":
			f.summary, f.dry = true, true
		case "--older-than":
			if i+1 >= len(args) {
				return f, fmt.Errorf("%w: --older-than needs an age such as 7d", errUsage)
			}
			d, err := parseAge(args[i+1])
			if err != nil {
				return f, fmt.Errorf("%w: --older-than %s: %w", errUsage, args[i+1], err)
			}
			f.older = d
			i++
		default:
			if strings.HasPrefix(x, "-") {
				return f, fmt.Errorf("%w: tacet clean [--all] [--dry-run] [--older-than 7d] [--transcripts] [--force <worktree>...]", errUsage)
			}
			f.names = append(f.names, x)
		}
	}
	if f.force && len(f.names) == 0 {
		return f, fmt.Errorf("%w: --force needs the worktrees named: tacet clean --force <path>", errUsage)
	}
	return f, nil
}

// parseAge reads 90d, 2w, 12h or any Go duration.
func parseAge(s string) (time.Duration, error) {
	unit := map[byte]time.Duration{'d': 24 * time.Hour, 'w': 7 * 24 * time.Hour}
	if len(s) > 1 {
		if u, ok := unit[s[len(s)-1]]; ok {
			n, err := strconv.ParseFloat(s[:len(s)-1], 64)
			if err != nil || n < 0 {
				return 0, errors.New("use a number of days such as 7d")
			}
			return time.Duration(n * float64(u)), nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, errors.New("use an age such as 7d, 2w or 12h")
	}
	return d, nil
}

// cleanTotals adds up what tacet clean found.
type cleanTotals struct {
	Candidates  int            `json:"candidates"`
	Safe        int            `json:"safe"`
	Forced      int            `json:"forced,omitempty"`
	Kept        int            `json:"kept"`
	KeptBy      map[string]int `json:"kept_by_reason,omitempty"`
	Bytes       int64          `json:"bytes"`
	Shared      int64          `json:"shared_bytes,omitempty"`
	KeptBytes   int64          `json:"kept_bytes"`
	Stale       int            `json:"stale"`
	Other       int            `json:"other"`
	Caches      int            `json:"cache_files"`
	CacheBytes  int64          `json:"cache_bytes"`
	Removed     int            `json:"removed,omitempty"`
	Freed       int64          `json:"freed_bytes,omitempty"`
	Errors      int            `json:"errors,omitempty"`
	CacheFreed  int64          `json:"cache_freed_bytes,omitempty"`
	BranchesDel int            `json:"branches_deleted,omitempty"`
}

type cleanReport struct {
	DryRun bool          `json:"dry_run"`
	Repos  []*clean.Repo `json:"repos"`
	Totals cleanTotals   `json:"totals"`
}

func totals(repos []*clean.Repo) cleanTotals {
	t := cleanTotals{KeptBy: map[string]int{}}
	for _, r := range repos {
		for _, w := range r.Worktrees {
			t.Candidates++
			switch {
			case w.Safe:
				t.Safe++
				t.Bytes += w.Bytes
				t.Shared += w.Shared
			case w.Forced:
				t.Forced++
				t.Bytes += w.Bytes
				t.Shared += w.Shared
			default:
				t.Kept++
				t.KeptBytes += w.Bytes
				for _, why := range w.Reasons {
					t.KeptBy[why]++
				}
			}
			if w.Removed {
				t.Removed++
				t.Freed += w.Bytes
			}
			if w.BranchDeleted {
				t.BranchesDel++
			}
			if w.Error != "" {
				t.Errors++
			}
		}
		t.Stale += len(r.Stale)
		for _, w := range r.Stale {
			if w.BranchDeleted {
				t.BranchesDel++
			}
		}
		t.Other += len(r.Other)
		t.Caches += len(r.Caches)
		for _, c := range r.Caches {
			t.CacheBytes += c.Bytes
		}
	}
	return t
}

// runClean removes the git worktrees Claude Code's agents left behind,
// only those with no uncommitted change and no commit found elsewhere,
// and tacet's own stale caches. With --transcripts it deletes old
// session transcripts instead.
func runClean(a *app, args []string) error {
	f, err := parseCleanFlags(args)
	if err != nil {
		return err
	}
	// Without a terminal or --yes there is no one to confirm: list only.
	dry := f.dry || (!a.yes && (a.json || !interactive()))
	if f.transcripts {
		return cleanTranscripts(a, f, dry)
	}
	var dirs []string
	if f.all {
		dirs = clean.Repos(claude.Home())
	} else {
		dirs = []string{a.root()}
	}
	o := clean.Options{OlderThan: f.older, Named: f.names, Force: f.force, Progress: progressLine(a), Now: cleanNow()}
	var repos []*clean.Repo
	for _, d := range dirs {
		r, err := clean.Inspect(d, o)
		if errors.Is(err, clean.ErrMain) {
			return err
		}
		if err != nil {
			if !f.all {
				return err
			}
			fmt.Fprintf(a.err, "tacet: %s: %v\n", d, err)
			continue
		}
		if f.summary {
			// The scan's background refresh: a dry run that records
			// the totals for the clean/worktrees finding.
			_ = clean.SaveSummary(r.Root, r.Summarize(cleanNow()))
		}
		repos = append(repos, r)
	}
	clearProgress(a)
	if f.summary {
		return nil
	}
	t := totals(repos)
	if !a.json {
		printClean(a.out, repos, t, f.all)
	}
	work := t.Safe + t.Forced + t.Caches
	for _, r := range repos {
		for _, w := range r.Stale {
			if w.Safe {
				work++
			}
		}
	}
	if work == 0 || dry {
		if a.json {
			return a.writeJSON(cleanReport{DryRun: true, Repos: repos, Totals: t})
		}
		switch {
		case work == 0:
			fmt.Fprintln(a.out, ui.Good.Render("nothing to remove"))
		case f.dry:
			fmt.Fprintln(a.out, ui.Dim.Render("dry run: nothing changed"))
		default:
			fmt.Fprintln(a.out, ui.Dim.Render("not a terminal, so nothing changed: rerun with --yes to remove the safe ones"))
		}
		return nil
	}
	if !a.yes {
		ok := false
		title := fmt.Sprintf("Remove %s and %s, freeing about %s?", plural(t.Safe+t.Forced, "worktree"), plural(t.Caches, "cache file"), clean.HumanBytes(t.Bytes+t.CacheBytes))
		if err := huh.NewConfirm().Title(title).Affirmative("Remove").Negative("Keep").Value(&ok).Run(); err != nil && !errors.Is(err, huh.ErrUserAborted) {
			return err
		}
		if !ok {
			fmt.Fprintln(a.out, "nothing changed")
			return nil
		}
	}
	var cacheFreed int64
	for _, r := range repos {
		r.Remove()
		n, err := clean.RemoveCaches(r.Caches)
		cacheFreed += n
		if err != nil {
			fmt.Fprintf(a.err, "tacet: %s: %v\n", r.Root, err)
		}
		left := *r
		left.Worktrees = nil
		for _, w := range r.Worktrees {
			if !w.Removed {
				left.Worktrees = append(left.Worktrees, w)
			}
		}
		_ = clean.SaveSummary(r.Root, left.Summarize(cleanNow()))
	}
	t = totals(repos)
	t.CacheFreed = cacheFreed
	if a.json {
		return a.writeJSON(cleanReport{Repos: repos, Totals: t})
	}
	printRemoved(a.out, repos, t)
	return nil
}

// progressLine shows sizing progress on a terminal's stderr, the
// running total of the tree being walked included, so a big tree is
// visibly moving.
func progressLine(a *app) func(clean.Progress) {
	f, ok := a.err.(*os.File)
	if a.json || !ok || !term.IsTerminal(f.Fd()) {
		return nil
	}
	return func(p clean.Progress) {
		fmt.Fprintf(a.err, "\r\033[K  sizing %d/%d  %s  %s", p.Done, p.Total, clean.HumanBytes(p.Bytes), filepath.Base(p.Path))
	}
}

func clearProgress(a *app) {
	if f, ok := a.err.(*os.File); ok && !a.json && term.IsTerminal(f.Fd()) {
		fmt.Fprint(a.err, "\r\033[K")
	}
}

// shortPath shows a worktree by its folder name when it sits in the
// repo's .claude/worktrees, else by its path.
func shortPath(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return strings.TrimPrefix(filepath.ToSlash(rel), ".claude/worktrees/")
	}
	return p
}

func sizeText(w *clean.Worktree) string {
	s := clean.HumanBytes(w.Bytes)
	if w.Capped {
		s = "over " + s
	}
	return s
}

func reasonText(w *clean.Worktree) string {
	var out []string
	for _, r := range w.Reasons {
		switch r {
		case clean.ReasonChanges:
			r = plural(w.Changes, "uncommitted change")
		case clean.ReasonUntracked:
			r = plural(w.Untracked, "untracked file")
		case clean.ReasonUnique:
			r = plural(w.Unique, "unique commit")
		}
		out = append(out, r)
	}
	return strings.Join(out, ", ")
}

func day(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	return t.Local().Format("2 Jan 2006")
}

func printClean(w io.Writer, repos []*clean.Repo, t cleanTotals, all bool) {
	wd := width()
	for _, r := range repos {
		var safe, kept []*clean.Worktree
		for _, x := range r.Worktrees {
			if x.Remove() {
				safe = append(safe, x)
			} else {
				kept = append(kept, x)
			}
		}
		if all && len(r.Worktrees)+len(r.Stale)+len(r.Caches) == 0 {
			continue
		}
		fmt.Fprintln(w, ui.Label.Render(r.Root))
		var b, sh int64
		for _, x := range safe {
			b += x.Bytes
			sh += x.Shared
		}
		fmt.Fprintf(w, "  %s  %s, %s%s\n", ui.Good.Render("safe to remove"), plural(len(safe), "worktree"), clean.HumanBytes(b), sharedNote(sh))
		if !all {
			for _, x := range safe {
				note := ""
				if x.Forced {
					note = ui.Bad.Render("  forced: " + reasonText(x))
				}
				if x.DeadLock {
					note += ui.Dim.Render("  locked by an agent that exited")
				}
				fmt.Fprintf(w, "    %-26s %-28s %10s  %s%s\n", shortPath(r.Root, x.Path), x.Branch, sizeText(x), ui.Dim.Render("changed "+day(x.Modified)), note)
			}
		}
		if len(kept) > 0 {
			fmt.Fprintf(w, "  %s  %s, %s\n", ui.Label.Render("kept"), plural(len(kept), "worktree"), "nothing in them is touched")
			for _, x := range kept {
				fmt.Fprintf(w, "    %-26s %-28s %10s  %s\n", shortPath(r.Root, x.Path), x.Branch, sizeText(x), ui.Dim.Render(reasonText(x)))
			}
		}
		if len(r.Stale) > 0 {
			fmt.Fprintf(w, "  %s  %s whose folder is gone\n", ui.Label.Render("stale"), plural(len(r.Stale), "entry"))
			for _, x := range r.Stale {
				what := "pruned"
				if !x.Safe {
					what = "kept: " + strings.Join(x.Reasons, ", ")
				}
				fmt.Fprintf(w, "    %-26s %-28s %s\n", shortPath(r.Root, x.Path), x.Branch, ui.Dim.Render(what))
			}
		}
		if len(r.Other) > 0 && !all {
			fmt.Fprintf(w, "  %s  %s, not touched unless you name one\n", ui.Label.Render("other worktrees"), plural(len(r.Other), "worktree"))
			for _, x := range r.Other {
				fmt.Fprintf(w, "    %s  %s\n", x.Path, ui.Dim.Render(x.Branch))
			}
		}
		if len(r.Caches) > 0 {
			var cb int64
			for _, c := range r.Caches {
				cb += c.Bytes
			}
			fmt.Fprintf(w, "  %s  %s, %s\n", ui.Label.Render("tacet caches"), plural(len(r.Caches), "file"), clean.HumanBytes(cb))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, ui.Section("total", -1, wd))
	fmt.Fprintf(w, "  %s in %s: %d safe to remove, %d kept\n", plural(t.Candidates, "agent worktree"), plural(len(repos), "repo"), t.Safe+t.Forced, t.Kept)
	if len(t.KeptBy) > 0 {
		var why []string
		for k, n := range t.KeptBy {
			why = append(why, fmt.Sprintf("%s (%d)", k, n))
		}
		sort.Strings(why)
		fmt.Fprintf(w, "  kept because: %s\n", strings.Join(why, ", "))
	}
	fmt.Fprintf(w, "  removing them frees about %s%s\n", ui.Brand.Render(clean.HumanBytes(t.Bytes)), sharedNote(t.Shared))
	if t.Stale > 0 || t.Other > 0 {
		fmt.Fprintf(w, "  %s, %s left alone\n", plural(t.Stale, "stale entry"), plural(t.Other, "other worktree"))
	}
	fmt.Fprintln(w)
}

func sharedNote(n int64) string {
	if n == 0 {
		return ""
	}
	return ui.Dim.Render(fmt.Sprintf(" (plus %s in hard links shared with a package store, which may stay)", clean.HumanBytes(n)))
}

func printRemoved(w io.Writer, repos []*clean.Repo, t cleanTotals) {
	for _, r := range repos {
		for _, x := range append(append([]*clean.Worktree{}, r.Worktrees...), r.Stale...) {
			switch {
			case x.Error != "":
				fmt.Fprintf(w, "  %s %s: %s\n", ui.Bad.Render("✗"), x.Path, x.Error)
			case x.Removed && x.BranchKept != "":
				fmt.Fprintf(w, "  %s branch %s kept: %s\n", ui.Dim.Render("·"), x.Branch, x.BranchKept)
			}
		}
	}
	fmt.Fprintf(w, "%s removed %s and %s, freed %s\n", ui.Good.Render("✓"), plural(t.Removed, "worktree"), plural(t.BranchesDel, "merged branch"), ui.Brand.Render(clean.HumanBytes(t.Freed+t.CacheFreed)))
}

// cleanTranscripts lists, and after a yes deletes, session transcripts
// older than --older-than (90 days unless given) for this project, or
// for every project with --all.
func cleanTranscripts(a *app, f cleanFlags, dry bool) error {
	age := f.older
	if age == 0 {
		age = transcriptsAge
	}
	var dirs []string
	if f.all {
		dirs = clean.ProjectDirs(claude.Home())
	} else {
		dirs = []string{claude.ProjectDir(a.root())}
	}
	now := cleanNow()
	var ts []clean.Transcript
	var n int64
	for _, d := range dirs {
		for _, t := range clean.OldTranscripts(d, age, now) {
			ts = append(ts, t)
			n += t.Bytes
		}
	}
	const warn = "tacet status, measure and skill ideas read these transcripts; deleted sessions drop out of them"
	if a.json && (dry || len(ts) == 0) {
		return a.writeJSON(map[string]any{"dry_run": true, "transcripts": ts, "bytes": n, "warning": warn})
	}
	if !a.json {
		fmt.Fprintln(a.out, ui.Section("transcripts older than "+ageText(age), len(ts), width()))
		for _, t := range ts {
			fmt.Fprintf(a.out, "  %10s  %s  %s\n", clean.HumanBytes(t.Bytes), day(t.Modified), ui.Dim.Render(t.Path))
		}
		fmt.Fprintf(a.out, "  %s in all\n", ui.Brand.Render(clean.HumanBytes(n)))
		fmt.Fprintln(a.out, "  "+ui.Bad.Render("note: "+warn))
	}
	if len(ts) == 0 {
		return nil
	}
	if dry {
		fmt.Fprintln(a.out, ui.Dim.Render("dry run: nothing changed"))
		return nil
	}
	if !a.yes {
		ok := false
		title := fmt.Sprintf("Delete %s, %s?", plural(len(ts), "session transcript"), clean.HumanBytes(n))
		if err := huh.NewConfirm().Title(title).Affirmative("Delete").Negative("Keep").Value(&ok).Run(); err != nil && !errors.Is(err, huh.ErrUserAborted) {
			return err
		}
		if !ok {
			fmt.Fprintln(a.out, "nothing changed")
			return nil
		}
	}
	freed, err := clean.RemoveTranscripts(ts)
	if a.json {
		if jerr := a.writeJSON(map[string]any{"transcripts": ts, "freed_bytes": freed}); jerr != nil {
			return jerr
		}
	} else {
		fmt.Fprintf(a.out, "%s deleted %s, freed %s\n", ui.Good.Render("✓"), plural(len(ts), "transcript"), clean.HumanBytes(freed))
	}
	return err
}

func ageText(d time.Duration) string {
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	}
	return d.String()
}

// cleanFinding is the scan's clean/worktrees suggestion. It stays cheap:
// one git call to count agent worktrees, sizes from .tacet/clean.json
// when a tacet clean ran in the last day. A stale or missing summary is
// refreshed in the background for the next scan.
func cleanFinding(root string) []core.Finding {
	main, n, err := clean.Count(root)
	if err != nil || n == 0 {
		return nil
	}
	s, fresh := clean.LoadSummary(main, cleanNow())
	if !fresh {
		refreshClean(main)
	}
	if n <= cleanMinCount && (!fresh || s.Bytes < cleanMinBytes) {
		return nil
	}
	msg := fmt.Sprintf("%s here; run tacet clean to see the space they take and which are safe to remove", plural(n, "agent worktree"))
	sev := core.Info
	if fresh {
		msg = fmt.Sprintf("agent worktrees here take %s (%d safe to remove); run tacet clean", clean.HumanBytes(s.Bytes), s.Safe)
		if s.Bytes >= cleanBigBytes {
			sev = core.Medium
		}
	}
	return []core.Finding{{
		Rule: "clean/worktrees", Severity: sev, Path: filepath.Join(main, ".claude", "worktrees"),
		Message: msg,
		Hint:    "tacet clean removes only worktrees with no uncommitted change and no commit missing from other branches",
		Fix:     &core.Fix{ID: cleanFixID, Title: "remove agent worktrees that hold no unsaved work"},
	}}
}

// refreshClean starts tacet clean --summary in the background, a dry
// run that writes .tacet/clean.json, at most once an hour. A variable so tests
// can see it.
var refreshClean = func(root string) {
	if !config.Initialized(root) {
		return
	}
	mark := filepath.Join(config.Dir(root), "clean.refresh")
	if fi, err := os.Stat(mark); err == nil && time.Since(fi.ModTime()) < time.Hour {
		return
	}
	self, err := os.Executable()
	if err != nil || strings.HasSuffix(self, ".test") {
		return // never re-run a test binary as tacet
	}
	if os.WriteFile(mark, nil, 0o600) != nil {
		return
	}
	cmd := exec.Command(self, "-C", root, "--json", "clean", "--summary") // #nosec G204 -- tacet itself
	detach(cmd)
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}
