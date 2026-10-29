// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/tokens"
)

// Limits the size rules use.
const (
	MaxLines         = 200      // Anthropic's guidance per CLAUDE.md file
	CodexLimit       = 32 << 10 // Codex stops reading AGENTS.md here
	MinProcedureStep = 5        // numbered steps before a list reads as a procedure
)

// Check runs every rule over the set.
func Check(s *Set) []core.Finding {
	var out []core.Finding
	all := s.All()
	for _, d := range all {
		out = append(out, checkSize(s, d)...)
		out = append(out, checkImports(s, d)...)
		out = append(out, checkStale(s, d)...)
		out = append(out, checkTree(s, d)...)
		out = append(out, checkProcedure(s, d)...)
		out = append(out, checkVague(s, d)...)
		out = append(out, checkNotes(s, d)...)
		out = append(out, checkScoped(s, d)...)
	}
	out = append(out, checkShadowedAgents(s)...)
	out = append(out, checkDuplicates(s, all)...)
	out = append(out, checkContradictions(s, all)...)
	measureFixes(s, out)
	core.Rank(out)
	return out
}

func rel(s *Set, p string) string {
	if r, err := filepath.Rel(s.Root, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return p
}

func checkSize(s *Set, d *Doc) []core.Finding {
	var out []core.Finding
	lines := strings.Count(d.Body, "\n") + 1
	if d.Always && lines > MaxLines {
		out = append(out, core.Finding{
			Rule: "size/lines", Severity: core.Medium, Path: rel(s, d.Path),
			Message: fmt.Sprintf("%d lines loaded every session (aim for under %d)", lines, MaxLines),
			Hint:    "move procedures into skills and folder-only rules into .claude/rules with paths:",
		})
	}
	if filepath.Base(d.Path) == "AGENTS.md" && len(d.Body) > CodexLimit {
		out = append(out, core.Finding{
			Rule: "size/vendor", Severity: core.High, Path: rel(s, d.Path),
			Message: fmt.Sprintf("%d KiB; Codex ignores everything after 32 KiB", len(d.Body)>>10),
		})
	}
	return out
}

func checkImports(s *Set, d *Doc) []core.Finding {
	var out []core.Finding
	for _, r := range d.Missing {
		out = append(out, core.Finding{
			Rule: "import/missing", Severity: core.High, Path: rel(s, d.Path), Line: r.Line,
			Message: "@" + r.Target + " doesn't exist, so it silently loads nothing",
		})
	}
	for _, r := range d.TooDeep {
		out = append(out, core.Finding{
			Rule: "import/depth", Severity: core.Medium, Path: rel(s, d.Path), Line: r.Line,
			Message: fmt.Sprintf("@%s is more than %d imports deep and never loads", r.Target, MaxImportDepth),
		})
	}
	return out
}

func checkShadowedAgents(s *Set) []core.Finding {
	agents := filepath.Join(s.Root, "AGENTS.md")
	if _, err := os.Stat(agents); err != nil {
		return nil
	}
	for _, d := range s.All() {
		if d.Path == agents {
			return nil // loaded, directly or through an import
		}
	}
	var claudeMD string
	for _, d := range s.Docs {
		if d.Kind == Project && filepath.Dir(d.Path) == s.Root {
			claudeMD = d.Path
		}
	}
	f := core.Finding{
		Rule: "agents/shadowed", Severity: core.High, Path: "AGENTS.md",
		Message: "Claude Code skips AGENTS.md here because a CLAUDE file exists",
		Hint:    "import it: add @AGENTS.md to CLAUDE.md",
	}
	if claudeMD != "" {
		// A Change, not Old/New, so it composes with other fixes to CLAUDE.md.
		f.Fix = &core.Fix{
			// Not safe: it makes Claude follow rules it was skipping, but it
			// loads AGENTS.md every session, which adds tokens (measured
			// +600 to +3,500 on real projects). The person decides.
			ID: "agents-bridge", Title: "Import AGENTS.md from CLAUDE.md (adds its tokens to every session)", Safe: false,
			Edits: []core.Edit{{Path: claudeMD, Change: func(cur []byte) ([]byte, error) {
				for _, r := range Imports(string(cur)) {
					if r.Target == "AGENTS.md" || r.Target == "./AGENTS.md" {
						return cur, nil
					}
				}
				return append([]byte("@AGENTS.md\n\n"), cur...), nil
			}}},
		}
	}
	return []core.Finding{f}
}

var pathRefRe = regexp.MustCompile("`((?:\\./)?[A-Za-z0-9_.\\-]+(?:/[A-Za-z0-9_.\\-]+)+/?)`")

// checkStale flags backticked repo paths that no longer exist and make,
// npm, pnpm, yarn and just targets that aren't defined.
func checkStale(s *Set, d *Doc) []core.Finding {
	if d.Kind == User {
		return nil
	}
	var out []core.Finding
	base := filepath.Dir(d.Path)
	if d.Kind == Rule {
		base = s.Root
	}
	targets := commandTargets(s.Root)
	for n, line := range strings.Split(d.Body, "\n") {
		for _, m := range pathRefRe.FindAllStringSubmatch(line, -1) {
			p := m[1]
			if strings.ContainsAny(p, "*<>{}$") || strings.Contains(p, "://") || strings.HasPrefix(p, "~") {
				continue
			}
			if !exists(filepath.Join(base, p)) && !exists(filepath.Join(s.Root, p)) {
				out = append(out, core.Finding{
					Rule: "stale/path", Severity: core.Medium, Path: rel(s, d.Path), Line: n + 1,
					Message: p + " doesn't exist; the agent will look for it and waste turns",
					Hint:    "point it at the new path or delete the line; tacet can't know where it went",
				})
			}
		}
		for _, c := range commandRefs(line) {
			if set, ok := targets[c.tool]; ok && !set[c.target] {
				out = append(out, core.Finding{
					Rule: "stale/command", Severity: core.Medium, Path: rel(s, d.Path), Line: n + 1,
					Message: fmt.Sprintf("%s %s isn't defined in %s", c.tool, c.target, c.source()),
					Hint:    "name a target that exists, or add this one",
				})
			}
		}
	}
	return out
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

type cmdRef struct{ tool, target string }

func (c cmdRef) source() string {
	switch c.tool {
	case "make":
		return "the Makefile"
	case "just":
		return "the justfile"
	}
	return "package.json scripts"
}

var cmdRe = regexp.MustCompile(`\b(make|just|npm run|pnpm run|pnpm|yarn|bun run)\s+([a-z][a-z0-9:_\-]*)`)

func commandRefs(line string) []cmdRef {
	var out []cmdRef
	for _, m := range cmdRe.FindAllStringSubmatch(line, -1) {
		tool := m[1]
		if tool == "make" && proseAfterMake[m[2]] {
			continue
		}
		switch tool {
		case "npm run", "pnpm run", "pnpm", "yarn", "bun run":
			if builtinPM[m[2]] {
				continue
			}
			tool = "npm"
		}
		out = append(out, cmdRef{tool, m[2]})
	}
	return out
}

// Words that follow "make" in prose, not targets.
var proseAfterMake = map[string]bool{"it": true, "sure": true, "the": true, "a": true, "an": true, "this": true, "that": true, "them": true, "sense": true, "changes": true, "use": true}

// Package manager subcommands that aren't scripts.
var builtinPM = map[string]bool{"install": true, "add": true, "remove": true, "i": true, "exec": true, "dlx": true, "test": true, "run": true, "ci": true, "up": true, "update": true, "why": true, "list": true, "init": true, "create": true, "dev": false}

var makeTargetRe = regexp.MustCompile(`(?m)^([A-Za-z0-9][A-Za-z0-9_.\-]*)\s*:([^=]|$)`)
var justRecipeRe = regexp.MustCompile(`(?m)^@?([A-Za-z0-9][A-Za-z0-9_\-]*)[^\n:=]*:([^=]|$)`)
var scriptsRe = regexp.MustCompile(`(?s)"scripts"\s*:\s*\{(.*?)\}`)
var scriptKeyRe = regexp.MustCompile(`"([^"]+)"\s*:`)

// Targets is the make, just and npm targets root defines, keyed by
// tool; a tool with no definition file is absent.
func Targets(root string) map[string]map[string]bool { return commandTargets(root) }

// commandTargets reads the targets a project defines. A tool with no
// definition file is absent, so its commands aren't checked.
func commandTargets(root string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(root, name)) // #nosec G304 -- project build files
		return string(b)
	}
	for _, name := range []string{"Makefile", "makefile", "GNUmakefile"} {
		if b := read(name); b != "" {
			set := map[string]bool{}
			for _, m := range makeTargetRe.FindAllStringSubmatch(b, -1) {
				set[m[1]] = true
			}
			out["make"] = set
		}
	}
	for _, name := range []string{"justfile", "Justfile", ".justfile"} {
		if b := read(name); b != "" {
			set := map[string]bool{}
			for _, m := range justRecipeRe.FindAllStringSubmatch(b, -1) {
				set[m[1]] = true
			}
			out["just"] = set
		}
	}
	if b := read("package.json"); b != "" {
		set := map[string]bool{}
		if m := scriptsRe.FindStringSubmatch(b); m != nil {
			for _, k := range scriptKeyRe.FindAllStringSubmatch(m[1], -1) {
				set[k[1]] = true
			}
		}
		out["npm"] = set
	}
	return out
}

// checkTree flags directory listings: the agent can list a folder when it
// needs to, and research found overviews don't help.
func checkTree(s *Set, d *Doc) []core.Finding {
	var out []core.Finding
	for _, b := range fences(d.Body) {
		if strings.Contains(b.text, "├──") || strings.Contains(b.text, "└──") {
			out = append(out, core.Finding{
				Rule: "derivable/tree", Severity: core.Low, Path: rel(s, d.Path), Line: b.line,
				Message: "a directory tree the agent can list itself",
				Hint:    "delete it, or keep a short codemap in ARCHITECTURE.md",
				Tokens:  tokens.Estimate(b.text),
				Fix:     treeFix(s, d, b),
			})
		}
	}
	return out
}

type fence struct {
	line int // opening fence, 1-based
	end  int // closing fence, 1-based
	text string
}

func fences(body string) []fence {
	var out []fence
	var cur *fence
	for n, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			if cur == nil {
				cur = &fence{line: n + 1}
			} else {
				cur.end = n + 1
				out = append(out, *cur)
				cur = nil
			}
			continue
		}
		if cur != nil {
			cur.text += l + "\n"
		}
	}
	return out
}

var stepRe = regexp.MustCompile(`^\s*(\d+)[.)]\s+\S`)

// checkProcedure flags long numbered procedures in always-loaded files:
// as a skill only its one-line description stays in context.
func checkProcedure(s *Set, d *Doc) []core.Finding {
	if !d.Always {
		return nil
	}
	var out []core.Finding
	lines := strings.Split(d.Body, "\n")
	heading := ""
	start, steps, text := 0, 0, ""
	flush := func() {
		if steps >= MinProcedureStep {
			name := heading
			if name == "" {
				name = "procedure"
			}
			out = append(out, core.Finding{
				Rule: "move/procedure", Severity: core.Low, Path: rel(s, d.Path), Line: start,
				Message: fmt.Sprintf("a %d-step procedure (%q) loads every session", steps, name),
				Hint:    "make it a skill: only its description stays loaded",
				Tokens:  tokens.Estimate(text),
			})
		}
		steps, text = 0, ""
	}
	for n, l := range lines {
		if strings.HasPrefix(l, "#") {
			flush()
			heading = strings.TrimSpace(strings.TrimLeft(l, "#"))
			continue
		}
		if stepRe.MatchString(l) {
			if steps == 0 {
				start = n + 1
			}
			steps++
			text += l + "\n"
			continue
		}
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "   ") || strings.HasPrefix(l, "\t") {
			if steps > 0 {
				text += l + "\n"
			}
			continue
		}
		flush()
	}
	flush()
	return out
}

var vaguePhrases = []string{
	"write clean code", "follow best practices", "write good code", "be careful",
	"keep it simple", "use common sense", "write high quality code", "make sure it works",
	"write maintainable code", "follow good practices",
}

func checkVague(s *Set, d *Doc) []core.Finding {
	var out []core.Finding
	for n, l := range strings.Split(strings.ToLower(d.Body), "\n") {
		for _, p := range vaguePhrases {
			if strings.Contains(l, p) {
				out = append(out, core.Finding{
					Rule: "vague", Severity: core.Info, Path: rel(s, d.Path), Line: n + 1,
					Message: fmt.Sprintf("%q gives the agent nothing to check", p),
					Hint:    "say what to run or what never to do, e.g. \"run make check before committing\"",
				})
				break
			}
		}
	}
	return out
}

// checkDuplicates flags substantial lines that load more than once
// across the files in context.
func checkDuplicates(s *Set, all []*Doc) []core.Finding {
	type at struct {
		doc  *Doc
		line int
	}
	first := map[string]at{}
	var out []core.Finding
	for _, d := range all {
		if !d.Always {
			continue
		}
		inFence := false
		for n, l := range strings.Split(d.Body, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "```") {
				inFence = !inFence
				continue
			}
			key := normalize(l)
			if inFence || len(key) < 40 {
				continue
			}
			if f, ok := first[key]; ok {
				if f.doc == d && f.line == n+1 {
					continue
				}
				out = append(out, core.Finding{
					Rule: "dup/line", Severity: core.Low, Path: rel(s, d.Path), Line: n + 1,
					Message: fmt.Sprintf("also in %s:%d; it loads twice", rel(s, f.doc.Path), f.line),
					Tokens:  tokens.Estimate(l),
					Fix:     dupFix(s, f.doc, d, f.line, n+1),
				})
				continue
			}
			first[key] = at{d, n + 1}
		}
	}
	return out
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

func normalize(l string) string {
	l = strings.ToLower(strings.TrimSpace(l))
	l = strings.TrimLeft(l, "-*>#0123456789.) ")
	return strings.Trim(nonWord.ReplaceAllString(l, " "), " ")
}

var ruleRe = regexp.MustCompile(`(?i)\b(always|never|do not|don't)\s+(use|run|add|commit|write|call|edit|push|create|import)\s+([a-z0-9_.\-/` + "`" + `]+)`)

// checkContradictions flags "always X" and "never X" about the same X in
// files that load together: the agent may follow either.
func checkContradictions(s *Set, all []*Doc) []core.Finding {
	type claim struct {
		doc  *Doc
		line int
		yes  bool
	}
	claims := map[string][]claim{}
	for _, d := range all {
		if !d.Always {
			continue
		}
		for n, l := range strings.Split(d.Body, "\n") {
			for _, m := range ruleRe.FindAllStringSubmatch(l, -1) {
				key := strings.ToLower(m[2] + " " + strings.Trim(m[3], "`"))
				yes := strings.EqualFold(m[1], "always")
				claims[key] = append(claims[key], claim{d, n + 1, yes})
			}
		}
	}
	keys := make([]string, 0, len(claims))
	for k := range claims {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []core.Finding
	for _, k := range keys {
		var pos, neg *claim
		for i := range claims[k] {
			c := &claims[k][i]
			if c.yes && pos == nil {
				pos = c
			}
			if !c.yes && neg == nil {
				neg = c
			}
		}
		if pos != nil && neg != nil {
			out = append(out, core.Finding{
				Rule: "contradiction", Severity: core.Medium, Path: rel(s, neg.doc.Path), Line: neg.line,
				Message: fmt.Sprintf("says never %q but %s:%d says always", k, rel(s, pos.doc.Path), pos.line),
			})
		}
	}
	return out
}
