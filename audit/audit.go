// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package audit checks a project's Claude Code setup for context that
// loads on every request without paying for itself, and plans the fix.
package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/RhythmShahriar/tacet/claude"
	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/fix"
	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/RhythmShahriar/tacet/measure"
	"github.com/RhythmShahriar/tacet/tokens"
)

// MinSessions is how many past sessions an "unused" finding needs.
const MinSessions = 5

// Input is what the audit looks at.
type Input struct {
	Root    string
	Startup *measure.Startup
	Usage   *measure.Usage
}

// LocalSettings is where tacet writes per-person settings: Claude Code
// keeps settings.local.json out of git for you.
func LocalSettings(root string) string {
	return filepath.Join(root, ".claude", "settings.local.json")
}

// Run returns every finding, ranked by tokens saved.
func Run(in Input) []core.Finding {
	var out []core.Finding
	out = append(out, heavyReads(in.Root, claude.Denies(claude.ReadSettings(in.Root)))...)
	out = append(out, toolSearch(in)...)
	out = append(out, unusedMCP(in)...)
	out = append(out, unusedSkills(in)...)
	out = append(out, longSkills(in.Root)...)
	out = append(out, cacheMisses(in)...)
	out = append(out, subagentShare(in)...)
	out = append(out, memoryIndex(in.Root)...)
	out = append(out, agentsHook(in.Root)...)
	out = append(out, attribution(in.Root)...)
	core.Rank(out)
	return out
}

// heavy is a path pattern the agent never needs to read whole.
type heavy struct {
	glob  string // checked against the project
	rule  string // the deny rule
	label string
}

var heavyPaths = []heavy{
	{"package-lock.json", "Read(./**/package-lock.json)", "npm lockfile"},
	{"pnpm-lock.yaml", "Read(./**/pnpm-lock.yaml)", "pnpm lockfile"},
	{"yarn.lock", "Read(./**/yarn.lock)", "yarn lockfile"},
	{"bun.lockb", "Read(./**/bun.lockb)", "bun lockfile"},
	{"composer.lock", "Read(./**/composer.lock)", "composer lockfile"},
	{"Cargo.lock", "Read(./**/Cargo.lock)", "cargo lockfile"},
	{"poetry.lock", "Read(./**/poetry.lock)", "poetry lockfile"},
	{"uv.lock", "Read(./**/uv.lock)", "uv lockfile"},
	{"go.sum", "Read(./**/go.sum)", "go checksums"},
	{"node_modules", "Read(./**/node_modules/**)", "node_modules"},
	{"vendor", "Read(./vendor/**)", "vendored code"},
	{"dist", "Read(./dist/**)", "build output"},
	{"build", "Read(./build/**)", "build output"},
	{".next", "Read(./**/.next/**)", "Next.js build"},
	{"coverage", "Read(./coverage/**)", "coverage output"},
	{"target", "Read(./target/**)", "build output"},
}

// heavyReads plans deny rules for lockfiles and build output the project
// has and doesn't deny yet. Reading one costs thousands of tokens and
// teaches the agent nothing; it still sees them in listings and git.
func heavyReads(root string, denies []string) []core.Finding {
	have := map[string]bool{}
	for _, d := range denies {
		have[d] = true
	}
	var rules, labels []string
	size := int64(0)
	for _, h := range heavyPaths {
		fi, err := os.Stat(filepath.Join(root, h.glob))
		if err != nil || have[h.rule] {
			continue
		}
		if h.glob == "vendor" || h.glob == "build" || h.glob == "target" {
			// Only build-tool folders: a hand-written vendor/ or build/
			// can be source the agent should read.
			if !looksGenerated(root, h.glob) {
				continue
			}
		}
		rules = append(rules, h.rule)
		labels = append(labels, h.label)
		if !fi.IsDir() {
			size += fi.Size()
		}
	}
	if len(rules) == 0 {
		return nil
	}
	saved := int(size / 4)
	return []core.Finding{{
		Rule: "reads/heavy", Severity: core.Medium,
		Message: fmt.Sprintf("Claude can read %s whole", strings.Join(dedupe(labels), ", ")),
		Hint:    "deny reads of them; listings, git and your tools still see them",
		Tokens:  0,
		Fix: &core.Fix{
			ID: "deny-heavy-reads", Title: fmt.Sprintf("Deny reads of %d generated paths", len(rules)), Safe: true,
			Edits:  []core.Edit{fix.AddDeny(LocalSettings(root), rules...)},
			Tokens: saved,
		},
	}}
}

func looksGenerated(root, dir string) bool {
	markers := map[string][]string{
		"vendor": {"composer.json", "go.mod", "Gemfile"},
		"build":  {"package.json", "build.gradle", "CMakeLists.txt", "pyproject.toml"},
		"target": {"Cargo.toml", "pom.xml"},
	}
	for _, m := range markers[dir] {
		if _, err := os.Stat(filepath.Join(root, m)); err == nil {
			return true
		}
	}
	return false
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func toolSearch(in Input) []core.Finding {
	if in.Startup == nil || in.Startup.ToolSearch {
		return nil
	}
	mcp := 0
	for _, it := range in.Startup.Items {
		if it.Group == "mcp" {
			mcp += it.Tokens
		}
	}
	if mcp == 0 {
		return nil
	}
	return []core.Finding{{
		Rule: "mcp/tool-search-off", Severity: core.High,
		Message: "ENABLE_TOOL_SEARCH=false loads every MCP tool schema on every request",
		Hint:    "remove it so tools load only when used (Anthropic measured 85% fewer tool tokens)",
		Tokens:  mcp * 9 / 10,
		Fix: &core.Fix{
			ID: "tool-search-on", Title: "Turn MCP tool search back on", Safe: false,
			Edits: []core.Edit{fix.JSONEdit(LocalSettings(in.Root), func(m map[string]any) {
				env, _ := m["env"].(map[string]any)
				if env == nil {
					env = map[string]any{}
					m["env"] = env
				}
				env["ENABLE_TOOL_SEARCH"] = "true"
			})},
		},
	}}
}

// unusedMCP flags servers no recent session called. A deny rule on the
// whole server takes its tools out of context; removing the deny brings
// them back.
func unusedMCP(in Input) []core.Finding {
	if in.Usage == nil || in.Usage.Sessions < MinSessions || in.Startup == nil {
		return nil
	}
	var out []core.Finding
	for _, it := range in.Startup.Items {
		if it.Group != "mcp" {
			continue
		}
		key := normServer(it.Name)
		used := 0
		for s, n := range in.Usage.MCPCalls {
			if normServer(s) == key || strings.HasSuffix(normServer(s), "_"+key) {
				used += n
			}
		}
		if used > 0 {
			continue
		}
		out = append(out, core.Finding{
			Rule: "mcp/unused", Severity: core.Medium,
			Message: fmt.Sprintf("MCP server %q wasn't used in the last %d sessions", it.Name, in.Usage.Sessions),
			Hint:    "turn its tools off for this project; tacet undo brings them back",
			Tokens:  it.Tokens,
			Fix: &core.Fix{
				ID: "mcp-off-" + it.Name, Title: "Turn off MCP server " + it.Name, Safe: false,
				Edits: []core.Edit{fix.AddDeny(LocalSettings(in.Root), "mcp__"+it.Name+"__*")},
			},
		})
	}
	return out
}

func normServer(s string) string {
	return strings.ToLower(strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(s))
}

func unusedSkills(in Input) []core.Finding {
	if in.Usage == nil || in.Usage.Sessions < MinSessions || in.Startup == nil {
		return nil
	}
	var out []core.Finding
	for _, it := range in.Startup.Items {
		if it.Group != "skills" || strings.Contains(it.Name, ":") || in.Usage.SkillCalls[it.Name] > 0 {
			continue
		}
		path := it.Path
		out = append(out, core.Finding{
			Rule: "skills/unused", Severity: core.Low, Path: path,
			Message: fmt.Sprintf("skill %q wasn't used in the last %d sessions", it.Name, in.Usage.Sessions),
			Hint:    "keep it callable as /" + it.Name + " but out of the listing",
			Tokens:  it.Tokens,
			Fix: &core.Fix{
				ID: "skill-hide-" + it.Name, Title: "Hide skill " + it.Name + " from the model (still /" + it.Name + ")", Safe: false,
				Edits: []core.Edit{{Path: path, Change: func(cur []byte) ([]byte, error) { return setFrontmatter(cur, "disable-model-invocation", "true"), nil }}},
			},
		})
	}
	return out
}

// setFrontmatter sets key in a --- block, adding the block when missing.
func setFrontmatter(b []byte, key, val string) []byte {
	s := string(b)
	if !strings.HasPrefix(s, "---\n") {
		return []byte("---\n" + key + ": " + val + "\n---\n" + s)
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return b
	}
	head, rest := s[4:4+end], s[4+end:]
	lines := strings.Split(head, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, key+":") {
			lines[i] = key + ": " + val
			return []byte("---\n" + strings.Join(lines, "\n") + rest)
		}
	}
	return []byte("---\n" + head + "\n" + key + ": " + val + rest)
}

func longSkills(root string) []core.Finding {
	var out []core.Finding
	for _, sk := range claude.Skills(root) {
		if sk.ModelHidden || sk.Plugin != "" {
			continue
		}
		n := len([]rune(sk.Description))
		switch {
		case n > 1536:
			out = append(out, core.Finding{
				Rule: "skills/description", Severity: core.Medium, Path: sk.Path,
				Message: fmt.Sprintf("skill %q has a %d-character description; Claude Code cuts it at 1,536", sk.Name, n),
				Hint:    "keep the trigger words first and move detail into the body",
			})
		case n > 500:
			out = append(out, core.Finding{
				Rule: "skills/description", Severity: core.Info, Path: sk.Path,
				Message: fmt.Sprintf("skill %q's %d-character description loads every session", sk.Name, n),
				Hint:    "a sentence or two is enough to trigger it; the body loads on use",
				Tokens:  tokens.Estimate(sk.Description) - 60,
			})
		}
	}
	return out
}

func cacheMisses(in Input) []core.Finding {
	if in.Usage == nil || len(in.Usage.Misses) == 0 {
		return nil
	}
	by := map[string]int{}
	tok := map[string]int{}
	for _, m := range in.Usage.Misses {
		by[m.Cause]++
		tok[m.Cause] += m.Tokens
	}
	tips := map[string]string{
		"model-switch": "switching models mid-session rebuilds the whole cache; pick the model before you start",
		"idle":         "a break past the cache lifetime reprocesses everything; /clear or start fresh after a long pause",
		"compact":      "compaction rewrites the context; /clear between unrelated tasks is free",
		"other":        "a changed tool list or settings mid-session; avoid toggling MCP servers or plugins during a task",
	}
	causes := make([]string, 0, len(by))
	for c := range by {
		causes = append(causes, c)
	}
	sort.Slice(causes, func(i, j int) bool { return tok[causes[i]] > tok[causes[j]] })
	var out []core.Finding
	for _, c := range causes {
		out = append(out, core.Finding{
			Rule: "cache/" + c, Severity: core.Info,
			Message: fmt.Sprintf("%d cache misses from %s reprocessed %s tokens", by[c], c, short(tok[c])),
			Hint:    tips[c],
		})
	}
	return out
}

func short(n int) string { return ui.Tokens(n) }

// subagentShare reports when subagents spend most of the tokens: the
// biggest lever is then how many run and on which model.
func subagentShare(in Input) []core.Finding {
	u := in.Usage
	if u == nil || u.SubTokens == 0 {
		return nil
	}
	all := u.Input + u.CacheWrite + u.CacheRead + u.Output
	if all == 0 || u.SubTokens*2 < all {
		return nil
	}
	share := float64(u.SubTokens) / float64(all) * 100
	return []core.Finding{{
		Rule: "usage/subagents", Severity: core.Medium,
		Message: fmt.Sprintf("subagents used %.0f%% of all tokens (%d subagents, %s tokens)", share, u.Subagents, short(u.SubTokens)),
		Hint:    "run search and review agents on a smaller model (model: haiku in the agent's frontmatter) and fewer at once; each one re-reads its own context",
	}}
}

func memoryIndex(root string) []core.Finding {
	p := filepath.Join(claude.MemoryDir(root), "MEMORY.md")
	b, err := os.ReadFile(p) // #nosec G304 -- Claude Code's memory index
	if err != nil {
		return nil
	}
	lines := strings.Count(string(b), "\n")
	if lines <= 200 && len(b) <= 25_000 {
		return nil
	}
	return []core.Finding{{
		Rule: "memory/index", Severity: core.Medium, Path: p,
		Message: fmt.Sprintf("MEMORY.md is %d lines; Claude Code loads only the first 200 lines or 25KB", lines),
		Hint:    "keep one line per memory in the index and the detail in topic files",
	}}
}

// agentsHook flags a SessionStart hook that prints AGENTS.md: Claude Code
// can already read it, so it loads twice.
func agentsHook(root string) []core.Finding {
	var out []core.Finding
	for _, f := range claude.ReadSettings(root) {
		hooks, _ := f.Data["hooks"].(map[string]any)
		for _, cmd := range commands(hooks["SessionStart"]) {
			if strings.Contains(cmd, "AGENTS.md") {
				out = append(out, core.Finding{
					Rule: "agents/hook", Severity: core.Medium, Path: f.Path,
					Message: "a SessionStart hook prints AGENTS.md, which then loads twice",
					Hint:    "drop the hook and add @AGENTS.md to CLAUDE.md",
				})
			}
		}
	}
	return out
}

func commands(v any) []string {
	var out []string
	groups, _ := v.([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		hs, _ := gm["hooks"].([]any)
		for _, h := range hs {
			hm, _ := h.(map[string]any)
			if c, ok := hm["command"].(string); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

// attribution offers to turn off Claude Code's commit and pull request
// credit lines for people who want a clean history. Never applied
// without a yes.
func attribution(root string) []core.Finding {
	for _, f := range claude.ReadSettings(root) {
		if _, ok := f.Data["attribution"]; ok {
			return nil
		}
		if _, ok := f.Data["includeCoAuthoredBy"]; ok {
			return nil
		}
	}
	return []core.Finding{{
		Rule: "git/attribution", Severity: core.Info,
		Message: "Claude Code adds its own credit lines to commits and pull requests by default",
		Hint:    "optional: turn them off for this project",
		Fix: &core.Fix{
			ID: "attribution-off", Title: "Turn off commit and PR credit lines", Safe: false,
			Edits: []core.Edit{fix.JSONEdit(LocalSettings(root), func(m map[string]any) {
				m["attribution"] = map[string]any{"commit": "", "pr": ""}
			})},
		},
	}}
}
