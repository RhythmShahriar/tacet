// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/fix"
	"github.com/RhythmShahriar/tacet/handoff"
	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/RhythmShahriar/tacet/plugin"
)

type suggestion struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Why    string `json:"why"`
	Tokens int    `json:"tokens,omitempty"`
}

type statusJSON struct {
	Root        string          `json:"root"`
	Initialized bool            `json:"initialized"`
	Features    map[string]bool `json:"features"`
	Baseline    *Baseline       `json:"baseline,omitempty"`
	Startup     int             `json:"startup_tokens"`
	PerRequest  int             `json:"per_request_tokens"`
	SinceInit   int             `json:"sessions_since_init"`
	HitRate     float64         `json:"cache_hit_rate"`
	Suggestions []suggestion    `json:"suggestions"`
	DocReads    int             `json:"document_reads"`
	DocSaved    int             `json:"document_tokens_saved"`
	Update      *updateInfo     `json:"update,omitempty"`
}

// docSavings adds up .tacet/savings.jsonl.
func docSavings(root string) (reads, saved int) {
	b, err := os.ReadFile(savingsPath(root))
	if err != nil {
		return 0, 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		var s Saving
		if json.Unmarshal([]byte(l), &s) == nil && s.File != "" {
			if s.Method != "follow-up" {
				reads++
			}
			saved += s.Saved
		}
	}
	return reads, saved
}

func runStatus(a *app, _ []string) error {
	root := a.root()
	if !config.Initialized(root) {
		if a.json {
			return a.writeJSON(statusJSON{Root: root})
		}
		fmt.Fprintln(a.out, ui.Banner(version))
		fmt.Fprintln(a.out, "tacet isn't set up here yet. Run: tacet init")
		return nil
	}
	b := loadBaseline(root)
	since := time.Time{}
	if b != nil {
		since = b.Time
	}
	s, err := doScan(root, since)
	if err != nil {
		return err
	}
	_, sug := s.fixes()
	st := statusJSON{Root: root, Initialized: true, Features: config.Load(root).Features, Baseline: b,
		Startup: s.Startup.Total, PerRequest: s.Usage.PerRequest(), SinceInit: s.Usage.Sessions, HitRate: s.Usage.HitRate}
	st.DocReads, st.DocSaved = docSavings(root)
	for _, f := range sug {
		st.Suggestions = append(st.Suggestions, suggestion{ID: f.Fix.ID, Title: f.Fix.Title, Why: f.Message, Tokens: saving(f)})
	}
	st.Update = updateStatus()
	if a.json {
		return a.writeJSON(st)
	}
	w := a.out
	fmt.Fprintln(w, ui.Banner(version))
	if u := st.Update; u != nil {
		fmt.Fprintln(w, ui.Brand.Render(fmt.Sprintf("tacet %s is available: run %s", u.Latest, u.Command)))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, ui.Section("savings", -1, width()))
	if b != nil {
		fmt.Fprintf(w, "  startup context   %s → %s  %s\n", ui.Tokens(b.Startup), ui.Tokens(st.Startup), delta(b.Startup, st.Startup))
		if st.SinceInit > 0 && b.PerRequest > 0 {
			fmt.Fprintf(w, "  billed per request %s → %s  %s  (%d sessions since init)\n", ui.Tokens(b.PerRequest), ui.Tokens(st.PerRequest), delta(b.PerRequest, st.PerRequest), st.SinceInit)
		} else {
			fmt.Fprintln(w, "  "+ui.Dim.Render("billed savings show after your next sessions"))
		}
	}
	if st.DocReads > 0 {
		fmt.Fprintf(w, "  documents         %d reads, about %s fewer tokens read, net %s\n", st.DocReads, ui.Tokens(st.DocSaved), ui.Dim.Render("(PDF, Word, PowerPoint, Excel)"))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, ui.Section("suggestions", len(st.Suggestions), width()))
	if len(st.Suggestions) == 0 {
		fmt.Fprintln(w, "  "+ui.Good.Render("nothing waiting"))
	}
	for _, x := range st.Suggestions {
		tok := ""
		if x.Tokens > 0 {
			tok = ui.Brand.Render(" -" + ui.Tokens(x.Tokens))
		}
		fmt.Fprintf(w, "  %s%s\n    %s\n    %s\n", x.Title, tok, ui.Dim.Render(x.Why), ui.Dim.Render("tacet apply "+x.ID+"  ·  tacet dismiss "+x.ID))
	}
	fmt.Fprintln(w)
	on := ""
	for _, f := range config.FeatureNames() {
		mark := ui.Dim.Render(f)
		if st.Features[f] {
			mark = ui.Good.Render(f)
		}
		on += mark + " "
	}
	fmt.Fprintln(w, ui.Section("features", -1, width()))
	fmt.Fprintln(w, "  "+on)
	fmt.Fprintln(w, "  "+ui.Dim.Render("tacet on|off <feature>"))
	return nil
}

func delta(before, after int) string {
	if before <= 0 {
		return ""
	}
	d := float64(after-before) / float64(before) * 100
	switch {
	case d < -0.5:
		return ui.Good.Render(fmt.Sprintf("%.0f%%", d))
	case d > 0.5:
		return ui.Bad.Render(fmt.Sprintf("+%.0f%%", d))
	}
	return ui.Dim.Render("same")
}

// runUninstall reverts every tacet change, removes the plugin and the
// project's .tacet folder.
func runUninstall(a *app, _ []string) error {
	root := a.root()
	n := 0
	for {
		if _, err := fix.Undo(root); err != nil {
			break
		}
		n++
	}
	if err := plugin.Uninstall(); err != nil {
		return err
	}
	if b, err := os.ReadFile(handoff.Path(root)); err == nil && len(b) >= len(handoff.Header) && string(b[:len(handoff.Header)]) == handoff.Header {
		_ = os.Remove(handoff.Path(root))
	}
	if err := os.RemoveAll(config.Dir(root)); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "tacet removed: %d change set(s) reverted, plugin and /tacet skill removed.\n", n)
	return nil
}
