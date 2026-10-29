// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package codegraph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Symbol is one definition.
type Symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	End       int    `json:"end"`
	Signature string `json:"signature"`
	Depth     int    `json:"depth,omitempty"` // nesting inside other symbols, in an outline
}

// Ref is one line that uses a name.
type Ref struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	In   string `json:"in,omitempty"` // the innermost symbol around the line
	Text string `json:"text"`
}

// ErrNotIndexed means a file isn't in the index: missing, ignored, too
// big or in a language tacet doesn't read.
var ErrNotIndexed = errors.New("file not in the code index")

// splitQuery reads "Type.Method" as the name Method with Type in its
// signature or file.
func splitQuery(q string) (name, qual string) {
	q = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(q), "()"))
	for _, sep := range []string{"::", "->", "."} {
		if i := strings.LastIndex(q, sep); i > 0 && i+len(sep) < len(q) {
			return q[i+len(sep):], q[:i]
		}
	}
	return q, ""
}

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// Symbols finds definitions by name: exact matches first, then names that
// start with q, then names that contain it, ignoring case. It returns at
// most limit symbols and how many matched in all.
func (ix *Index) Symbols(ctx context.Context, q string, limit int) ([]Symbol, int, error) {
	name, qual := splitQuery(q)
	if name == "" {
		return nil, 0, nil
	}
	rows, err := ix.db.QueryContext(ctx, `SELECT f.path, d.name, d.kind, d.line, d.end_line, d.sig
FROM defs d JOIN files f ON f.id = d.file WHERE d.name LIKE ? ESCAPE '\'`, "%"+likeEscape(name)+"%")
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []Symbol
	for rows.Next() {
		var s Symbol
		if err := rows.Scan(&s.Path, &s.Name, &s.Kind, &s.Line, &s.End, &s.Signature); err != nil {
			return nil, 0, err
		}
		if qual != "" && !strings.Contains(strings.ToLower(s.Signature+" "+s.Path), strings.ToLower(qual)) {
			continue
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if qual != "" {
		// Type.method means that method: keep exact names when there are any.
		var exact []Symbol
		for _, s := range out {
			if strings.EqualFold(s.Name, name) {
				exact = append(exact, s)
			}
		}
		if len(exact) > 0 {
			out = exact
		}
	}
	low := strings.ToLower(name)
	score := func(s Symbol) int {
		switch n := strings.ToLower(s.Name); {
		case s.Name == name:
			return 0
		case n == low:
			return 1
		case strings.HasPrefix(n, low):
			return 2
		}
		return 3
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := score(out[i]), score(out[j])
		if a != b {
			return a < b
		}
		if ta, tb := isTest(out[i].Path), isTest(out[j].Path); ta != tb {
			return !ta
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Line < out[j].Line
	})
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

// isTest reports whether path looks like a test file.
func isTest(p string) bool {
	b := strings.ToLower(filepath.Base(p))
	return strings.Contains(b, "_test.") || strings.Contains(b, ".test.") || strings.Contains(b, ".spec.") ||
		strings.HasPrefix(b, "test_") || strings.Contains(p, "/tests/") || strings.Contains(p, "/__tests__/") ||
		strings.HasPrefix(p, "tests/") || strings.HasPrefix(p, "test/")
}

// Callers finds the lines that use name, outside its definitions, with
// the symbol each line sits in. Calls, constructions and type uses come
// back when there are any; plain mentions, which may be a local that
// shares the name, only when there are none. Matching is by name, so two
// symbols that share a name share their callers. It returns at most
// limit refs and how many there are in all.
func (ix *Index) Callers(ctx context.Context, q string, limit int) ([]Ref, int, error) {
	name, _ := splitQuery(q)
	if name == "" {
		return nil, 0, nil
	}
	rows, err := ix.db.QueryContext(ctx, `SELECT f.id, f.path, r.lines, r.strong FROM refs r JOIN files f ON f.id = r.file
WHERE r.name = ? ORDER BY f.path`, name)
	if err != nil {
		return nil, 0, err
	}
	type hit struct {
		file   int64
		strong bool
		Ref
	}
	var hits []hit
	anyStrong := false
	for rows.Next() {
		var h hit
		var lines string
		var strong int
		if err := rows.Scan(&h.file, &h.Path, &lines, &strong); err != nil {
			_ = rows.Close()
			return nil, 0, err
		}
		anyStrong = anyStrong || strong > 0
		for _, l := range unpackLines(lines) {
			h.Line, h.strong = l.line, l.strong
			hits = append(hits, h)
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if anyStrong {
		kept := hits[:0]
		for _, h := range hits {
			if h.strong {
				kept = append(kept, h)
			}
		}
		hits = kept
	}
	// Tests last: the agent usually wants the real call sites first.
	sort.SliceStable(hits, func(i, j int) bool { return !isTest(hits[i].Path) && isTest(hits[j].Path) })
	total := len(hits)
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Ref, 0, len(hits))
	outlines := map[int64][]Symbol{}
	lines := map[string][]string{}
	for _, h := range hits {
		syms, ok := outlines[h.file]
		if !ok {
			syms, _ = ix.fileSymbols(ctx, h.file)
			outlines[h.file] = syms
		}
		h.In = enclosing(syms, h.Line)
		ls, ok := lines[h.Path]
		if !ok {
			b, err := os.ReadFile(filepath.Join(ix.root, filepath.FromSlash(h.Path))) // #nosec G304 -- an indexed project file
			if err == nil {
				ls = strings.Split(string(b), "\n")
			}
			lines[h.Path] = ls
		}
		if h.Line-1 < len(ls) {
			h.Text = signature([][]byte{[]byte(ls[h.Line-1])}, 1)
		}
		out = append(out, h.Ref)
	}
	return out, total, nil
}

// enclosing is the innermost symbol whose lines hold line.
func enclosing(syms []Symbol, line int) string {
	best := -1
	for i, s := range syms {
		if s.Line <= line && line <= s.End && s.Line != line {
			if best < 0 || s.End-s.Line <= syms[best].End-syms[best].Line {
				best = i
			}
		}
	}
	if best < 0 {
		return ""
	}
	return syms[best].Name
}

func (ix *Index) fileSymbols(ctx context.Context, file int64) ([]Symbol, error) {
	rows, err := ix.db.QueryContext(ctx, `SELECT f.path, d.name, d.kind, d.line, d.end_line, d.sig
FROM defs d JOIN files f ON f.id = d.file WHERE d.file = ? ORDER BY d.line, d.end_line DESC`, file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Symbol
	for rows.Next() {
		var s Symbol
		if err := rows.Scan(&s.Path, &s.Name, &s.Kind, &s.Line, &s.End, &s.Signature); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Rel turns a path given by the agent (absolute, ./relative or with
// backslashes) into the slash-separated path the index uses.
func (ix *Index) Rel(path string) string {
	p := filepath.FromSlash(strings.TrimSpace(path))
	if filepath.IsAbs(p) {
		if r, err := filepath.Rel(ix.root, p); err == nil {
			p = r
		}
	}
	return filepath.ToSlash(filepath.Clean(p))
}

// Outline lists the symbols in one file in line order, each with its
// nesting depth, so a caller can read only the lines it needs.
func (ix *Index) Outline(ctx context.Context, path string) ([]Symbol, error) {
	var id int64
	if err := ix.db.QueryRowContext(ctx, `SELECT id FROM files WHERE path = ?`, ix.Rel(path)).Scan(&id); err != nil {
		return nil, ErrNotIndexed
	}
	syms, err := ix.fileSymbols(ctx, id)
	if err != nil {
		return nil, err
	}
	var stack []int // end lines of the open symbols
	for i := range syms {
		for len(stack) > 0 && syms[i].Line > stack[len(stack)-1] {
			stack = stack[:len(stack)-1]
		}
		syms[i].Depth = len(stack)
		if syms[i].End > syms[i].Line {
			stack = append(stack, syms[i].End)
		}
	}
	return syms, nil
}
