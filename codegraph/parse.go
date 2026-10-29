// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package codegraph

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"

	ts "github.com/odvcencio/gotreesitter"
)

// parseTimeout bounds one file's parse, so a pathological file can't
// stall the index. A file that hits it keeps what was parsed.
const parseTimeout = 3_000_000 // microseconds

// maxSig is the longest signature kept, in runes.
const maxSig = 160

// def is a definition found in one file.
type def struct {
	name, kind, sig string
	line, end       int
}

// ref is one line where a name appears. Strong refs call the name, make
// one with new or use it as a type; the rest may be a local variable
// that only shares the name.
type ref struct {
	name   string
	line   int
	strong bool
}

// parser parses one language. It isn't safe for concurrent use.
type parser struct {
	lang   *language
	tsLang *ts.Language
	p      *ts.Parser
	tagger *ts.Tagger
	idents map[ts.Symbol]bool
	member map[ts.Symbol]bool
	calls  map[ts.Symbol]bool
	types  map[ts.Symbol]bool
}

func newParser(l *language) (*parser, error) {
	tl := l.load()
	tg, err := ts.NewTagger(tl, l.query)
	if err != nil {
		return nil, err
	}
	p := ts.NewParser(tl)
	p.SetTimeoutMicros(parseTimeout)
	ps := &parser{lang: l, tsLang: tl, p: p, tagger: tg,
		idents: symbols(tl, identTypes), member: symbols(tl, memberTypes),
		calls: symbols(tl, callTypes), types: symbols(tl, typeRefTypes)}
	return ps, nil
}

// symbols are the grammar's symbols whose names are in set.
func symbols(tl *ts.Language, set map[string]bool) map[ts.Symbol]bool {
	out := map[ts.Symbol]bool{}
	for i, n := range tl.SymbolNames {
		if set[n] {
			out[ts.Symbol(i)] = true // #nosec G115 -- symbol tables are small
		}
	}
	return out
}

// parse returns the definitions in src and every line where an
// identifier appears, the definitions' own names left out.
func (ps *parser) parse(src []byte) ([]def, []ref) {
	var tree *ts.Tree
	var err error
	if f := ps.lang.tokenSource(); f != nil {
		tree, err = ps.p.ParseWithTokenSource(src, f(src, ps.tsLang))
	} else {
		tree, err = ps.p.Parse(src)
	}
	if err != nil || tree == nil || tree.RootNode() == nil {
		return nil, nil
	}
	defer tree.Release()

	lines := bytes.Split(src, []byte("\n"))
	var defs []def
	at := map[uint32]bool{} // byte offsets of definition names
	seen := map[[2]any]int{}
	for _, t := range ps.tagger.TagTree(tree) {
		kind, ok := strings.CutPrefix(t.Kind, "definition.")
		if !ok {
			continue
		}
		name := shortName(t.Name)
		if name == "" {
			continue
		}
		line := int(t.Range.StartPoint.Row) + 1
		end := int(t.Range.EndPoint.Row) + 1
		if t.Range.EndPoint.Column == 0 && end > line {
			end--
		}
		at[t.NameRange.StartByte] = true
		d := def{name: name, kind: kind, line: line, end: end, sig: signature(lines, line)}
		// One name on one line is one definition; the specific kind wins
		// over the catch-all constant and type patterns.
		key := [2]any{name, line}
		if i, dup := seen[key]; dup {
			if weak(defs[i].kind) && !weak(kind) {
				defs[i] = d
			}
			continue
		}
		seen[key] = len(defs)
		defs = append(defs, d)
	}

	var refs []ref
	have := map[ref]int{} // name and line, strong left false
	cur := ts.NewTreeCursorFromTree(tree)
	for {
		n := cur.CurrentNode()
		if n.ChildCount() == 0 && ps.idents[n.Symbol()] && !at[n.StartByte()] {
			if name := n.Text(src); len(name) > 1 {
				r := ref{name: name, line: int(n.StartPoint().Row) + 1}
				strong := ps.strong(n)
				if i, ok := have[r]; ok {
					refs[i].strong = refs[i].strong || strong
				} else {
					have[r] = len(refs)
					r.strong = strong
					refs = append(refs, r)
				}
			}
		}
		if cur.GotoFirstChild() {
			continue
		}
		for !cur.GotoNextSibling() {
			if !cur.GotoParent() {
				return defs, refs
			}
		}
	}
}

// strong reports whether the identifier n is called, constructed or
// used as a type: f(), x.f(), new F(), F{} or a type name.
func (ps *parser) strong(n *ts.Node) bool {
	if ps.types[n.Symbol()] {
		return true
	}
	q := n
	p := q.Parent()
	for range 2 { // Kotlin nests a.b as a navigation suffix
		if p != nil && ps.member[p.Symbol()] && isLastNamed(p, q) {
			q, p = p, p.Parent()
		}
	}
	return p != nil && ps.calls[p.Symbol()] && (isFirstNamed(p, q) || !isLastNamed(p, q))
}

// isFirstNamed reports whether c is p's first named child.
func isFirstNamed(p, c *ts.Node) bool {
	f := p.NamedChild(0)
	return f != nil && f.StartByte() == c.StartByte() && f.EndByte() == c.EndByte()
}

// isLastNamed reports whether c is p's last named child.
func isLastNamed(p, c *ts.Node) bool {
	k := p.NamedChildCount()
	if k == 0 {
		return false
	}
	l := p.NamedChild(k - 1)
	return l != nil && l.StartByte() == c.StartByte() && l.EndByte() == c.EndByte()
}

// weak kinds come from patterns that also match more specific ones.
func weak(kind string) bool { return kind == "constant" || kind == "type" || kind == "variable" }

// shortName drops a qualifier: Foo::bar and a.b name bar and b.
func shortName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "::"); i >= 0 {
		s = s[i+2:]
	}
	if i := strings.LastIndexAny(s, ".\\"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimLeft(s, "~*&")
	if strings.ContainsAny(s, " \t\n(){}") {
		return ""
	}
	return s
}

// signature is the definition's first line, tidied and capped.
func signature(lines [][]byte, line int) string {
	if line < 1 || line > len(lines) {
		return ""
	}
	s := strings.Join(strings.Fields(string(lines[line-1])), " ")
	s = strings.TrimSpace(strings.TrimSuffix(s, "{"))
	if utf8.RuneCountInString(s) > maxSig {
		s = string([]rune(s)[:maxSig]) + "..."
	}
	return s
}

// refGroup is every line of one file that uses one name, stored as one
// row: a file has far fewer names than identifier lines.
type refGroup struct {
	name          string
	lines         string // "12 *40 77": a star marks a strong use
	strong, total int
}

// group folds a file's refs into one group per name, in first-use order.
func group(refs []ref) []refGroup {
	at := map[string]int{}
	var out []refGroup
	var b []byte
	for _, r := range refs {
		i, ok := at[r.name]
		if !ok {
			i = len(out)
			at[r.name] = i
			out = append(out, refGroup{name: r.name})
		}
		g := &out[i]
		b = b[:0]
		if g.lines != "" {
			b = append(b, ' ')
		}
		if r.strong {
			b = append(b, '*')
			g.strong++
		}
		b = strconv.AppendInt(b, int64(r.line), 10)
		g.lines += string(b)
		g.total++
	}
	return out
}

type packedLine struct {
	line   int
	strong bool
}

// unpackLines reads a refGroup's lines back.
func unpackLines(s string) []packedLine {
	var out []packedLine
	for _, f := range strings.Fields(s) {
		strong := strings.HasPrefix(f, "*")
		n, err := strconv.Atoi(strings.TrimPrefix(f, "*"))
		if err == nil {
			out = append(out, packedLine{n, strong})
		}
	}
	return out
}
