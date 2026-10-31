// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/fix"
	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/internal/ui"
)

func runMeasure(a *app, _ []string) error {
	s, err := doScan(a.root(), time.Time{})
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(map[string]any{"startup": s.Startup, "usage": s.Usage})
	}
	w := a.out
	fmt.Fprintln(w, ui.Banner(version))
	fmt.Fprintln(w)
	fmt.Fprintln(w, ui.Section("loaded every session", -1, width()))
	for _, it := range s.Startup.Items {
		note := ""
		if it.Note != "" {
			note = ui.Dim.Render("  " + it.Note)
		}
		fmt.Fprintf(w, "  %8s  %-7s %s%s\n", ui.Tokens(it.Tokens), it.Group, it.Name, note)
	}
	fmt.Fprintf(w, "  %8s  %s\n", ui.Brand.Render(ui.Tokens(s.Startup.Total)), ui.Label.Render("total, on top of Claude Code's own prompt (estimate)"))
	u := s.Usage
	fmt.Fprintln(w)
	fmt.Fprintln(w, ui.Section("last sessions", u.Sessions, width()))
	if u.Requests == 0 {
		fmt.Fprintln(w, "  "+ui.Dim.Render("no transcripts for this project yet"))
		return nil
	}
	fmt.Fprintf(w, "  %d requests, %s tokens each on average, %s cache hits\n", u.Requests, ui.Tokens(u.PerRequest()), pct(u.HitRate))
	fmt.Fprintf(w, "  %s output tokens, %s written to the cache, %s read from it\n", ui.Tokens(u.Output), ui.Tokens(u.CacheWrite), ui.Tokens(u.CacheRead))
	if u.Subagents > 0 {
		fmt.Fprintf(w, "  %d subagents made %d more requests, %s tokens in all\n", u.Subagents, u.SubRequests, ui.Tokens(u.SubTokens))
	}
	if len(u.Misses) > 0 {
		fmt.Fprintf(w, "  %d cache misses reprocessed %s tokens (tacet audit says why)\n", len(u.Misses), ui.Tokens(u.MissTokens))
	}
	return nil
}

func pct(f float64) string { return fmt.Sprintf("%.0f%%", f*100) }

func runAudit(a *app, _ []string) error {
	s, err := doScan(a.root(), time.Time{})
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(s.Audit)
	}
	printFindings(a.out, "setup", s.Audit)
	return nil
}

func runDocs(a *app, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "fix":
			return runDocsFix(a, args[1:])
		case "init":
			return runDocsInit(a, args[1:])
		case "sync":
			return runDocsSync(a, args[1:])
		case "lint":
		default:
			return fmt.Errorf("%w: tacet docs lint|fix|init|sync", errUsage)
		}
	}
	s, err := doScan(a.root(), time.Time{})
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(s.Docs)
	}
	printFindings(a.out, "instruction files", s.Docs)
	return nil
}

// runCheck is the CI gate: findings at or above --fail-on (default
// medium) exit 1.
func runCheck(a *app, args []string) error {
	failOn := core.Medium
	for i := 0; i < len(args); i++ {
		if args[i] == "--fail-on" && i+1 < len(args) {
			failOn = core.Severity(args[i+1])
			i++
		}
	}
	rank := map[core.Severity]int{core.High: 3, core.Medium: 2, core.Low: 1, core.Info: 0}
	if _, ok := rank[failOn]; !ok {
		return fmt.Errorf("%w: --fail-on high|medium|low|info", errUsage)
	}
	s, err := doScan(a.root(), time.Time{})
	if err != nil {
		return err
	}
	// CI has no transcripts or user config; judge only what is in the repo.
	var fs []core.Finding
	for _, f := range s.all() {
		if strings.HasPrefix(f.Rule, "cache/") || f.Rule == "git/attribution" || strings.HasSuffix(f.Rule, "/unused") || strings.HasPrefix(f.Rule, "statusline/") || strings.HasPrefix(f.Rule, "clean/") {
			continue
		}
		fs = append(fs, f)
	}
	if a.json {
		if err := a.writeJSON(fs); err != nil {
			return err
		}
	} else {
		printFindings(a.out, "tacet check", fs)
	}
	for _, f := range fs {
		if rank[f.Severity] >= rank[failOn] {
			return errFindings
		}
	}
	return nil
}

func runOn(a *app, args []string) error  { return setFeature(a, args, true) }
func runOff(a *app, args []string) error { return setFeature(a, args, false) }

func setFeature(a *app, args []string, on bool) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: tacet on|off <%s>", errUsage, strings.Join(config.FeatureNames(), "|"))
	}
	root := a.root()
	c := config.Load(root)
	if err := c.Set(args[0], on); err != nil {
		return fmt.Errorf("%w %q; features: %s", err, args[0], strings.Join(config.FeatureNames(), ", "))
	}
	if err := featureFiles(a, root, args[0], on); err != nil {
		return err
	}
	if err := c.Save(root); err != nil {
		return err
	}
	state := "off"
	if on {
		state = "on"
	}
	fmt.Fprintf(a.out, "%s is %s for this project\n", args[0], state)
	return nil
}

func runUndo(a *app, _ []string) error {
	run, err := fix.Undo(a.root())
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "reverted %s (%d files)\n", strings.Join(run.Fixes, ", "), len(run.Changes))
	return nil
}

// runApply applies one suggestion by fix id, the way /tacet does.
func runApply(a *app, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: tacet apply <fix-id>", errUsage)
	}
	if args[0] == cleanFixID {
		// The person said yes to it in /tacet: remove the safe ones.
		a.yes = true
		return runClean(a, nil)
	}
	root := a.root()
	s, err := doScan(root, time.Time{})
	if err != nil {
		return err
	}
	for _, f := range s.all() {
		if f.Fix != nil && f.Fix.ID == args[0] {
			if _, err := fix.Apply(root, []*core.Fix{f.Fix}); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "%s %s (tacet undo reverts it)\n", ui.Good.Render("✓"), f.Fix.Title)
			return nil
		}
	}
	return fmt.Errorf("no suggestion %q now; run tacet status", args[0])
}

func runDismiss(a *app, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: tacet dismiss <fix-id>", errUsage)
	}
	root := a.root()
	m := loadDismissed(root)
	m[args[0]] = true
	if err := writeFileJSON(dismissedPath(root), m); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "won't suggest %s again here\n", args[0])
	return nil
}
