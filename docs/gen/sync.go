// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package gen

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/RhythmShahriar/tacet/docs/lint"
)

// Target is another agent's instruction file, generated from AGENTS.md.
type Target struct {
	Name  string
	Path  string // slash path from the project root
	front string // frontmatter the format needs before the header
	dir   string // a folder whose presence means the tool is in use
}

// Targets are the formats sync writes.
var Targets = []Target{
	{Name: "cursor", Path: ".cursor/rules/agents.mdc", front: "---\ndescription: Project rules, generated from AGENTS.md\nalwaysApply: true\n---\n", dir: ".cursor/rules"},
	{Name: "copilot", Path: ".github/copilot-instructions.md"},
	{Name: "gemini", Path: "GEMINI.md"},
}

// State is where a generated file stands against AGENTS.md.
type State string

// States.
const (
	New     State = "new"     // not there yet
	Stale   State = "stale"   // generated from an older AGENTS.md
	Current State = "current" // matches AGENTS.md
	Drift   State = "drift"   // generated, then edited by hand
	Manual  State = "manual"  // written by hand, never generated
)

// Item is one target's plan.
type Item struct {
	Target Target
	Path   string // absolute
	State  State
	Old    []byte // nil when missing
	New    []byte
}

// SyncResult is a sync plan; nothing is written by Sync.
type SyncResult struct {
	Items       []Item
	AgentsBytes int
	OverCodex   bool // Codex stops reading AGENTS.md at 32 KiB
}

// ErrNoAgents means there is no AGENTS.md to sync from.
var ErrNoAgents = errors.New("no AGENTS.md here; tacet docs init writes one")

var headerRe = regexp.MustCompile(`(?m)^<!-- tacet sync: sha256:([0-9a-f]{64}) [^\n]*-->\n`)

// Sync plans the generated files. With to empty it covers only targets
// that already exist; to names targets to create as well.
func Sync(root string, to []string) (*SyncResult, error) {
	src, err := os.ReadFile(filepath.Join(root, "AGENTS.md")) // #nosec G304 -- the project's AGENTS.md
	if err != nil {
		return nil, ErrNoAgents
	}
	want := map[string]bool{}
	for _, n := range to {
		found := false
		for _, t := range Targets {
			found = found || t.Name == n
		}
		if !found {
			return nil, fmt.Errorf("no sync target %q; targets: cursor, copilot, gemini", n)
		}
		want[n] = true
	}
	res := &SyncResult{AgentsBytes: len(src), OverCodex: len(src) > lint.CodexLimit}
	body := clean(src)
	for _, t := range Targets {
		p := filepath.Join(root, filepath.FromSlash(t.Path))
		old, rerr := os.ReadFile(p) // #nosec G304 -- a file sync generates
		if rerr != nil {
			old = nil
		}
		used := old != nil || want[t.Name]
		if !used && t.dir != "" {
			if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(t.dir))); err == nil && fi.IsDir() {
				used = true
			}
		}
		if !used {
			continue
		}
		next := Render(t, body)
		res.Items = append(res.Items, Item{Target: t, Path: p, State: state(old, next), Old: old, New: next})
	}
	return res, nil
}

// Render is a target's file for a cleaned AGENTS.md body.
func Render(t Target, body []byte) []byte {
	sum := sha256.Sum256(body)
	var b bytes.Buffer
	b.WriteString(t.front)
	fmt.Fprintf(&b, "<!-- tacet sync: sha256:%s do not edit; edit AGENTS.md -->\n", hex.EncodeToString(sum[:]))
	b.Write(body)
	return b.Bytes()
}

// state compares what is there with what sync would write. A generated
// file whose body no longer matches its own hash was edited by hand.
func state(old, next []byte) State {
	switch {
	case old == nil:
		return New
	case bytes.Equal(old, next):
		return Current
	}
	loc := headerRe.FindSubmatchIndex(old)
	if loc == nil {
		return Manual
	}
	sum := sha256.Sum256(old[loc[1]:])
	if hex.EncodeToString(sum[:]) != string(old[loc[2]:loc[3]]) {
		return Drift
	}
	return Stale
}

var blankRun = regexp.MustCompile(`\n{3,}`)

// clean drops HTML comments, which other agents may not strip, and the
// blank lines they leave.
func clean(src []byte) []byte {
	s := lint.StripComments(string(src))
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		lines = append(lines, strings.TrimRight(l, " \t"))
	}
	s = blankRun.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	return []byte(strings.TrimSpace(s) + "\n")
}
