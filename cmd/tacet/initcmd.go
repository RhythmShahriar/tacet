// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/audit"
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
	subModel := ""
	for i := 0; i < len(args); i++ {
		switch x := args[i]; {
		case x == "--no-plugin":
			noPlugin = true
		case x == "--subagents" && i+1 < len(args):
			subModel = strings.ToLower(args[i+1])
			i++
		case strings.HasPrefix(x, "--subagents="):
			subModel = strings.ToLower(strings.TrimPrefix(x, "--subagents="))
		}
	}
	if subModel != "" && !slices.Contains(audit.SubagentModels, subModel) {
		return fmt.Errorf("%w: --subagents haiku|sonnet|main", errUsage)
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

	if err := chooseSubagents(a, root, subModel); err != nil {
		return err
	}

	pluginErr := error(nil)
	npx := false
	if !noPlugin {
		bin, err := os.Executable()
		if err == nil {
			bin, _ = filepath.EvalSymlinks(bin)
			npx = inNpxCache(bin)
			pluginErr = plugin.Install(bin, version)
		} else {
			pluginErr = err
		}
	}

	if a.json {
		return a.writeJSON(map[string]any{
			"root": root, "startup_tokens": s.Startup.Total, "applied": applied,
			"suggestions": len(sug), "plugin_error": errString(pluginErr), "saved_estimate": saved,
			"npx_warning": npx,
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
	if npx {
		fmt.Fprintln(w, ui.Bad.Render("!")+" "+npxWarning)
	}
	if len(sug) > 0 {
		fmt.Fprintf(w, "%s %d suggestion(s) need a yes from you: type /tacet in Claude Code.\n", ui.Brand.Render("•"), len(sug))
	}
	for _, f := range sug {
		if f.Fix.ID == "statusline" {
			fmt.Fprintf(w, "%s Optional: %s shows tokens saved in Claude Code's statusline (terminal only).\n", ui.Brand.Render("•"), ui.Label.Render("tacet statusline --install"))
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Done. tacet now works on its own. Restart Claude Code to load the plugin; type /tacet anytime.")
	return nil
}

// npxWarning tells an npx user the hooks point at a copy npm may delete.
const npxWarning = "tacet is running from npx's temporary cache, so the hooks stop working once npm clears it. " +
	"Install it globally so the hooks keep working: npm i -g @rhythmsh/tacet, then run tacet init again."

// inNpxCache reports whether bin sits in npx's cache (~/.npm/_npx/<hash>),
// which npm may clear at any time.
func inNpxCache(bin string) bool {
	for _, part := range strings.Split(filepath.ToSlash(bin), "/") {
		if part == "_npx" {
			return true
		}
	}
	return false
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// runPlugin writes the plugin without installing it, for
// claude --plugin-dir and for packaging.
func runPlugin(a *app, args []string) error {
	if len(args) != 2 || args[0] != "write" {
		return fmt.Errorf("%w: tacet plugin write <dir>", errUsage)
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	bin, _ = filepath.EvalSymlinks(bin)
	if err := plugin.Write(args[1], bin, version); err != nil {
		return err
	}
	fmt.Fprintln(a.out, filepath.Join(args[1], "plugins", plugin.Name))
	return nil
}

// chooseSubagents asks which model search subagents should use, the one
// choice init leaves to the person: it trades cost against how hard a
// search the cheaper model gets right. --subagents answers it; --yes
// takes the recommended Haiku; without a terminal nothing changes.
func chooseSubagents(a *app, root, model string) error {
	if audit.ProjectExplore(root) != "" && !audit.IsTacetExplore(readOrNil(audit.ExploreAgentPath(root))) {
		return nil // the person's own Explore agent stays
	}
	switch {
	case model != "":
	case a.yes:
		model = "haiku"
	case interactive() && !a.json:
		model = "haiku"
		err := huh.NewSelect[string]().
			Title("Which model should search subagents use?").
			Description("Only the read-only Explore agent changes. Your main model still plans and writes the code.").
			Options(
				huh.NewOption("Haiku: cheapest, recommended (about 40% lower cost in tacet's benchmark)", "haiku"),
				huh.NewOption("Sonnet: in between", "sonnet"),
				huh.NewOption("My main model: no change", "main"),
			).Value(&model).Run()
		if errors.Is(err, huh.ErrUserAborted) {
			return nil
		}
		if err != nil {
			return err
		}
	default:
		return nil
	}
	if a.json {
		quiet := *a
		quiet.out = io.Discard
		return runSubagents(&quiet, []string{model})
	}
	return runSubagents(a, []string{model})
}

func readOrNil(p string) []byte {
	b, err := os.ReadFile(p) // #nosec G304 -- tacet's own agent file path
	if err != nil {
		return nil
	}
	return b
}
