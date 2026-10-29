// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package measure works out what a project costs Claude Code: the
// context loaded before the first prompt and, from past transcripts,
// what sessions actually billed and where the prompt cache missed.
package measure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RhythmShahriar/tacet/claude"
	"github.com/RhythmShahriar/tacet/docs/lint"
	"github.com/RhythmShahriar/tacet/tokens"
)

// Typical cost of one MCP tool definition loaded upfront, from measured
// servers (about 550 to 850 tokens each).
const toolSchemaTokens = 700

// Name-only listing cost of a deferred tool when tool search is on.
const deferredToolTokens = 15

// Item is one line of the startup breakdown.
type Item struct {
	Group  string `json:"group"` // docs, skills, mcp, memory
	Name   string `json:"name"`
	Path   string `json:"path,omitempty"`
	Tokens int    `json:"tokens"`
	Note   string `json:"note,omitempty"`
}

// Startup is the context a new session loads before the first prompt,
// beyond Claude Code's own system prompt and built-in tools.
type Startup struct {
	Items      []Item `json:"items"`
	Total      int    `json:"total"`
	ToolSearch bool   `json:"tool_search"`
	Estimated  bool   `json:"estimated"`
}

// Options tune a measurement.
type Options struct {
	UserHome string         // Claude config dir; "" skips user files
	Tools    map[string]int // live tools/list counts by server, from --live
}

// MeasureStartup adds up the startup context for root.
func MeasureStartup(root string, o Options) (*Startup, *lint.Set, error) {
	st := &Startup{Estimated: true}
	set, err := lint.Load(root, o.UserHome)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range set.All() {
		if !d.Always {
			continue
		}
		st.Items = append(st.Items, Item{Group: "docs", Name: relTo(root, d.Path), Path: d.Path, Tokens: d.Tokens, Note: string(d.Kind)})
	}

	for _, sk := range claude.Skills(root) {
		if sk.ModelHidden {
			continue
		}
		desc := sk.Description
		if utf8.RuneCountInString(desc) > 1536 {
			desc = string([]rune(desc)[:1536])
		}
		name := sk.Name
		if sk.Plugin != "" {
			name = sk.Plugin + ":" + name
		}
		st.Items = append(st.Items, Item{Group: "skills", Name: name, Path: sk.Path, Tokens: tokens.Estimate(sk.Name + ": " + desc)})
	}

	files := claude.ReadSettings(root)
	v, set2 := claude.Env(files, "ENABLE_TOOL_SEARCH")
	st.ToolSearch = !set2 || v != "false"
	for _, s := range claude.MCPServers(root) {
		n, live := o.Tools[s.Name]
		it := Item{Group: "mcp", Name: s.Name, Path: s.Source}
		per := toolSchemaTokens
		if st.ToolSearch {
			per = deferredToolTokens
		}
		switch {
		case live:
			it.Tokens = n * per
			it.Note = plural(n, "tool")
		case st.ToolSearch:
			it.Tokens = 10 * per
			it.Note = "deferred by tool search; run with --live for exact tools"
		default:
			it.Tokens = 10 * per
			it.Note = "tool search off: every schema loads; run with --live for exact tools"
		}
		if s.Plugin != "" {
			it.Note = "from " + s.Plugin + "; " + it.Note
		}
		st.Items = append(st.Items, it)
	}

	if mem := filepath.Join(claude.MemoryDir(root), "MEMORY.md"); o.UserHome != "" {
		if b, err := os.ReadFile(mem); err == nil { // #nosec G304 -- Claude Code's own memory index
			st.Items = append(st.Items, Item{Group: "memory", Name: "auto memory index", Path: mem, Tokens: tokens.Estimate(loadedMemory(string(b)))})
		}
	}

	for _, it := range st.Items {
		st.Total += it.Tokens
	}
	sort.SliceStable(st.Items, func(i, j int) bool { return st.Items[i].Tokens > st.Items[j].Tokens })
	return st, set, nil
}

// loadedMemory is the part of MEMORY.md Claude Code loads: the first 200
// lines or 25KB, whichever ends first.
func loadedMemory(s string) string {
	lines := strings.SplitAfter(s, "\n")
	if len(lines) > 200 {
		lines = lines[:200]
	}
	s = strings.Join(lines, "")
	if len(s) > 25_000 {
		s = s[:25_000]
	}
	return s
}

func relTo(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, h) {
		return "~" + filepath.ToSlash(p[len(h):])
	}
	return p
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return itoa(n) + " " + w + "s"
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// Miss is a request that reprocessed most of its context.
type Miss struct {
	Session string    `json:"session"`
	Time    time.Time `json:"time"`
	Cause   string    `json:"cause"` // model-switch, idle, compact, other
	Tokens  int       `json:"tokens"`
}

// Usage is what past sessions billed.
type Usage struct {
	Sessions     int            `json:"sessions"`
	Requests     int            `json:"requests"`
	Input        int            `json:"input"`
	CacheWrite   int            `json:"cache_write"`
	CacheRead    int            `json:"cache_read"`
	Output       int            `json:"output"`
	HitRate      float64        `json:"hit_rate"`
	Misses       []Miss         `json:"misses,omitempty"`
	MissTokens   int            `json:"miss_tokens"`
	MCPCalls     map[string]int `json:"mcp_calls"`
	SkillCalls   map[string]int `json:"skill_calls"`
	DocReads     map[string]int `json:"doc_reads"` // pdf, docx, audio reads by extension
	Since        time.Time      `json:"since"`
	Subagents    int            `json:"subagents"`
	SubRequests  int            `json:"subagent_requests"`
	SubTokens    int            `json:"subagent_tokens"`
	FirstRequest int            `json:"first_request"` // average context of a session's first request
}

// PerRequest is the average input a request processed.
func (u *Usage) PerRequest() int {
	if u.Requests == 0 {
		return 0
	}
	return (u.Input + u.CacheWrite + u.CacheRead) / u.Requests
}

// MeasureUsage reads the newest limit transcripts of root (0 for all)
// from since on.
func MeasureUsage(root string, since time.Time, limit int) *Usage {
	paths := claude.Transcripts(root)
	if limit > 0 && len(paths) > limit {
		paths = paths[len(paths)-limit:]
	}
	type job struct {
		path string
		sub  bool
	}
	var jobs []job
	for _, p := range paths {
		jobs = append(jobs, job{p, false})
		for _, sp := range claude.Subagents(p) {
			jobs = append(jobs, job{sp, true})
		}
	}
	// Big projects have thousands of subagent transcripts: parse in parallel.
	sessions := make([]*claude.Session, len(jobs))
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				if s, err := claude.ReadSession(jobs[i].path); err == nil && len(s.Turns) > 0 {
					s.Subagent = jobs[i].sub
					sessions[i] = s
				}
			}
		}()
	}
	for i := range jobs {
		next <- i
	}
	close(next)
	wg.Wait()
	kept := sessions[:0]
	for _, s := range sessions {
		if s != nil {
			kept = append(kept, s)
		}
	}
	sessions = kept
	return Sum(sessions, since)
}

// Sum adds up sessions. Exported for tests and the report.
func Sum(sessions []*claude.Session, since time.Time) *Usage {
	u := &Usage{MCPCalls: map[string]int{}, SkillCalls: map[string]int{}, DocReads: map[string]int{}, Since: since}
	firsts, nfirst := 0, 0
	for _, s := range sessions {
		counted := false
		var prev *claude.Turn
		for i := range s.Turns {
			t := &s.Turns[i]
			if t.Role != "assistant" || t.Time.Before(since) {
				continue
			}
			if !counted {
				counted = true
				if s.Subagent {
					u.Subagents++
				} else {
					u.Sessions++
					firsts += t.Usage.Input + t.Usage.CacheWrite + t.Usage.CacheRead
					nfirst++
				}
			}
			if s.Subagent {
				u.SubRequests++
				u.SubTokens += t.Usage.Total()
			}
			u.Requests++
			u.Input += t.Usage.Input
			u.CacheWrite += t.Usage.CacheWrite
			u.CacheRead += t.Usage.CacheRead
			u.Output += t.Usage.Output
			for _, tool := range t.Tools {
				if strings.HasPrefix(tool, "mcp__") {
					u.MCPCalls[mcpServer(tool)]++
				}
			}
			for _, sk := range t.Skills {
				u.SkillCalls[sk]++
			}
			for _, f := range t.Files {
				if ext := strings.ToLower(filepath.Ext(f)); docExt[ext] {
					u.DocReads[ext]++
				}
			}
			if m, ok := miss(prev, t); ok {
				m.Session = s.ID
				u.Misses = append(u.Misses, m)
				u.MissTokens += m.Tokens
			}
			prev = t
		}
	}
	if in := u.Input + u.CacheWrite + u.CacheRead; in > 0 {
		u.HitRate = float64(u.CacheRead) / float64(in)
	}
	if nfirst > 0 {
		u.FirstRequest = firsts / nfirst
	}
	return u
}

var docExt = map[string]bool{".pdf": true, ".docx": true, ".pptx": true, ".xlsx": true, ".epub": true, ".mp3": true, ".wav": true, ".m4a": true, ".ogg": true}

// mcpServer is the server part of mcp__server__tool, or of
// mcp__plugin_name_server__tool for plugin servers.
func mcpServer(tool string) string {
	rest := strings.TrimPrefix(tool, "mcp__")
	if i := strings.Index(rest, "__"); i >= 0 {
		return rest[:i]
	}
	return rest
}

// miss reports a request that wrote most of a large context to the cache
// again instead of reading it and guesses why.
func miss(prev, t *claude.Turn) (Miss, bool) {
	if prev == nil {
		return Miss{}, false
	}
	ctx := t.Usage.Input + t.Usage.CacheWrite + t.Usage.CacheRead
	prevCtx := prev.Usage.Input + prev.Usage.CacheWrite + prev.Usage.CacheRead
	if ctx < 10_000 || t.Usage.CacheRead > ctx/2 {
		return Miss{}, false
	}
	m := Miss{Time: t.Time, Tokens: t.Usage.CacheWrite + t.Usage.Input}
	switch {
	case t.Model != "" && prev.Model != "" && t.Model != prev.Model:
		m.Cause = "model-switch"
	case t.Time.Sub(prev.Time) > 5*time.Minute:
		m.Cause = "idle"
	case ctx < prevCtx/2:
		m.Cause = "compact"
	default:
		m.Cause = "other"
	}
	return m, true
}
