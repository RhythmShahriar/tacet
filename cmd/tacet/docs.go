// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/docs/gen"
	"github.com/RhythmShahriar/tacet/docs/lint"
	"github.com/RhythmShahriar/tacet/fix"
	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/RhythmShahriar/tacet/tokens"
	"github.com/charmbracelet/huh"
)

// hasFlag reports and removes a flag.
func hasFlag(args *[]string, name string) bool {
	for i, x := range *args {
		if x == name {
			*args = append((*args)[:i:i], (*args)[i+1:]...)
			return true
		}
	}
	return false
}

// confirm asks once; --yes says yes and a script without it says no.
func confirm(a *app, title string) (bool, error) {
	if a.yes {
		return true, nil
	}
	if !interactive() {
		if !a.json {
			fmt.Fprintln(a.out, ui.Dim.Render("Not a terminal: rerun with --yes to apply."))
		}
		return false, nil
	}
	ok := false
	err := huh.NewConfirm().Title(title).Affirmative("Apply").Negative("Skip").Value(&ok).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return false, nil
	}
	return ok, err
}

func printDiff(w io.Writer, d string) {
	for _, l := range strings.Split(strings.TrimSuffix(d, "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"), strings.HasPrefix(l, "@@"):
			l = ui.Dim.Render(l)
		case strings.HasPrefix(l, "+"):
			l = ui.Good.Render(l)
		case strings.HasPrefix(l, "-"):
			l = ui.Bad.Render(l)
		}
		fmt.Fprintln(w, "    "+l)
	}
}

func relPath(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return p
}

type plannedFix struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Rule   string `json:"rule"`
	Safe   bool   `json:"safe"`
	Tokens int    `json:"tokens"`
	Diff   string `json:"diff"`
	fix    *core.Fix
}

// runDocsFix shows each instruction-file fix as a diff with what it
// takes off startup, then applies the ones confirmed. --safe keeps only
// the safe ones; --dry-run only shows them.
func runDocsFix(a *app, args []string) error {
	dry := hasFlag(&args, "--dry-run")
	safeOnly := hasFlag(&args, "--safe")
	if len(args) > 0 {
		return fmt.Errorf("%w: tacet docs fix [--safe] [--dry-run] [--yes]", errUsage)
	}
	root := a.root()
	s, err := doScan(root, time.Time{})
	if err != nil {
		return err
	}
	dismissed := loadDismissed(root)
	plan := []plannedFix{}
	hints := []core.Finding{}
	for _, f := range s.Docs {
		if f.Fix == nil {
			if f.Rule == "stale/path" || f.Rule == "stale/command" {
				hints = append(hints, f)
			}
			continue
		}
		if dismissed[f.Fix.ID] || (safeOnly && !f.Fix.Safe) {
			continue
		}
		changes, err := lint.Plan([]*core.Fix{f.Fix})
		if err != nil {
			continue // the file moved under us; the next scan replans it
		}
		var d strings.Builder
		for _, c := range changes {
			d.WriteString(lint.Diff(relPath(root, c.Path), c.Old, c.New))
		}
		if d.Len() == 0 {
			continue
		}
		n, err := s.set.Saving(f.Fix)
		if err != nil {
			n = f.Fix.Tokens
		}
		plan = append(plan, plannedFix{ID: f.Fix.ID, Title: f.Fix.Title, Rule: f.Rule, Safe: f.Fix.Safe, Tokens: n, Diff: d.String(), fix: f.Fix})
	}

	before := s.set.StartupTokens()
	all := make([]*core.Fix, 0, len(plan))
	for _, p := range plan {
		all = append(all, p.fix)
	}
	total := 0
	if len(all) > 0 {
		if total, err = s.set.Saving(all...); err != nil {
			return err // fix.Apply would fail the same way
		}
	}

	w := a.out
	if !a.json {
		fmt.Fprintln(w, ui.Section("instruction-file fixes", len(plan), width()))
		if len(plan) == 0 {
			fmt.Fprintln(w, "  "+ui.Good.Render("nothing to fix"))
		}
		for _, p := range plan {
			kind := ui.Dim.Render("suggestion")
			if p.Safe {
				kind = ui.Good.Render("safe")
			}
			fmt.Fprintf(w, "  %s %s  %s  %s\n", ui.Brand.Render("•"), p.Title, kind, tokenDelta(p.Tokens))
			printDiff(w, p.Diff)
			fmt.Fprintln(w)
		}
		if len(hints) > 0 {
			fmt.Fprintln(w, ui.Section("no automatic fix", len(hints), width()))
			for _, h := range hints {
				fmt.Fprintf(w, "  %s:%d %s\n         %s\n", h.Path, h.Line, h.Message, ui.Dim.Render("→ "+h.Hint))
			}
			fmt.Fprintln(w)
		}
		if len(plan) > 0 {
			fmt.Fprintf(w, "Startup instructions: %s → %s tokens (estimate).\n", ui.Tokens(before), ui.Tokens(before-total))
		}
	}

	applied := []string{}
	if len(plan) > 0 && !dry {
		chosen := all
		switch {
		case a.yes:
		case !interactive():
			chosen = nil
			if !a.json {
				fmt.Fprintln(w, ui.Dim.Render("Not a terminal: rerun with --yes to apply them."))
			}
		default:
			opts := make([]huh.Option[int], len(plan))
			picked := make([]int, 0, len(plan))
			for i, p := range plan {
				opts[i] = huh.NewOption(fmt.Sprintf("%s (%s)", p.Title, strings.TrimSpace(tokenDelta(p.Tokens))), i).Selected(true)
				picked = append(picked, i)
			}
			err := huh.NewMultiSelect[int]().Title("Apply which fixes? tacet undo reverts them.").Options(opts...).Value(&picked).Run()
			if err != nil && !errors.Is(err, huh.ErrUserAborted) {
				return err
			}
			chosen = nil
			if err == nil {
				for _, i := range picked {
					chosen = append(chosen, plan[i].fix)
				}
			}
		}
		if len(chosen) > 0 {
			if _, err := fix.Apply(root, chosen); err != nil {
				return err
			}
			for _, f := range chosen {
				applied = append(applied, f.ID)
			}
			if !a.json {
				fmt.Fprintf(w, "%s Applied %d fix(es) (tacet undo reverts them).\n", ui.Good.Render("✓"), len(chosen))
			}
		}
	}
	if a.json {
		return a.writeJSON(map[string]any{"startup_tokens": before, "saving": total, "fixes": plan, "hints": hints, "applied": applied})
	}
	return nil
}

func tokenDelta(n int) string {
	switch {
	case n > 0:
		return ui.Brand.Render("-" + ui.Tokens(n))
	case n < 0:
		return ui.Bad.Render("+" + ui.Tokens(-n))
	}
	return ui.Dim.Render("±0")
}

// runDocsInit writes the lean templates a project is missing and never
// touches a file that exists.
func runDocsInit(a *app, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: tacet docs init [--yes]", errUsage)
	}
	root := a.root()
	edits := gen.Init(root)
	w := a.out
	type file struct {
		Path   string `json:"path"`
		Lines  int    `json:"lines"`
		Tokens int    `json:"tokens"`
	}
	files := []file{}
	for _, e := range edits {
		files = append(files, file{relPath(root, e.Path), strings.Count(string(e.New), "\n"), tokens.Estimate(lint.StripComments(string(e.New)))})
	}
	if !a.json {
		fmt.Fprintln(w, ui.Section("lean templates", len(files), width()))
		if len(files) == 0 {
			fmt.Fprintln(w, "  "+ui.Good.Render("AGENTS.md, CLAUDE.md and ARCHITECTURE.md exist; nothing to write"))
		}
		for _, f := range files {
			fmt.Fprintf(w, "  %s %-16s %3d lines, %s tokens loaded\n", ui.Brand.Render("+"), f.Path, f.Lines, ui.Tokens(f.Tokens))
		}
		if len(files) > 0 {
			fmt.Fprintln(w)
		}
	}
	written := false
	if len(edits) > 0 {
		ok, err := confirm(a, fmt.Sprintf("Write %d file(s)? Existing files are never touched.", len(edits)))
		if err != nil {
			return err
		}
		if ok {
			if _, err := fix.Apply(root, []*core.Fix{{ID: "docs-init", Title: "Write lean instruction templates", Edits: edits}}); err != nil {
				return err
			}
			written = true
		}
	}
	shadowed := false
	if set, err := lint.Load(root, ""); err == nil {
		for _, f := range lint.Check(set) {
			shadowed = shadowed || f.Rule == "agents/shadowed"
		}
	}
	if a.json {
		return a.writeJSON(map[string]any{"files": files, "written": written, "agents_shadowed": shadowed})
	}
	if written {
		fmt.Fprintf(w, "%s Wrote %d file(s) (tacet undo removes them). Fill in the comments; they cost nothing until you do.\n", ui.Good.Render("✓"), len(edits))
	}
	if shadowed {
		fmt.Fprintln(w, ui.Brand.Render("•")+" Your CLAUDE.md doesn't import AGENTS.md: tacet docs fix adds the @AGENTS.md line.")
	}
	return nil
}

// runDocsSync copies AGENTS.md into the other agents' files, reporting
// generated files edited by hand instead of overwriting them.
func runDocsSync(a *app, args []string) error {
	force := hasFlag(&args, "--force")
	check := hasFlag(&args, "--check")
	var to []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--to" && i+1 < len(args):
			to = append(to, strings.Split(args[i+1], ",")...)
			i++
		case strings.HasPrefix(args[i], "--to="):
			to = append(to, strings.Split(strings.TrimPrefix(args[i], "--to="), ",")...)
		default:
			return fmt.Errorf("%w: tacet docs sync [--to cursor,copilot,gemini] [--force] [--check] [--yes]", errUsage)
		}
	}
	root := a.root()
	res, err := gen.Sync(root, to)
	if err != nil {
		return err
	}
	w := a.out
	var writes []core.Edit
	pending := 0
	type row struct {
		Target string    `json:"target"`
		Path   string    `json:"path"`
		State  gen.State `json:"state"`
	}
	rows := []row{}
	if !a.json {
		fmt.Fprintln(w, ui.Section("docs sync from AGENTS.md", len(res.Items), width()))
	}
	if res.OverCodex && !a.json {
		fmt.Fprintf(w, "  %s AGENTS.md is %d KiB; Codex stops reading at 32 KiB, so the end is lost there\n", ui.Bad.Render("!"), res.AgentsBytes>>10)
	}
	if len(res.Items) == 0 && !a.json {
		fmt.Fprintln(w, "  "+ui.Dim.Render("no Cursor, Copilot or Gemini file here; name them with --to cursor,copilot,gemini"))
	}
	for _, it := range res.Items {
		rows = append(rows, row{it.Target.Name, it.Target.Path, it.State})
		write := false
		msg := ""
		switch it.State {
		case gen.Current:
			msg = ui.Good.Render("✓") + " up to date"
		case gen.New:
			msg, write = ui.Brand.Render("+")+" will be written", true
		case gen.Stale:
			msg, write = ui.Brand.Render("~")+" AGENTS.md changed; will be regenerated", true
		case gen.Drift:
			msg, write = ui.Bad.Render("!")+" edited by hand since tacet wrote it; move the edit into AGENTS.md and sync again, or --force", force
		case gen.Manual:
			msg, write = ui.Bad.Render("!")+" written by hand; --force replaces it with AGENTS.md (tacet undo restores it)", force
		}
		if it.State != gen.Current {
			pending++
		}
		if !a.json {
			fmt.Fprintf(w, "  %-9s %-34s %s\n", it.Target.Name, it.Target.Path, msg)
		}
		if write {
			writes = append(writes, core.Edit{Path: it.Path, Old: it.Old, New: it.New})
		}
	}
	if check {
		if a.json {
			if err := a.writeJSON(map[string]any{"targets": rows, "over_codex": res.OverCodex}); err != nil {
				return err
			}
		}
		if pending > 0 || res.OverCodex {
			return errFindings
		}
		return nil
	}
	written := false
	if len(writes) > 0 {
		ok, err := confirm(a, fmt.Sprintf("Write %d file(s) from AGENTS.md?", len(writes)))
		if err != nil {
			return err
		}
		if ok {
			if _, err := fix.Apply(root, []*core.Fix{{ID: "docs-sync", Title: "Sync AGENTS.md to other agents", Edits: writes}}); err != nil {
				return err
			}
			written = true
		}
	}
	if a.json {
		return a.writeJSON(map[string]any{"targets": rows, "over_codex": res.OverCodex, "written": written})
	}
	if written {
		fmt.Fprintf(w, "%s Wrote %d file(s) (tacet undo reverts them).\n", ui.Good.Render("✓"), len(writes))
	}
	return nil
}
