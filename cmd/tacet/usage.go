// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/claude"
	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/RhythmShahriar/tacet/measure"
)

// usageDays is how far back the report's usage tables look.
const usageDays = 14

// price is the list price of one model in US dollars per million tokens.
type price struct {
	match                string // a substring of the model id
	in, write, read, out float64
	ratio                float64 // every rate times ratio past longAt input tokens
	longAt               int
}

// prices are Anthropic's list prices per million tokens, first match
// wins, so versions come before their family. Cache writes use the
// 5-minute rate. Source: https://claude.com/pricing (was
// https://www.anthropic.com/pricing), October 2026. The report labels
// every dollar figure as an estimate.
var prices = []price{
	{match: "fable-5-1", in: 10, write: 12.5, read: 0.25, out: 50},
	{match: "fable-5", in: 10, write: 12.5, read: 1, out: 50},
	{match: "opus-5-5", in: 4, write: 5, read: 0.20, out: 20},
	{match: "opus-5", in: 5, write: 6.25, read: 0.50, out: 25},
	{match: "opus-4", in: 5, write: 6.25, read: 0.50, out: 25},
	{match: "sonnet-5-5", in: 2, write: 2.5, read: 0.10, out: 10},
	{match: "sonnet-5", in: 2, write: 2.5, read: 0.20, out: 10},
	{match: "sonnet-4", in: 3, write: 3.75, read: 0.30, out: 15},
	// Haiku 5.5 bills five times as much once a prompt passes 100k tokens.
	{match: "haiku-5-5", in: 0.10, write: 0.125, read: 0.01, out: 0.50, ratio: 5, longAt: 100_000},
	{match: "haiku-4", in: 1, write: 1.25, read: 0.10, out: 5},
	// An unlisted version is priced as its family's latest.
	{match: "fable", in: 10, write: 12.5, read: 0.25, out: 50},
	{match: "opus", in: 4, write: 5, read: 0.20, out: 20},
	{match: "sonnet", in: 2, write: 2.5, read: 0.10, out: 10},
	{match: "haiku", in: 0.10, write: 0.125, read: 0.01, out: 0.50, ratio: 5, longAt: 100_000},
}

// cost estimates what one request billed; ok is false for a model the
// table doesn't know.
func cost(model string, u claude.Usage) (usd float64, ok bool) {
	m := strings.ToLower(model)
	for _, p := range prices {
		if !strings.Contains(m, p.match) {
			continue
		}
		k := 1.0
		if p.longAt > 0 && u.Input+u.CacheWrite+u.CacheRead > p.longAt {
			k = p.ratio
		}
		return k * (float64(u.Input)*p.in + float64(u.CacheWrite)*p.write + float64(u.CacheRead)*p.read + float64(u.Output)*p.out) / 1e6, true
	}
	return 0, false
}

// usd formats an estimate: cents below a dollar keep one more digit.
func usd(v float64) string {
	switch {
	case v <= 0:
		return "$0"
	case v < 0.01:
		return fmt.Sprintf("$%.4f", v)
	case v < 10:
		return fmt.Sprintf("$%.2f", v)
	}
	return fmt.Sprintf("$%.0f", v)
}

// usageRow is one line of a usage table.
type usageRow struct {
	Label                          string
	Requests                       int
	Input, Write, Read, Out, Total string
	Cost                           string
	Pct                            float64 // share of the column the bar shows
	Share                          string
}

// usageTables is the report's ccusage-style view of the last days.
type usageTables struct {
	Days     []usageRow
	DayTotal usageRow
	Models   []usageRow
	Tools    []usageRow
	Who      []usageRow
	Unpriced []string // models without a list price, left out of the cost
	Since    string
}

// sums adds requests up for one row.
type sums struct {
	reqs                    int
	in, write, read, out    int
	usd                     float64
	calls, resultTokens     int
	hasResults, unpricedAny bool
}

func (s *sums) add(model string, u claude.Usage) {
	s.reqs++
	s.in += u.Input
	s.write += u.CacheWrite
	s.read += u.CacheRead
	s.out += u.Output
	if c, ok := cost(model, u); ok {
		s.usd += c
	} else {
		s.unpricedAny = true
	}
}

func (s *sums) total() int { return s.in + s.write + s.read + s.out }

func (s *sums) row(label string, whole float64) usageRow {
	r := usageRow{Label: label, Requests: s.reqs, Input: ui.Tokens(s.in), Write: ui.Tokens(s.write),
		Read: ui.Tokens(s.read), Out: ui.Tokens(s.out), Total: ui.Tokens(s.total()), Cost: usd(s.usd)}
	if whole > 0 {
		r.Pct = s.usd / whole * 100
		r.Share = fmt.Sprintf("%.0f%%", r.Pct)
	}
	return r
}

// buildUsage reads the requests from since on into the tables.
func buildUsage(sessions []*claude.Session, since time.Time) *usageTables {
	days, models, who := map[string]*sums{}, map[string]*sums{}, map[string]*sums{}
	tools := map[string]*sums{}
	unpriced := map[string]bool{}
	all := &sums{}
	get := func(m map[string]*sums, k string) *sums {
		if m[k] == nil {
			m[k] = &sums{}
		}
		return m[k]
	}
	for _, s := range sessions {
		names := map[string]string{} // tool_use id to tool name
		for i := range s.Turns {
			t := &s.Turns[i]
			if t.Time.Before(since) {
				continue
			}
			if t.Role == "user" {
				for id, n := range t.Results {
					if name := names[id]; name != "" {
						x := get(tools, name)
						x.resultTokens += (n*2 + 6) / 7 // tokens.Estimate on plain text
						x.hasResults = true
					}
				}
				continue
			}
			for j, name := range t.Tools {
				get(tools, name).calls++
				if j < len(t.ToolIDs) && t.ToolIDs[j] != "" {
					names[t.ToolIDs[j]] = name
				}
			}
			if t.Usage.Total() == 0 {
				continue // synthetic or interrupted turns bill nothing
			}
			if _, ok := cost(t.Model, t.Usage); !ok {
				unpriced[t.Model] = true
			}
			all.add(t.Model, t.Usage)
			get(days, t.Time.Local().Format("2006-01-02")).add(t.Model, t.Usage)
			get(models, modelName(t.Model)).add(t.Model, t.Usage)
			k := "main sessions"
			if s.Subagent {
				k = "subagents"
			}
			get(who, k).add(t.Model, t.Usage)
		}
	}
	if all.reqs == 0 && len(tools) == 0 {
		return nil
	}
	u := &usageTables{Since: since.Local().Format("2 Jan 2006")}
	dayKeys := make([]string, 0, len(days))
	for k := range days {
		dayKeys = append(dayKeys, k)
	}
	sort.Strings(dayKeys)
	for _, k := range dayKeys {
		d, _ := time.ParseInLocation("2006-01-02", k, time.Local)
		u.Days = append(u.Days, days[k].row(d.Format("Mon 2 Jan"), all.usd))
	}
	u.DayTotal = all.row("total", 0)
	for _, k := range byCost(models) {
		u.Models = append(u.Models, models[k].row(k, all.usd))
	}
	for _, k := range byCost(who) {
		u.Who = append(u.Who, who[k].row(k, all.usd))
	}
	calls := map[string]int{}
	totalCalls := 0
	for k, v := range tools {
		calls[k] = v.calls
		totalCalls += v.calls
	}
	for i, k := range sortedKeys(calls) {
		if i == 10 || calls[k] == 0 {
			break
		}
		x := tools[k]
		r := usageRow{Label: k, Requests: x.calls, Total: "–", Pct: share(x.calls, totalCalls)}
		r.Share = fmt.Sprintf("%.0f%%", r.Pct)
		if x.hasResults {
			r.Total = ui.Tokens(x.resultTokens)
		}
		u.Tools = append(u.Tools, r)
	}
	for m := range unpriced {
		if m == "" {
			m = "unknown"
		}
		u.Unpriced = append(u.Unpriced, m)
	}
	sort.Strings(u.Unpriced)
	return u
}

// modelName drops the claude- prefix and a date suffix: claude-opus-4-5-20251101 is opus-4-5.
func modelName(m string) string {
	if m == "" {
		return "unknown"
	}
	m = strings.TrimPrefix(m, "claude-")
	if i := strings.LastIndex(m, "-"); i > 0 && len(m)-i == 9 && strings.Trim(m[i+1:], "0123456789") == "" {
		m = m[:i]
	}
	return m
}

// byCost orders keys by estimated cost, then by tokens, largest first.
func byCost(m map[string]*sums) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := m[keys[i]], m[keys[j]]
		if a.usd != b.usd {
			return a.usd > b.usd
		}
		if a.total() != b.total() {
			return a.total() > b.total()
		}
		return keys[i] < keys[j]
	})
	return keys
}

// onVsBefore is the main-session comparison against the baseline,
// estimated in dollars as well as tokens.
type onVsBefore struct {
	Enough  bool
	Reason  string // why there is no comparison yet
	Rows    []reportCompare
	Saved   string // estimated dollars saved over the requests since init
	Since   int    // main-session requests since init
	Partial bool   // some requests had no list price
}

// buildOnVsBefore compares main-session requests before b.Time with the
// ones after it. It shows only with minRequests after, as status does.
func buildOnVsBefore(sessions []*claude.Session, b *Baseline) *onVsBefore {
	if b == nil {
		return nil
	}
	var before, after sums
	for _, s := range sessions {
		if s.Subagent {
			continue
		}
		for _, t := range s.Turns {
			if t.Role != "assistant" || t.Usage.Total() == 0 {
				continue
			}
			if t.Time.Before(b.Time) {
				before.add(t.Model, t.Usage)
			} else {
				after.add(t.Model, t.Usage)
			}
		}
	}
	o := &onVsBefore{Since: after.reqs, Partial: before.unpricedAny || after.unpricedAny}
	switch {
	case !enoughData(b, &measure.Usage{MainRequests: after.reqs}):
		o.Reason = notEnough(b, &measure.Usage{MainRequests: after.reqs})
		return o
	case before.reqs == 0:
		o.Reason = "nothing to compare: the transcripts from before init are gone"
		return o
	}
	o.Enough = true
	per := func(s *sums, n int) int { return n / s.reqs }
	o.Rows = append(o.Rows,
		compare("tokens per request", per(&before, before.in+before.write+before.read), per(&after, after.in+after.write+after.read), ui.Tokens, true),
		compareRate("cache hits", rate(before.read, before.in+before.write+before.read), rate(after.read, after.in+after.write+after.read)),
		compare("output per request", per(&before, before.out), per(&after, after.out), ui.Tokens, true),
	)
	bc, ac := before.usd/float64(before.reqs), after.usd/float64(after.reqs)
	c := compare("cost per request", int(bc*1e6), int(ac*1e6), func(n int) string { return usd(float64(n) / 1e6) }, true)
	o.Rows = append(o.Rows, c,
		reportCompare{Label: "main-session requests", Before: fmt.Sprint(before.reqs), After: fmt.Sprint(after.reqs), Class: "same"})
	if d := (bc - ac) * float64(after.reqs); d > 0 {
		o.Saved = usd(d)
	}
	return o
}

func rate(part, whole int) float64 {
	if whole <= 0 {
		return 0
	}
	return float64(part) / float64(whole)
}

// usagePaths are the transcripts the usage tables and the comparison
// read: those written in the last usageDays or since the baseline and
// the newest recentSessions from before the baseline.
func usagePaths(root string, b *Baseline, cut time.Time) []string {
	all := claude.Transcripts(root) // oldest first
	from := cut
	if b != nil && b.Time.Before(from) {
		from = b.Time
	}
	var out, before []string
	for _, p := range all {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		switch {
		case !fi.ModTime().Before(from):
			out = append(out, p)
		case b != nil && fi.ModTime().Before(b.Time):
			before = append(before, p)
		}
	}
	if len(before) > recentSessions {
		before = before[len(before)-recentSessions:]
	}
	return append(before, out...)
}
