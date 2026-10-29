// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package codegraph

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/RhythmShahriar/tacet/tokens"
)

// MapFile is one file of a ranked map with the symbols chosen from it.
type MapFile struct {
	Path    string   `json:"path"`
	Rank    float64  `json:"rank"`
	Symbols []Symbol `json:"symbols"`
}

// weakShare is what a plain mention of a name counts against a call.
const weakShare = 0.1

// testShare scales the score of symbols defined in test files.
const testShare = 0.1

// DefaultBudget is the map's size when none is given, in tokens.
const DefaultBudget = 1024

// Map ranks the project's files by how much the rest of the code uses
// them (PageRank over the file reference graph) and its symbols by the
// references they get, then keeps the best ones that fit in budget
// tokens. With dir set, only files under dir are shown.
func (ix *Index) Map(ctx context.Context, budget int, dir string) ([]MapFile, error) {
	if budget <= 0 {
		budget = DefaultBudget
	}
	dir = strings.Trim(ix.Rel(dir), "/")
	if dir == "." {
		dir = ""
	}

	paths := map[int64]string{}
	fam := map[int64]string{}
	rows, err := ix.db.QueryContext(ctx, `SELECT id, path, lang FROM files`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var p, lang string
		if err := rows.Scan(&id, &p, &lang); err != nil {
			_ = rows.Close()
			return nil, err
		}
		paths[id] = p
		fam[id] = family(lang)
	}
	_ = rows.Close()

	type defRow struct {
		file int64
		Symbol
	}
	var defs []defRow
	defFiles := map[string][]int64{} // name -> files defining it, once each
	rows, err = ix.db.QueryContext(ctx, `SELECT file, name, kind, line, end_line, sig FROM defs`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d defRow
		if err := rows.Scan(&d.file, &d.Name, &d.Kind, &d.Line, &d.End, &d.Signature); err != nil {
			_ = rows.Close()
			return nil, err
		}
		d.Path = paths[d.file]
		defs = append(defs, d)
		if fs := defFiles[d.Name]; !containsID(fs, d.file) {
			defFiles[d.Name] = append(fs, d.file)
		}
	}
	_ = rows.Close()

	// Edges: file A uses a name file B defines. A name defined in several
	// files splits its weight between them.
	type key struct {
		file int64
		name string
	}
	out := map[int64]map[int64]float64{} // from -> to -> weight
	outSum := map[int64]float64{}
	uses := map[key][]struct {
		from int64
		w    float64
	}{}
	rows, err = ix.db.QueryContext(ctx, `SELECT file, name, strong, total FROM refs
WHERE name IN (SELECT name FROM defs)`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var from int64
		var name string
		var strong, all int
		if err := rows.Scan(&from, &name, &strong, &all); err != nil {
			_ = rows.Close()
			return nil, err
		}
		// A plain mention may be a local that shares the name: it counts
		// a tenth of a call or a type use.
		n := float64(strong) + weakShare*float64(all-strong)
		if builtins[fam[from]][name] {
			continue
		}
		var targets []int64
		for _, to := range defFiles[name] {
			if fam[to] == fam[from] {
				targets = append(targets, to)
			}
		}
		if len(targets) == 0 {
			continue
		}
		w := nameWeight(name, len(targets)) * math.Sqrt(n) / float64(len(targets))
		for _, to := range targets {
			if to == from {
				continue
			}
			if out[from] == nil {
				out[from] = map[int64]float64{}
			}
			out[from][to] += w
			outSum[from] += w
			uses[key{to, name}] = append(uses[key{to, name}], struct {
				from int64
				w    float64
			}{from, w})
		}
	}
	_ = rows.Close()

	rank := pageRank(paths, out, outSum)

	// A symbol's score is the rank that flows to it along its edges.
	score := map[key]float64{}
	for k, us := range uses {
		for _, u := range us {
			score[k] += rank[u.from] * u.w / outSum[u.from]
		}
		if isTest(paths[k.file]) {
			score[k] *= testShare // test helpers rarely answer a question about the code
		}
	}
	sort.SliceStable(defs, func(i, j int) bool {
		a, b := score[key{defs[i].file, defs[i].Name}], score[key{defs[j].file, defs[j].Name}]
		if a != b {
			return a > b
		}
		if ra, rb := rank[defs[i].file], rank[defs[j].file]; ra != rb {
			return ra > rb
		}
		if defs[i].Path != defs[j].Path {
			return defs[i].Path < defs[j].Path
		}
		return defs[i].Line < defs[j].Line
	})

	// Keep the best symbols that fit, then show them file by file.
	var order []int64
	picked := map[int64][]Symbol{}
	used := 0
	for _, d := range defs {
		if d.Signature == "" || (dir != "" && d.Path != dir && !strings.HasPrefix(d.Path, dir+"/")) {
			continue
		}
		cost := tokens.Estimate(symbolLine(d.Symbol)) + 1
		if _, ok := picked[d.file]; !ok {
			cost += tokens.Estimate(d.Path) + 1
		}
		if used+cost > budget {
			if budget-used < 8 {
				break
			}
			continue
		}
		used += cost
		if _, ok := picked[d.file]; !ok {
			order = append(order, d.file)
		}
		picked[d.file] = append(picked[d.file], d.Symbol)
	}
	files := make([]MapFile, 0, len(order))
	for _, id := range order {
		syms := picked[id]
		sort.Slice(syms, func(i, j int) bool { return syms[i].Line < syms[j].Line })
		files = append(files, MapFile{Path: paths[id], Rank: rank[id], Symbols: syms})
	}
	return files, nil
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// nameWeight favors distinctive names: a long camelCase or snake_case
// name says more than a short one many files define.
func nameWeight(name string, defined int) float64 {
	w := 1.0
	if len(name) >= 8 && (strings.Contains(name, "_") || hasInnerUpper(name)) {
		w *= 10
	}
	if strings.HasPrefix(name, "_") {
		w *= 0.1
	}
	if defined > 5 {
		w *= 0.1
	}
	return w
}

func hasInnerUpper(s string) bool {
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// pageRank runs PageRank over the file graph; files nothing links from
// spread their rank evenly.
func pageRank(nodes map[int64]string, out map[int64]map[int64]float64, outSum map[int64]float64) map[int64]float64 {
	const damping, rounds = 0.85, 30
	n := float64(len(nodes))
	rank := make(map[int64]float64, len(nodes))
	if n == 0 {
		return rank
	}
	for id := range nodes {
		rank[id] = 1 / n
	}
	for range rounds {
		next := make(map[int64]float64, len(nodes))
		dangling := 0.0
		for id, r := range rank {
			if outSum[id] == 0 {
				dangling += r
				continue
			}
			for to, w := range out[id] {
				next[to] += damping * r * w / outSum[id]
			}
		}
		base := (1-damping)/n + damping*dangling/n
		for id := range nodes {
			next[id] += base
		}
		rank = next
	}
	return rank
}

// symbolLine is how a symbol shows in a map.
func symbolLine(s Symbol) string { return fmt.Sprintf("%5d  %s", s.Line, s.Signature) }

// Render writes a map as text: each file, then its symbols with their
// line numbers.
func Render(files []MapFile) string {
	var b strings.Builder
	for _, f := range files {
		b.WriteString(f.Path)
		b.WriteString("\n")
		for _, s := range f.Symbols {
			b.WriteString(symbolLine(s))
			b.WriteString("\n")
		}
	}
	return b.String()
}
