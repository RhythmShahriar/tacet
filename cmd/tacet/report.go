// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/RhythmShahriar/tacet/measure"
)

//go:embed report.html.tmpl
var reportHTML string

var reportTmpl = template.Must(template.New("report").Funcs(template.FuncMap{"tokens": ui.Tokens, "plural": plural}).Parse(reportHTML))

// reportBar is one row with a share of a whole, drawn as a bar.
type reportBar struct {
	Label  string
	Detail string
	Value  string
	Count  int
	Pct    float64 // 0 to 100, the bar width
}

// reportCompare is one before and after line.
type reportCompare struct {
	Label, Before, After, Delta string
	Class                       string // good, bad or same
}

type reportData struct {
	Version     string
	Root        string
	Generated   string
	Startup     []reportBar
	Groups      []reportBar
	StartupSum  string
	HasBaseline bool
	BaseDate    string
	Compare     []reportCompare
	Sessions    int
	Requests    int
	PerRequest  string
	HitRate     string
	HitActive   string // main-session hit rate without idle-gap requests
	MainReqs    int
	IdleCount   int
	Misses      []reportBar
	MissCount   int
	MissTokens  string
	Subagents   int
	SubRequests int
	SubTokens   string
	SubShare    string
	SubPct      float64
	AllTokens   string
	DocReads    int
	DocSaved    string
	DocFiles    []reportBar
	Suggestions []suggestion
	Usage       *usageTables // ccusage-style tables of the last usageDays
	OnVsBefore  *onVsBefore  // main sessions before and after init, with cost
}

// missCauses explains each cache-miss cause in plain words.
var missCauses = map[string]string{
	"model-switch": "the model changed mid-session",
	"idle":         "idle for over 5 minutes, so the cache expired",
	"compact":      "the conversation was compacted",
	"other":        "the prompt or tool list changed",
}

func runReport(a *app, args []string) error {
	noOpen := false
	for _, x := range args {
		switch x {
		case "--no-open":
			noOpen = true
		default:
			return fmt.Errorf("%w: tacet report [--no-open]", errUsage)
		}
	}
	root := a.root()
	d, err := buildReport(root)
	if err != nil {
		return err
	}
	// html/template drops comments, so the header goes in front.
	var buf bytes.Buffer
	buf.WriteString("<!doctype html>\n<!-- tacet · github.com/RhythmShahriar/tacet -->\n")
	if err := reportTmpl.Execute(&buf, d); err != nil {
		return err
	}
	p := filepath.Join(config.Dir(root), "report.html")
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	_ = config.Exclude(root)
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(map[string]string{"path": p})
	}
	fmt.Fprintln(a.out, "wrote "+p)
	if !noOpen && interactive() {
		if err := openFile(p); err != nil {
			fmt.Fprintln(a.err, ui.Dim.Render("couldn't open it: "+err.Error()))
		}
	}
	return nil
}

func buildReport(root string) (*reportData, error) {
	b := loadBaseline(root)
	since := time.Time{}
	if b != nil {
		since = b.Time
	}
	s, err := doScan(root, since)
	if err != nil {
		return nil, err
	}
	u := s.Usage
	d := &reportData{
		Version: version, Root: root, Generated: time.Now().Format("2 Jan 2006 15:04"),
		StartupSum: ui.Tokens(s.Startup.Total), Sessions: u.Sessions, Requests: u.Requests,
		PerRequest: ui.Tokens(u.PerRequestMain()), HitRate: pct(u.HitRateMain()),
		HitActive: pct(u.HitRateMainActive()), MainReqs: u.MainRequests, IdleCount: len(idleMisses(u)),
	}

	groups := map[string]int{}
	for _, it := range s.Startup.Items {
		groups[it.Group] += it.Tokens
		d.Startup = append(d.Startup, reportBar{Label: it.Name, Detail: it.Group + noteSuffix(it.Note), Value: ui.Tokens(it.Tokens), Pct: share(it.Tokens, s.Startup.Total)})
	}
	for _, g := range sortedKeys(groups) {
		d.Groups = append(d.Groups, reportBar{Label: g, Value: ui.Tokens(groups[g]), Pct: share(groups[g], s.Startup.Total)})
	}

	if b != nil {
		d.HasBaseline = true
		d.BaseDate = b.Time.Local().Format("2 Jan 2006")
		d.Compare = append(d.Compare, compare("startup context", b.Startup, s.Startup.Total, ui.Tokens, true))
		if enoughData(b, u) {
			d.Compare = append(d.Compare, compare("tokens per request", b.PerRequestMain, u.PerRequestMain(), ui.Tokens, true))
			d.Compare = append(d.Compare, compareRate("cache hits", b.HitRateMain, u.HitRateMain()))
		} else {
			d.Compare = append(d.Compare, reportCompare{Label: "tokens per request", Before: ui.Tokens(b.PerRequestMain), After: notEnough(b, u), Class: "same"})
		}
		d.Compare = append(d.Compare, reportCompare{Label: "main-session requests", Before: fmt.Sprint(b.RequestsMain), After: fmt.Sprint(u.MainRequests), Class: "same"})
		d.Compare = append(d.Compare, reportCompare{Label: "sessions measured", Before: fmt.Sprint(b.Sessions), After: fmt.Sprint(u.Sessions), Class: "same"})
	}

	now := time.Now()
	cut := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, 1-usageDays)
	sessions := measure.ReadSessions(usagePaths(root, b, cut))
	d.Usage = buildUsage(sessions, cut)
	d.OnVsBefore = buildOnVsBefore(sessions, b)

	causes, counts := map[string]int{}, map[string]int{}
	for _, m := range u.Misses {
		causes[m.Cause] += m.Tokens
		counts[m.Cause]++
	}
	d.MissCount, d.MissTokens = len(u.Misses), ui.Tokens(u.MissTokens)
	for _, c := range sortedKeys(causes) {
		d.Misses = append(d.Misses, reportBar{Label: c, Detail: missCauses[c], Value: ui.Tokens(causes[c]), Count: counts[c], Pct: share(causes[c], u.MissTokens)})
	}

	all := u.Input + u.CacheWrite + u.CacheRead + u.Output
	d.Subagents, d.SubRequests, d.SubTokens, d.AllTokens = u.Subagents, u.SubRequests, ui.Tokens(u.SubTokens), ui.Tokens(all)
	d.SubPct = share(u.SubTokens, all)
	d.SubShare = fmt.Sprintf("%.0f%%", d.SubPct)

	reads, saved := docSavings(root)
	d.DocReads, d.DocSaved = reads, ui.Tokens(saved)
	d.DocFiles = docFiles(root, saved)

	_, sug := s.fixes()
	for _, f := range sug {
		d.Suggestions = append(d.Suggestions, suggestion{ID: f.Fix.ID, Title: f.Fix.Title, Why: f.Message, Tokens: saving(f)})
	}
	return d, nil
}

// docFiles is the net saving per document from .tacet/savings.jsonl,
// largest first. Follow-up reads count against their file, as in
// docSavings.
func docFiles(root string, total int) []reportBar {
	b, err := os.ReadFile(savingsPath(root))
	if err != nil {
		return nil
	}
	saved, reads := map[string]int{}, map[string]int{}
	for _, l := range strings.Split(string(b), "\n") {
		var s Saving
		if json.Unmarshal([]byte(l), &s) != nil || s.File == "" {
			continue
		}
		saved[s.File] += s.Saved
		if s.Method != "follow-up" {
			reads[s.File]++
		}
	}
	var out []reportBar
	for _, f := range sortedKeys(saved) {
		out = append(out, reportBar{Label: f, Value: ui.Tokens(saved[f]), Count: reads[f], Pct: share(saved[f], total)})
	}
	return out
}

func compare(label string, before, after int, f func(int) string, lowerBetter bool) reportCompare {
	c := reportCompare{Label: label, Before: f(before), After: f(after), Class: "same", Delta: "same"}
	if before <= 0 {
		c.Delta = ""
		return c
	}
	d := float64(after-before) / float64(before) * 100
	switch {
	case d < -0.5:
		c.Delta = fmt.Sprintf("%.0f%%", d)
		c.Class = map[bool]string{true: "good", false: "bad"}[lowerBetter]
	case d > 0.5:
		c.Delta = fmt.Sprintf("+%.0f%%", d)
		c.Class = map[bool]string{true: "bad", false: "good"}[lowerBetter]
	}
	return c
}

func compareRate(label string, before, after float64) reportCompare {
	c := reportCompare{Label: label, Before: pct(before), After: pct(after), Class: "same", Delta: "same"}
	pts := (after - before) * 100
	switch {
	case pts > 0.5:
		c.Delta, c.Class = fmt.Sprintf("+%.0f pts", pts), "good"
	case pts < -0.5:
		c.Delta, c.Class = fmt.Sprintf("%.0f pts", pts), "bad"
	}
	return c
}

func share(part, whole int) float64 {
	if whole <= 0 || part <= 0 {
		return 0
	}
	p := float64(part) / float64(whole) * 100
	if p > 100 {
		return 100
	}
	return p
}

// plural writes n and the word, with an s unless n is 1.
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func noteSuffix(n string) string {
	if n == "" {
		return ""
	}
	return " · " + n
}

// sortedKeys orders a map's keys by value, largest first, then by name.
func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}

// openFile shows p in the default browser without waiting for it.
func openFile(p string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", p) // #nosec G204 -- tacet's own report file
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", p) // #nosec G204 -- tacet's own report file
	default:
		cmd = exec.Command("xdg-open", p) // #nosec G204 -- tacet's own report file
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
