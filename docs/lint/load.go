// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package lint loads the instruction files Claude Code puts in context at
// startup and checks them for waste: size, staleness, duplication and
// content that belongs somewhere cheaper.
package lint

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/RhythmShahriar/tacet/tokens"
)

// Kind says why a file is in context.
type Kind string

// Kinds of instruction file.
const (
	User    Kind = "user"    // ~/.claude/CLAUDE.md
	Project Kind = "project" // CLAUDE.md or .claude/CLAUDE.md, here or in a parent
	Local   Kind = "local"   // CLAUDE.local.md
	Agents  Kind = "agents"  // AGENTS.md, read when no CLAUDE.md is
	Rule    Kind = "rule"    // .claude/rules/*.md
	Import  Kind = "import"  // pulled in by @path
)

// MaxImportDepth is how many @import hops Claude Code follows.
const MaxImportDepth = 4

// Doc is one instruction file and the files it imports.
type Doc struct {
	Path    string
	Kind    Kind
	Body    string
	Always  bool // loaded at startup; false for path-scoped rules
	Tokens  int  // this file alone, HTML block comments removed
	Depth   int
	Imports []*Doc
	Missing []Ref // @imports whose target doesn't exist
	TooDeep []Ref // @imports past MaxImportDepth
}

// Ref is a reference written in a file.
type Ref struct {
	Target string
	Line   int
}

// Set is everything loaded for one project.
type Set struct {
	Root string
	Docs []*Doc // top-level files in load order
}

// All walks every doc, imports included, once each.
func (s *Set) All() []*Doc {
	var out []*Doc
	seen := map[string]bool{}
	var walk func(d *Doc)
	walk = func(d *Doc) {
		if seen[d.Path] {
			return
		}
		seen[d.Path] = true
		out = append(out, d)
		for _, c := range d.Imports {
			walk(c)
		}
	}
	for _, d := range s.Docs {
		walk(d)
	}
	return out
}

// StartupTokens is the estimated cost of every always-loaded file.
func (s *Set) StartupTokens() int {
	n := 0
	for _, d := range s.All() {
		if d.Always {
			n += d.Tokens
		}
	}
	return n
}

// Load finds the instruction files for root the way Claude Code does:
// the user file, CLAUDE.md files from the filesystem root down to root,
// CLAUDE.local.md, AGENTS.md only where no CLAUDE file shadows it, and
// the rules in .claude/rules. userHome is Claude's config dir; empty
// skips the user file (tests and CI).
func Load(root, userHome string) (*Set, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	s := &Set{Root: root}
	add := func(path string, k Kind, always bool) *Doc {
		d := readDoc(path, k, always, 0, map[string]bool{})
		if d != nil {
			s.Docs = append(s.Docs, d)
		}
		return d
	}
	if userHome != "" {
		add(filepath.Join(userHome, "CLAUDE.md"), User, true)
	}

	// Parents first, root last: Claude Code concatenates them in that order.
	var dirs []string
	for d := root; ; d = filepath.Dir(d) {
		dirs = append([]string{d}, dirs...)
		if filepath.Dir(d) == d {
			break
		}
	}
	claudeSeen := false
	for _, d := range dirs {
		found := false
		for _, name := range []string{"CLAUDE.md", filepath.Join(".claude", "CLAUDE.md")} {
			if add(filepath.Join(d, name), Project, true) != nil {
				found = true
			}
		}
		if add(filepath.Join(d, "CLAUDE.local.md"), Local, true) != nil {
			found = true
		}
		claudeSeen = claudeSeen || found
	}
	if !claudeSeen {
		for _, d := range dirs {
			add(filepath.Join(d, "AGENTS.md"), Agents, true)
		}
	}

	rules, _ := filepath.Glob(filepath.Join(root, ".claude", "rules", "*.md"))
	sub, _ := filepath.Glob(filepath.Join(root, ".claude", "rules", "*", "*.md"))
	for _, r := range append(rules, sub...) {
		b, err := os.ReadFile(r) // #nosec G304 -- a rule file in the project
		if err != nil {
			continue
		}
		add(r, Rule, !hasPathsFrontmatter(b))
	}
	return s, nil
}

func readDoc(path string, k Kind, always bool, depth int, seen map[string]bool) *Doc {
	b, err := os.ReadFile(path) // #nosec G304 -- instruction files Claude Code would load
	if err != nil {
		return nil
	}
	body := string(b)
	d := &Doc{Path: path, Kind: k, Body: body, Always: always, Depth: depth}
	d.Tokens = tokens.Estimate(StripComments(body))
	seen[path] = true
	for _, ref := range Imports(body) {
		target := resolve(path, ref.Target)
		if seen[target] {
			continue
		}
		if depth+1 > MaxImportDepth {
			d.TooDeep = append(d.TooDeep, ref)
			continue
		}
		c := readDoc(target, Import, always, depth+1, seen)
		if c == nil {
			d.Missing = append(d.Missing, ref)
			continue
		}
		d.Imports = append(d.Imports, c)
	}
	return d
}

func resolve(from, target string) string {
	if strings.HasPrefix(target, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, target[2:])
		}
	}
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(filepath.Dir(from), filepath.FromSlash(target))
}

var importRe = regexp.MustCompile(`(?:^|\s)@((?:~/|\.{0,2}/)?[A-Za-z0-9_.\-/]+[A-Za-z0-9_\-/])`)

// Imports lists the @path references in body, skipping code blocks and
// inline code the way Claude Code does. An @ in an email address or a
// bare @name with no dot or slash isn't an import.
func Imports(body string) []Ref {
	var out []Ref
	inFence := false
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(nil, 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		line = stripInlineCode(line)
		for _, m := range importRe.FindAllStringSubmatch(line, -1) {
			t := m[1]
			if !looksLikeFile(t) {
				continue
			}
			out = append(out, Ref{Target: strings.TrimRight(t, "."), Line: n})
		}
	}
	return out
}

// looksLikeFile tells an @import from an @name, an @scope/package or a
// decorator: it starts like a path or its last part has an extension.
func looksLikeFile(t string) bool {
	if strings.HasPrefix(t, "./") || strings.HasPrefix(t, "../") || strings.HasPrefix(t, "~/") || strings.HasPrefix(t, "/") {
		return true
	}
	last := t[strings.LastIndex(t, "/")+1:]
	return strings.Contains(strings.TrimLeft(last, "."), ".") || strings.HasPrefix(last, ".")
}

func stripInlineCode(line string) string {
	var b strings.Builder
	in := false
	for _, r := range line {
		if r == '`' {
			in = !in
			continue
		}
		if !in {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var commentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

// StripComments removes HTML comments, which Claude Code drops before a
// file reaches the model.
func StripComments(s string) string { return commentRe.ReplaceAllString(s, "") }

func hasPathsFrontmatter(b []byte) bool {
	if !bytes.HasPrefix(b, []byte("---")) {
		return false
	}
	end := bytes.Index(b[3:], []byte("\n---"))
	if end < 0 {
		return false
	}
	for _, l := range strings.Split(string(b[3:3+end]), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "paths:") {
			return true
		}
	}
	return false
}
