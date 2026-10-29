// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/fix"
	"github.com/RhythmShahriar/tacet/handoff"
	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/RhythmShahriar/tacet/plugin"
	"github.com/charmbracelet/huh"
)

// runInit is the one command most people run: measure, apply the safe
// fixes after one yes, install the plugin and hand over to the hooks.
func runInit(a *app, args []string) error {
	noPlugin := false
	for _, x := range args {
		if x == "--no-plugin" {
			noPlugin = true
		}
	}
	root := a.root()
	w := a.out
	if !a.json {
		fmt.Fprintln(w, ui.Banner(version))
		fmt.Fprintln(w, ui.Dim.Render("project "+root))
		fmt.Fprintln(w)
	}
	s, err := doScan(root, time.Time{})
	if err != nil {
		return err
	}
	safe, sug := s.fixes()

	cfg := config.Load(root)
	if err := cfg.Save(root); err != nil {
		return err
	}
	_ = config.Exclude(root)
	_ = config.ExcludePath(root, handoff.File)
	if err := saveBaseline(root, s); err != nil {
		return err
	}

	saved := 0
	for _, f := range safe {
		saved += saving(f)
	}
	if !a.json {
		fmt.Fprintf(w, "Every session here loads about %s tokens of your setup before you type, plus Claude Code's own prompt.\n",
			ui.Brand.Render(ui.Tokens(s.Startup.Total)))
		if s.Usage.Requests > 0 {
			fmt.Fprintf(w, "Your last %d sessions averaged %s tokens a request with %s cache hits.\n",
				s.Usage.Sessions, ui.Tokens(s.Usage.PerRequest()), pct(s.Usage.HitRate))
		}
		fmt.Fprintln(w)
	}

	applied := []string{}
	if len(safe) > 0 {
		if !a.json {
			fmt.Fprintln(w, ui.Section("safe fixes", len(safe), width()))
			for _, f := range safe {
				fmt.Fprintf(w, "  %s %s\n", ui.Brand.Render("•"), f.Fix.Title)
			}
			fmt.Fprintln(w)
		}
		ok := a.yes
		if !ok && interactive() {
			title := fmt.Sprintf("Apply %d safe fix(es)? tacet undo reverts them.", len(safe))
			if err := huh.NewConfirm().Title(title).Affirmative("Apply").Negative("Skip").Value(&ok).Run(); err != nil && !errors.Is(err, huh.ErrUserAborted) {
				return err
			}
		} else if !ok && !a.json {
			fmt.Fprintln(w, ui.Dim.Render("Not a terminal: rerun with --yes to apply them."))
		}
		if ok {
			fixes := make([]*core.Fix, 0, len(safe))
			for _, f := range safe {
				fixes = append(fixes, f.Fix)
				applied = append(applied, f.Fix.ID)
			}
			if _, err := fix.Apply(root, fixes); err != nil {
				return err
			}
		}
	}

	pluginErr := error(nil)
	if !noPlugin {
		bin, err := os.Executable()
		if err == nil {
			bin, _ = filepath.EvalSymlinks(bin)
			pluginErr = plugin.Install(bin, version)
		} else {
			pluginErr = err
		}
	}

	if a.json {
		return a.writeJSON(map[string]any{
			"root": root, "startup_tokens": s.Startup.Total, "applied": applied,
			"suggestions": len(sug), "plugin_error": errString(pluginErr), "saved_estimate": saved,
		})
	}
	if len(applied) > 0 {
		fmt.Fprintf(w, "%s Applied %d fix(es).\n", ui.Good.Render("✓"), len(applied))
	}
	switch {
	case noPlugin:
		fmt.Fprintln(w, ui.Dim.Render("Plugin skipped (--no-plugin): hooks won't run until you install it."))
	case errors.Is(pluginErr, plugin.ErrNoClaude):
		fmt.Fprintln(w, ui.Bad.Render("!")+" Claude Code's claude command isn't on PATH, so the plugin isn't installed. Install Claude Code, then run tacet init again.")
	case pluginErr != nil:
		fmt.Fprintln(w, ui.Bad.Render("!")+" Plugin not installed: "+pluginErr.Error())
	default:
		fmt.Fprintf(w, "%s Plugin installed: hooks now run on their own in this project.\n", ui.Good.Render("✓"))
	}
	if len(sug) > 0 {
		fmt.Fprintf(w, "%s %d suggestion(s) need a yes from you: type /tacet in Claude Code.\n", ui.Brand.Render("•"), len(sug))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Done. tacet now works on its own. Restart Claude Code to load the plugin; type /tacet anytime.")
	return nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
