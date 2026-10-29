// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/codegraph"
	"github.com/RhythmShahriar/tacet/internal/config"
)

// maxRouteNote caps the note a search gets, so it costs a few tokens.
const maxRouteNote = 200

// routeBudget bounds the index lookup; past it the search goes on alone.
const routeBudget = 25 * time.Millisecond

// declRe reads a search for one name: a bare identifier, a declaration
// (func Name, def name, class Name, ...) or a call (Name( ), with the
// regex escapes an agent types around it.
var declRe = regexp.MustCompile(`^(?:(?:func|def|class|type|struct|interface|trait|enum|fn|function|module|impl|const|let|var)\s+)?(?:\([^()]*\)\s*)?([A-Za-z_$][A-Za-z0-9_$]*)\s*\(?$`)

// regexNoise is stripped from a Grep pattern before it is read.
var regexNoise = strings.NewReplacer(`\b`, "", `\s+`, " ", `\s*`, "", `\s`, " ", `\(`, "(", `\)`, ")", `\w*`, "", `\w+`, "")

// commonWords are names too generic to say anything about.
var commonWords = map[string]bool{
	"TODO": true, "FIXME": true, "XXX": true, "error": true, "Error": true, "err": true,
	"main": true, "init": true, "test": true, "Test": true, "self": true, "this": true,
	"func": true, "def": true, "class": true, "return": true, "import": true, "package": true,
	"string": true, "String": true, "true": true, "false": true, "null": true, "nil": true,
	"None": true, "new": true, "New": true, "get": true, "set": true, "run": true, "Run": true,
}

// searchName returns the symbol a Grep pattern looks for, or "" when it
// is a text search (alternation, wildcards, phrases).
func searchName(pattern string) string {
	p := strings.TrimSpace(pattern)
	p = strings.TrimPrefix(p, "^")
	p = strings.TrimSuffix(p, "$")
	p = strings.TrimSpace(regexNoise.Replace(p))
	if strings.HasSuffix(p, "()") {
		p = strings.TrimSuffix(p, ")")
	}
	m := declRe.FindStringSubmatch(p)
	if m == nil || len(m[1]) < 3 || commonWords[m[1]] {
		return ""
	}
	return m[1]
}

// globName returns the symbol a Glob pattern seems to look for:
// "**/*Tally*" gives Tally. File names with an extension are files, not
// symbols, so they give "".
func globName(pattern string) string {
	base := path.Base(filepath.ToSlash(strings.TrimSpace(pattern)))
	if strings.Contains(base, ".") {
		return ""
	}
	n := strings.Trim(base, "*?")
	if strings.ContainsAny(n, "*?[]{}") {
		return ""
	}
	return searchName(n)
}

type searchInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

// preSearch adds a short note to a Grep or Glob for a symbol the code
// index knows: where it is defined and how many callers it has, so the
// agent may need no search at all. It never blocks and stays quiet when
// the index is missing or the defining file changed since it was read.
func preSearch(root string, cfg *config.Config, in *hookInput) any {
	if !cfg.On("map") {
		return nil
	}
	var si searchInput
	if json.Unmarshal(in.ToolInput, &si) != nil {
		return nil
	}
	if si.Path != "" && filepath.IsAbs(si.Path) {
		if rel, err := filepath.Rel(root, si.Path); err != nil || strings.HasPrefix(rel, "..") {
			return nil // a search outside the project
		}
	}
	name := ""
	switch in.ToolName {
	case "Glob":
		name = globName(si.Pattern)
	default:
		name = searchName(si.Pattern)
	}
	if name == "" {
		return nil
	}
	note := routeNote(root, name)
	if note == "" {
		return nil
	}
	return map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "additionalContext": note,
	}}
}

// routeNote looks name up in the index read-only and words the note.
func routeNote(root, name string) string {
	ix, err := codegraph.OpenReadOnly(root)
	if err != nil {
		return ""
	}
	defer func() { _ = ix.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), routeBudget)
	defer cancel()
	defs, total, err := ix.Define(ctx, name, 3)
	if err != nil || len(defs) == 0 {
		return ""
	}
	var at []string
	for _, d := range defs {
		if !d.Fresh(ix.Root()) {
			return "" // the file changed: its line may be wrong
		}
		at = append(at, fmt.Sprintf("%s:%d", d.Path, d.Line))
	}
	where := strings.Join(at, ", ")
	if total > len(defs) {
		where += fmt.Sprintf(" and %d more", total-len(defs))
	}
	callers, tail := "", ""
	if n, err := ix.CallerCount(ctx, name); err == nil {
		switch n {
		case 0:
			callers = " (no callers)"
		case 1:
			callers, tail = " (1 caller)", " The tacet callers tool lists it without a grep."
		default:
			callers, tail = fmt.Sprintf(" (%d callers)", n), " The tacet callers tool lists them without a grep."
		}
	}
	note := fmt.Sprintf("tacet: `%s` is defined at %s%s.%s", name, where, callers, tail)
	if len(note) > maxRouteNote {
		note = fmt.Sprintf("tacet: `%s` is defined at %s%s.", name, at[0], callers)
	}
	if len(note) > maxRouteNote {
		return ""
	}
	return note
}
