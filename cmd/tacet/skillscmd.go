// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/audit"
	"github.com/RhythmShahriar/tacet/claude"
	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/RhythmShahriar/tacet/measure"
	"github.com/RhythmShahriar/tacet/skills"
)

const skillsUsage = `tacet skills [new "<description>" | new --from-session [--transcript <path>] | check | add <source> [--allow-scripts] | add --list]`

// runSkills lists the repeated requests tacet would turn into skills,
// drafts one on demand or checks the skills already there.
func runSkills(a *app, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "new":
			return runSkillsNew(a, args[1:])
		case "check":
			return runSkillsCheck(a, args[1:])
		case "add":
			return runSkillsAdd(a, args[1:])
		}
		return fmt.Errorf("%w: %s", errUsage, skillsUsage)
	}
	root := a.root()
	cs := skills.Candidates(root)
	on := config.Load(root).On("skills")
	if a.json {
		if cs == nil {
			cs = []skills.Candidate{}
		}
		return a.writeJSON(map[string]any{"enabled": on, "candidates": cs})
	}
	w := a.out
	fmt.Fprintln(w, ui.Section("repeated requests", len(cs), width()))
	if len(cs) == 0 {
		fmt.Fprintf(w, "  %s\n", ui.Dim.Render(fmt.Sprintf("nothing asked %d+ times across %d+ sessions yet", skills.MinCount, skills.MinSessions)))
		return nil
	}
	for _, c := range cs {
		fmt.Fprintf(w, "  %s  %s\n", ui.Brand.Render(c.Name), ui.Dim.Render(fmt.Sprintf("%d times in %d sessions", c.Count, c.Sessions)))
		fmt.Fprintf(w, "    %q\n", short100(c.Example))
		switch {
		case c.Exists:
			fmt.Fprintf(w, "    %s\n", ui.Dim.Render("a skill by this name already exists"))
		case on:
			fmt.Fprintf(w, "    %s\n", ui.Dim.Render("tacet apply skill-"+c.Name+"  ·  tacet dismiss skill-"+c.Name))
		}
	}
	if !on {
		fmt.Fprintf(w, "\n  %s\n", ui.Dim.Render("skill suggestions are off here: tacet on skills"))
	}
	return nil
}

func short100(s string) string {
	if r := []rune(s); len(r) > 100 {
		return string(r[:100]) + "..."
	}
	return s
}

// runSkillsNew drafts a skill from a description or the latest session
// and writes it only after a yes. --json shows the draft and writes
// nothing.
func runSkillsNew(a *app, args []string) error {
	fromSession := hasFlag(&args, "--from-session")
	transcript, err := flagValue(&args, "--transcript")
	if err != nil {
		return err
	}
	name, err := flagValue(&args, "--name")
	if err != nil {
		return err
	}
	if transcript != "" {
		fromSession = true
	}
	text := strings.TrimSpace(strings.Join(args, " "))
	root := a.root()
	var d skills.OnDemand
	if fromSession {
		d, err = skills.NewFromSession(root, transcript, text, name)
	} else {
		d, err = skills.NewFromDescription(root, text, name)
	}
	if err != nil {
		return err
	}
	if _, err := os.Stat(d.Path); err == nil {
		return fmt.Errorf("%w: %s (pick another --name)", skills.ErrExists, relPath(root, d.Path))
	}
	if a.json && !a.yes {
		return a.writeJSON(map[string]any{"draft": d, "written": false})
	}
	if !a.json {
		fmt.Fprintln(a.out, ui.Section("new skill", 1, width()))
		fmt.Fprintf(a.out, "  %s\n\n", ui.Dim.Render(relPath(root, d.Path)))
		for _, l := range strings.Split(strings.TrimRight(d.Skill, "\n"), "\n") {
			if l != "" {
				l = "    " + l
			}
			fmt.Fprintln(a.out, l)
		}
		fmt.Fprintln(a.out)
	}
	ok, err := confirm(a, "Write "+relPath(root, d.Path)+"?")
	if err != nil || !ok {
		return err
	}
	if err := d.Write(); err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(map[string]any{"draft": d, "written": true})
	}
	fmt.Fprintf(a.out, "%s wrote %s: Claude picks it up when you ask, or type /%s\n", ui.Good.Render("✓"), relPath(root, d.Path), d.Name)
	return nil
}

// flagValue reads and removes "name value".
func flagValue(args *[]string, name string) (string, error) {
	for i, x := range *args {
		if x != name {
			continue
		}
		if i+1 >= len(*args) {
			return "", fmt.Errorf("%w: %s needs a value", errUsage, name)
		}
		v := (*args)[i+1]
		*args = append((*args)[:i:i], (*args)[i+2:]...)
		return v, nil
	}
	return "", nil
}

// runSkillsCheck lists skills that never trigger, descriptions near or
// over the cap and overlapping skills, with the startup tokens each fix
// saves.
func runSkillsCheck(a *app, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: tacet skills check [--json]", errUsage)
	}
	fs, err := skillChecks(a.root())
	if err != nil {
		return err
	}
	total := 0
	for _, f := range fs {
		total += saving(f)
	}
	if a.json {
		type item struct {
			Rule     string `json:"rule"`
			Severity string `json:"severity"`
			Path     string `json:"path"`
			Message  string `json:"message"`
			Fix      string `json:"fix,omitempty"`
			Title    string `json:"title,omitempty"`
			Tokens   int    `json:"startup_tokens_saved"`
		}
		items := []item{}
		for _, f := range fs {
			it := item{Rule: f.Rule, Severity: string(f.Severity), Path: f.Path, Message: f.Message, Tokens: saving(f)}
			if f.Fix != nil {
				it.Fix, it.Title = f.Fix.ID, f.Fix.Title
			}
			items = append(items, it)
		}
		return a.writeJSON(map[string]any{"findings": items, "startup_tokens_saved": total})
	}
	w := a.out
	fmt.Fprintln(w, ui.Section("skills check", len(fs), width()))
	if len(fs) == 0 {
		fmt.Fprintln(w, "  "+ui.Good.Render("every skill triggers, stays short and covers its own requests"))
		return nil
	}
	for _, f := range fs {
		fmt.Fprintf(w, "  %s  %s\n", ui.Brand.Render(fmt.Sprintf("%-18s", f.Rule)), f.Message)
		if f.Fix != nil {
			fmt.Fprintf(w, "    %s\n", ui.Dim.Render(fmt.Sprintf("%s: saves %s startup tokens · tacet apply %s", f.Fix.Title, ui.Tokens(saving(f)), f.Fix.ID)))
		} else if f.Hint != "" {
			fmt.Fprintf(w, "    %s\n", ui.Dim.Render("→ "+f.Hint))
		}
	}
	fmt.Fprintf(w, "\n  %s\n", ui.Dim.Render("every fix together saves "+ui.Tokens(total)+" startup tokens; tacet undo reverts each"))
	return nil
}

// skillChecks runs the audit's skills rules only.
func skillChecks(root string) ([]core.Finding, error) {
	st, _, err := measure.MeasureStartup(root, measure.Options{UserHome: claude.Home()})
	if err != nil {
		return nil, err
	}
	u := measure.MeasureUsage(root, time.Time{}, recentSessions)
	fs := audit.SkillChecks(audit.Input{Root: root, Startup: st, Usage: u})
	for i := range fs {
		fs[i].Path = relPath(root, fs[i].Path)
	}
	core.Rank(fs)
	return fs, nil
}
