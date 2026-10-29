// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package lint

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/RhythmShahriar/tacet/core"
)

// FileChange is one file before and after a set of fixes; nil is a
// file that doesn't exist.
type FileChange struct {
	Path string
	Old  []byte
	New  []byte
}

// errChanged means a file no longer holds what a fix expected.
var errChanged = errors.New("file changed since tacet read it")

// Plan runs the fixes' edits in order on the files as they are now, the
// way fix.Apply will, and returns every touched file in first-touch
// order. Nothing is written.
func Plan(fixes []*core.Fix) ([]FileChange, error) {
	var order []string
	old := map[string][]byte{}
	cur := map[string][]byte{}
	for _, f := range fixes {
		for _, e := range f.Edits {
			base, seen := cur[e.Path]
			if !seen {
				b, err := os.ReadFile(e.Path) // #nosec G304 -- a file a fix planned to edit
				if err == nil {
					base = b
				}
				old[e.Path] = base
				order = append(order, e.Path)
			}
			if e.Change != nil {
				n, err := e.Change(base)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", e.Path, err)
				}
				cur[e.Path] = n
				continue
			}
			if !bytes.Equal(base, e.Old) {
				return nil, fmt.Errorf("%s: %w", e.Path, errChanged)
			}
			cur[e.Path] = e.New
		}
	}
	out := make([]FileChange, 0, len(order))
	for _, p := range order {
		out = append(out, FileChange{Path: p, Old: old[p], New: cur[p]})
	}
	return out, nil
}

// After loads the set again as it would be once the fixes are applied,
// imports and new rule files included, so StartupTokens before and after
// gives a fix's real delta.
func (s *Set) After(fixes []*core.Fix) (*Set, []FileChange, error) {
	plan, err := Plan(fixes)
	if err != nil {
		return nil, nil, err
	}
	overlay := map[string][]byte{}
	for _, c := range plan {
		overlay[c.Path] = c.New
	}
	next, err := load(s.Root, s.userHome, overlay)
	return next, plan, err
}

// Saving is how many startup tokens the fixes take off; negative when
// they add some.
func (s *Set) Saving(fixes ...*core.Fix) (int, error) {
	next, _, err := s.After(fixes)
	if err != nil {
		return 0, err
	}
	return s.StartupTokens() - next.StartupTokens(), nil
}

// Diff is a unified diff of one file with two lines of context.
func Diff(name string, old, next []byte) string {
	a, b := splitLines(old), splitLines(next)
	// Trim what both share at the ends; fixes change small regions.
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ops := diffOps(a[pre:len(a)-suf], b[pre:len(b)-suf])
	if len(ops) == 0 {
		return ""
	}
	full := make([]op, 0, pre+len(ops)+suf)
	for _, l := range a[:pre] {
		full = append(full, op{' ', l})
	}
	full = append(full, ops...)
	for _, l := range a[len(a)-suf:] {
		full = append(full, op{' ', l})
	}

	var w strings.Builder
	oldName, newName := "a/"+name, "b/"+name
	if old == nil {
		oldName = "/dev/null"
	}
	if next == nil {
		newName = "/dev/null"
	}
	fmt.Fprintf(&w, "--- %s\n+++ %s\n", oldName, newName)
	const ctx = 2
	for i := 0; i < len(full); {
		if full[i].kind == ' ' {
			i++
			continue
		}
		start := max(i-ctx, 0)
		end := i
		for end < len(full) {
			if full[end].kind != ' ' {
				end++
				continue
			}
			run := end
			for run < len(full) && full[run].kind == ' ' {
				run++
			}
			if run == len(full) || run-end > 2*ctx {
				end = min(end+ctx, len(full))
				break
			}
			end = run
		}
		ao, bo := 1, 1
		for _, o := range full[:start] {
			if o.kind != '+' {
				ao++
			}
			if o.kind != '-' {
				bo++
			}
		}
		an, bn := 0, 0
		for _, o := range full[start:end] {
			if o.kind != '+' {
				an++
			}
			if o.kind != '-' {
				bn++
			}
		}
		fmt.Fprintf(&w, "@@ -%d,%d +%d,%d @@\n", ao, an, bo, bn)
		for _, o := range full[start:end] {
			w.WriteByte(o.kind)
			w.WriteString(o.line)
			w.WriteByte('\n')
		}
		i = end
	}
	return w.String()
}

type op struct {
	kind byte // ' ', '-' or '+'
	line string
}

func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// diffOps is a longest-common-subsequence diff; past a size where the
// table gets big it shows the region as replaced.
func diffOps(a, b []string) []op {
	var out []op
	if len(a)*len(b) > 4_000_000 {
		for _, l := range a {
			out = append(out, op{'-', l})
		}
		for _, l := range b {
			out = append(out, op{'+', l})
		}
		return out
	}
	n, m := len(a), len(b)
	t := make([][]int, n+1)
	for i := range t {
		t[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i][j] = t[i+1][j+1] + 1
			} else {
				t[i][j] = max(t[i+1][j], t[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, op{' ', a[i]})
			i++
			j++
		case t[i+1][j] >= t[i][j+1]:
			out = append(out, op{'-', a[i]})
			i++
		default:
			out = append(out, op{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, op{'-', a[i]})
	}
	for ; j < m; j++ {
		out = append(out, op{'+', b[j]})
	}
	return out
}
