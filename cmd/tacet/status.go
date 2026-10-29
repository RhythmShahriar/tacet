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
	"github.com/RhythmShahriar/tacet/measure"
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
	// Main sessions only: what the savings compare. enough_data is false
	// until min_requests main-session requests ran since init.
	PerRequestMain    int            `json:"per_request_main"`
	HitRateMain       float64        `json:"cache_hit_rate_main"`
	HitRateMainActive float64        `json:"cache_hit_rate_main_without_idle"`
	RequestsMain      int            `json:"requests_main"`
	MinRequests       int            `json:"min_requests"`
	EnoughData        bool           `json:"enough_data"`
	IdleMisses        []measure.Miss `json:"idle_misses,omitempty"`
	Subagents         int            `json:"subagents"`
	SubRequests       int            `json:"subagent_requests"`
	SubTokens         int            `json:"subagent_tokens"`
	Suggestions       []suggestion   `json:"suggestions"`
	DocReads          int            `json:"document_reads"`
	DocSaved          int            `json:"document_tokens_saved"`
}

// minRequests is how many main-session requests since init a billed
// comparison needs. Fewer swing on one cold cache.
const minRequests = 20

// enoughData reports whether u can be compared against b.
func enoughData(b *Baseline, u *measure.Usage) bool {
	return b != nil && b.RequestsMain > 0 && u.MainRequests >= minRequests
}

// notEnough is what shows instead of a percentage.
func notEnough(b *Baseline, u *measure.Usage) string {
	if b != nil && b.RequestsMain == 0 && u.MainRequests >= minRequests {
		return "nothing to compare: no main-session requests before init"
	}
	return fmt.Sprintf("not enough data yet (%d of %d requests)", u.MainRequests, minRequests)
}

// idleMisses are the main-session requests that came back to an expired
// cache after an idle gap.
func idleMisses(u *measure.Usage) []measure.Miss {
	var out []measure.Miss
	for _, m := range u.Misses {
		if m.Cause == "idle" && !m.Subagent {
			out = append(out, m)
		}
	}
	return out
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
	u := s.Usage
	st.PerRequestMain, st.HitRateMain, st.HitRateMainActive, st.RequestsMain = u.PerRequestMain(), u.HitRateMain(), u.HitRateMainActive(), u.MainRequests
	st.MinRequests, st.EnoughData, st.IdleMisses = minRequests, enoughData(b, u), idleMisses(u)
	st.Subagents, st.SubRequests, st.SubTokens = u.Subagents, u.SubRequests, u.SubTokens
	st.DocReads, st.DocSaved = docSavings(root)
	for _, f := range sug {
		st.Suggestions = append(st.Suggestions, suggestion{ID: f.Fix.ID, Title: f.Fix.Title, Why: f.Message, Tokens: saving(f)})
	}
	if a.json {
		return a.writeJSON(st)
	}
	w := a.out
	fmt.Fprintln(w, ui.Banner(version))
	fmt.Fprintln(w)
	fmt.Fprintln(w, ui.Section("savings", -1, width()))
	if b != nil {
		fmt.Fprintf(w, "  startup context   %s → %s  %s\n", ui.Tokens(b.Startup), ui.Tokens(st.Startup), delta(b.Startup, st.Startup))
		if st.EnoughData {
			fmt.Fprintf(w, "  billed per request %s → %s  %s  %s\n", ui.Tokens(b.PerRequestMain), ui.Tokens(st.PerRequestMain), delta(b.PerRequestMain, st.PerRequestMain),
				ui.Dim.Render(fmt.Sprintf("(%d main-session requests since init)", st.RequestsMain)))
			fmt.Fprintf(w, "  cache hits        %s → %s  %s\n", pct(b.HitRateMain), pct(st.HitRateMain), ui.Dim.Render(fmt.Sprintf("(%s without %s after an idle gap)", pct(st.HitRateMainActive), plural(len(st.IdleMisses), "request"))))
		} else {
			fmt.Fprintln(w, "  billed per request "+ui.Dim.Render(notEnough(b, u)))
		}
	}
	if st.SubRequests > 0 {
		fmt.Fprintf(w, "  subagents         %s, %s tokens %s\n", plural(st.SubRequests, "request"), ui.Tokens(st.SubTokens), ui.Dim.Render("(not in the comparison)"))
	}
	for i, m := range st.IdleMisses {
		if i == 5 {
			fmt.Fprintln(w, "  "+ui.Dim.Render(fmt.Sprintf("  and %d more", len(st.IdleMisses)-5)))
			break
		}
		fmt.Fprintf(w, "  idle miss         %s  %s tokens re-cached  %s\n", m.Time.Local().Format("2 Jan 15:04"), ui.Tokens(m.Tokens), ui.Dim.Render(shortID(m.Session)))
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

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
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
