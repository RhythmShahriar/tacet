// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/RhythmShahriar/tacet/skills"
)

// runSkills lists the repeated requests tacet would turn into skills.
func runSkills(a *app, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: tacet skills [--json]", errUsage)
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
