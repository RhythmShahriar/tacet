// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package lint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/tokens"
)

// MinScopedLines is how many non-blank lines a section needs before
// moving it to a path rule is worth a file.
const MinScopedLines = 3

// fixable rules get their fix's startup delta measured after Check.
var fixable = map[string]bool{"derivable/tree": true, "dup/line": true, "move/notes": true, "move/scoped": true}

// measureFixes sets each fix's Tokens to what it takes off startup when
// applied alone, by loading the set again with the edit planned.
func measureFixes(s *Set, fs []core.Finding) {
	for i := range fs {
		f := fs[i].Fix
		if f == nil || !fixable[fs[i].Rule] {
			continue
		}
		if n, err := s.Saving(f); err == nil {
			f.Tokens = n
		}
	}
}

// inRoot says a file is the project's to change; the user file and
// parent folders' files are reported but never edited.
func inRoot(s *Set, p string) bool {
	r, err := filepath.Rel(s.Root, p)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// fixID is stable while the text it fixes stays the same, so dismissing
// a suggestion survives edits elsewhere in the file.
func fixID(kind string, s *Set, path, text string) string {
	h := sha256.Sum256([]byte(rel(s, path) + "\x00" + text))
	return kind + "-" + hex.EncodeToString(h[:4])
}

// block is lines a..b (0-based, inclusive) of a body split on "\n",
// with the newline that ends the last one when there is one.
func block(lines []string, a, b int) string {
	t := strings.Join(lines[a:b+1], "\n")
	if b < len(lines)-1 {
		t += "\n"
	}
	return t
}

// findBlock finds text starting at a line start; last picks the last
// such copy. -1 when absent.
func findBlock(s, text string, last bool) int {
	at := -1
	for off := 0; off <= len(s); {
		i := strings.Index(s[off:], text)
		if i < 0 {
			break
		}
		i += off
		if i == 0 || s[i-1] == '\n' {
			if !last {
				return i
			}
			at = i
		}
		off = i + 1
	}
	return at
}

// removeBlock is a Change deleting text (whole lines). Text already gone
// leaves the file alone, so overlapping fixes compose.
func removeBlock(text string, last bool) func([]byte) ([]byte, error) {
	return func(cur []byte) ([]byte, error) {
		s := string(cur)
		i := findBlock(s, text, last)
		if i < 0 {
			return cur, nil
		}
		out := s[:i] + s[i+len(text):]
		switch {
		case i == 0:
			out = strings.TrimLeft(out, "\n")
		case i >= len(out):
			out = strings.TrimRight(out, "\n") + "\n"
		case i >= 2 && out[i-2:i] == "\n\n" && out[i] == '\n':
			out = out[:i] + out[i+1:]
		}
		return []byte(out), nil
	}
}

// commentBlock is a Change wrapping text in an HTML block comment,
// which Claude Code strips before the model sees the file.
func commentBlock(text string) func([]byte) ([]byte, error) {
	return func(cur []byte) ([]byte, error) {
		s := string(cur)
		i := findBlock(s, text, false)
		if i < 0 {
			return cur, nil
		}
		nl := strings.HasSuffix(text, "\n")
		wrapped := "<!-- " + strings.TrimSuffix(text, "\n") + " -->"
		if nl {
			wrapped += "\n"
		}
		return []byte(s[:i] + wrapped + s[i+len(text):]), nil
	}
}

var (
	todoRe  = regexp.MustCompile(`^\s*(?:[-*+]\s+)?(?:\*\*|_)?(TODO|FIXME|XXX)\b`)
	noteRe  = regexp.MustCompile(`^\s*(?:[-*+]\s+)?(?:\*\*|_)?(?i:note|n\.b\.)(?:\s+(?:to|for)\s+[a-z ]+)?(?:\*\*|_)?\s*:`)
	humanRe = regexp.MustCompile(`(?i)\b(maintainers?|humans?|contributors?|reviewers?|note to self|future me)\b`)
	listRe  = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s`)
)

// checkNotes flags notes written for people, which cost tokens in every
// session. TODO and FIXME lines and notes addressed to maintainers are
// safe to hide in a comment; a plain "Note:" may be for the agent, so it
// waits as a suggestion.
func checkNotes(s *Set, d *Doc) []core.Finding {
	if !d.Always {
		return nil
	}
	var out []core.Finding
	lines := strings.Split(d.Body, "\n")
	inFence, inComment := false, false
	for n := 0; n < len(lines); n++ {
		l := lines[n]
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") {
			inFence = !inFence
			continue
		}
		if inComment {
			inComment = !strings.Contains(l, "-->")
			continue
		}
		if strings.Contains(l, "<!--") {
			inComment = !strings.Contains(l[strings.Index(l, "<!--"):], "-->")
			continue
		}
		if inFence {
			continue
		}
		todo := todoRe.MatchString(l)
		if !todo && !noteRe.MatchString(l) {
			continue
		}
		// The note runs on through lines that continue it.
		end := n
		item := listRe.MatchString(l)
		for end+1 < len(lines) {
			x := lines[end+1]
			xt := strings.TrimSpace(x)
			if xt == "" || strings.HasPrefix(xt, "#") || strings.HasPrefix(xt, "```") || strings.Contains(x, "<!--") || strings.Contains(x, "-->") {
				break
			}
			if item && !strings.HasPrefix(x, " ") && !strings.HasPrefix(x, "\t") {
				break
			}
			if !item && listRe.MatchString(x) {
				break
			}
			end++
		}
		text := block(lines, n, end)
		if strings.Contains(text, "-->") {
			n = end
			continue
		}
		safe := todo || humanRe.MatchString(text)
		f := core.Finding{
			Rule: "move/notes", Severity: core.Low, Path: rel(s, d.Path), Line: n + 1,
			Message: "a note for maintainers loads every session",
			Hint:    "wrap it in <!-- --> so people still see it and the agent doesn't pay for it",
			Tokens:  tokens.Estimate(text),
		}
		if !safe {
			f.Message = "a \"Note:\" line loads every session; if it is for people, hide it"
		}
		if inRoot(s, d.Path) {
			f.Fix = &core.Fix{
				ID: fixID("notes", s, d.Path, text), Title: fmt.Sprintf("Hide the note at %s:%d in an HTML comment", rel(s, d.Path), n+1),
				Safe: safe, Edits: []core.Edit{{Path: d.Path, Change: commentBlock(text)}},
			}
		}
		out = append(out, f)
		n = end
	}
	return out
}

type section struct {
	start, end int // 0-based lines, heading to last line, inclusive
	level      int
	title      string
}

// sections lists every heading outside code blocks with the lines it
// owns: up to the next heading at its level or above.
func sections(lines []string) []section {
	var out []section
	inFence := false
	for n, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(l, "#") {
			continue
		}
		lv := len(l) - len(strings.TrimLeft(l, "#"))
		if lv > 6 || (len(l) > lv && l[lv] != ' ') {
			continue
		}
		for i := range out {
			if out[i].end < 0 && out[i].level >= lv {
				out[i].end = n - 1
			}
		}
		out = append(out, section{start: n, end: -1, level: lv, title: strings.TrimSpace(l[lv:])})
	}
	for i := range out {
		if out[i].end < 0 {
			out[i].end = len(lines) - 1
		}
		for out[i].end > out[i].start && strings.TrimSpace(lines[out[i].end]) == "" {
			out[i].end--
		}
	}
	return out
}

var dirRefRe = regexp.MustCompile("(?:^|[\\s`'\"(\\[])(?:\\./)?([A-Za-z0-9_][A-Za-z0-9_.\\-]*)/(?:([A-Za-z0-9_][A-Za-z0-9_.\\-]*)/?)?")

// containers hold one package per folder, so a scope goes a level down.
var containers = map[string]bool{"apps": true, "packages": true, "libs": true, "services": true, "crates": true, "modules": true, "plugins": true}

// notScopes are folders people point the agent at to read, not to work
// in: a section about them still matters everywhere.
var notScopes = map[string]bool{"docs": true, "doc": true, "documentation": true}

func isDir(p string) bool { fi, err := os.Stat(p); return err == nil && fi.IsDir() }

// scopedDir is the one folder a section talks about, or "". The heading
// has to name it, so a section that only points at a file there stays.
func scopedDir(root, heading, text string) string {
	dir := ""
	for _, m := range dirRefRe.FindAllStringSubmatch(text, -1) {
		name := m[1]
		if !isDir(filepath.Join(root, name)) {
			continue
		}
		if containers[name] && m[2] != "" && isDir(filepath.Join(root, name, m[2])) {
			name += "/" + m[2]
		}
		if dir != "" && dir != name {
			return ""
		}
		dir = name
	}
	if dir == "" || notScopes[dir] || !namesDir(heading, dir) {
		return ""
	}
	return dir
}

var wordRe = regexp.MustCompile(`[a-z0-9]+`)

// namesDir says a heading names a folder: every word of the folder's
// last segment appears in it.
func namesDir(heading, dir string) bool {
	h := map[string]bool{}
	for _, w := range wordRe.FindAllString(strings.ToLower(heading), -1) {
		h[w] = true
		h[strings.TrimSuffix(w, "s")] = true
	}
	words := wordRe.FindAllString(strings.ToLower(dir[strings.LastIndex(dir, "/")+1:]), -1)
	for _, w := range words {
		if !h[w] && !h[strings.TrimSuffix(w, "s")] {
			return false
		}
	}
	return len(words) > 0
}

var ruleNameRe = regexp.MustCompile(`[^a-z0-9_-]+`)

// checkScoped flags sections about one folder only: as a rule with
// paths: frontmatter Claude Code loads them when it reads files there.
func checkScoped(s *Set, d *Doc) []core.Finding {
	if !d.Always || d.Kind == Rule || d.Kind == User || !inRoot(s, d.Path) {
		return nil
	}
	var out []core.Finding
	lines := strings.Split(d.Body, "\n")
	taken := -1
	for _, sec := range sections(lines) {
		if sec.level < 2 || sec.start <= taken {
			continue
		}
		body := 0
		for _, l := range lines[sec.start+1 : sec.end+1] {
			if strings.TrimSpace(l) != "" {
				body++
			}
		}
		if body < MinScopedLines {
			continue
		}
		text := block(lines, sec.start, sec.end)
		dir := scopedDir(s.Root, sec.title, text)
		if dir == "" {
			continue
		}
		taken = sec.end
		name := strings.Trim(ruleNameRe.ReplaceAllString(strings.ToLower(dir), "-"), "-")
		if name == "" {
			name = "scoped"
		}
		rulePath := filepath.Join(s.Root, ".claude", "rules", name+".md")
		moved := strings.TrimRight(text, "\n") + "\n"
		front := "---\npaths:\n  - \"" + dir + "/**\"\n---\n\n"
		out = append(out, core.Finding{
			Rule: "move/scoped", Severity: core.Low, Path: rel(s, d.Path), Line: sec.start + 1,
			Message: fmt.Sprintf("section %q is only about %s/ but loads every session", sec.title, dir),
			Hint:    fmt.Sprintf("move it to .claude/rules/%s.md with paths: so it loads only when Claude works in %s/", name, dir),
			Tokens:  tokens.Estimate(text),
			Fix: &core.Fix{
				ID: fixID("scope", s, d.Path, text), Title: fmt.Sprintf("Move %q to .claude/rules/%s.md, loaded only in %s/", sec.title, name, dir),
				Edits: []core.Edit{
					{Path: d.Path, Change: removeBlock(text, false)},
					{Path: rulePath, Change: func(cur []byte) ([]byte, error) {
						if strings.Contains(string(cur), moved) {
							return cur, nil
						}
						if len(cur) == 0 {
							return []byte(front + moved), nil
						}
						return []byte(strings.TrimRight(string(cur), "\n") + "\n\n" + moved), nil
					}},
				},
			},
		})
	}
	return out
}

// treeFix deletes a fenced tree, with its heading when the section holds
// nothing else.
func treeFix(s *Set, d *Doc, f fence) *core.Fix {
	if !inRoot(s, d.Path) {
		return nil
	}
	lines := strings.Split(d.Body, "\n")
	a, b := f.line-1, f.end-1
	p := a - 1
	for p >= 0 && strings.TrimSpace(lines[p]) == "" {
		p--
	}
	if p >= 0 {
		for _, sec := range sections(lines) {
			if sec.start == p && sec.end == b {
				a = p
			}
		}
	}
	text := block(lines, a, b)
	return &core.Fix{
		ID: fixID("tree", s, d.Path, text), Title: fmt.Sprintf("Delete the directory tree at %s:%d", rel(s, d.Path), f.line),
		Edits: []core.Edit{{Path: d.Path, Change: removeBlock(text, false)}},
	}
}

// dupFix removes one copy of a duplicated line: the later one, unless
// that copy is in AGENTS.md, the file the others should defer to.
func dupFix(s *Set, first, later *Doc, firstLine, laterLine int) *core.Fix {
	target, line := later, laterLine
	if filepath.Base(later.Path) == "AGENTS.md" && filepath.Base(first.Path) != "AGENTS.md" {
		target, line = first, firstLine
	}
	if !inRoot(s, target.Path) {
		return nil
	}
	lines := strings.Split(target.Body, "\n")
	text := block(lines, line-1, line-1)
	last := target == later
	return &core.Fix{
		ID: fixID("dup", s, target.Path, text), Title: fmt.Sprintf("Remove the duplicate line at %s:%d", rel(s, target.Path), line),
		Edits: []core.Edit{{Path: target.Path, Change: removeBlock(text, last)}},
	}
}
