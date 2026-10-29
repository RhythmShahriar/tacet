// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package convert

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/RhythmShahriar/tacet/tokens"
)

// FocusTokens caps the sections a focused read adds to the outline.
const FocusTokens = 4_000

// Focus writes a file holding the outline and the sections of the full
// text that best match what the person asked, so a question about one
// part of a long document is answered from a single read. It returns ""
// when nothing matches well enough, and the caller uses the outline.
func Focus(outlinePath, mdPath, ask string) string {
	terms := words(ask)
	if len(terms) == 0 {
		return ""
	}
	ob, err := os.ReadFile(outlinePath) // #nosec G304 -- tacet's own cache
	if err != nil {
		return ""
	}
	db, err := os.ReadFile(mdPath) // #nosec G304 -- tacet's own cache
	if err != nil {
		return ""
	}
	lines := strings.Split(string(db), "\n")
	secs := leafSections(lines)
	if len(secs) < 3 {
		return ""
	}
	// BM25-style scoring: rare terms count more, long sections don't win
	// just by being long.
	df := map[string]int{}
	tfs := make([]map[string]int, len(secs))
	avg := 0.0
	for i, s := range secs {
		tf := map[string]int{}
		for _, w := range words(s.text) {
			tf[w]++
		}
		tfs[i] = tf
		for w := range tf {
			df[w]++
		}
		avg += float64(len(s.text))
	}
	avg /= float64(len(secs))
	type hit struct {
		i     int
		score float64
	}
	var hits []hit
	for i, s := range secs {
		sc := 0.0
		for _, t := range terms {
			f := float64(tfs[i][t])
			if f == 0 {
				continue
			}
			idf := math.Log(1 + (float64(len(secs))-float64(df[t])+0.5)/(float64(df[t])+0.5))
			sc += idf * f * 2.2 / (f + 1.2*(0.25+0.75*float64(len(s.text))/avg))
		}
		if sc > 0 {
			hits = append(hits, hit{i, sc})
		}
	}
	if len(hits) == 0 {
		return ""
	}
	sort.Slice(hits, func(a, b int) bool { return hits[a].score > hits[b].score })
	// Keep the sections that score close to the best one, within budget.
	var picked []section
	budget := FocusTokens
	for _, h := range hits {
		if h.score < hits[0].score*0.5 || len(picked) == 3 {
			break
		}
		s := secs[h.i]
		if t := tokens.Estimate(s.text); t <= budget {
			picked = append(picked, s)
			budget -= t
		}
	}
	if len(picked) == 0 {
		return ""
	}
	sort.Slice(picked, func(a, b int) bool { return picked[a].start < picked[b].start })
	var b strings.Builder
	b.Write(ob)
	b.WriteString("\nSections of the full text that match the request (more may be relevant; read others by offset and limit):\n")
	for _, s := range picked {
		fmt.Fprintf(&b, "\n--- full text lines %d-%d ---\n%s\n", s.start, s.end, s.text)
	}
	sum := sha256.Sum256([]byte(ask))
	out := strings.TrimSuffix(outlinePath, ".outline.md") + ".focus-" + hex.EncodeToString(sum[:6]) + ".md"
	if err := os.WriteFile(out, []byte(b.String()), 0o600); err != nil {
		return ""
	}
	return out
}

type section struct {
	start, end int // 1-based file lines
	text       string
}

// leafSections splits the text at every heading, so each section is the
// smallest titled unit.
func leafSections(lines []string) []section {
	var out []section
	start := -1
	flush := func(end int) {
		if start >= 0 && end > start {
			out = append(out, section{start + 1, end, strings.Join(lines[start:end], "\n")})
		}
	}
	inFence := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(l, "#") {
			flush(i)
			start = i
		}
	}
	flush(len(lines))
	return out
}

var stop = map[string]bool{
	"the": true, "and": true, "for": true, "what": true, "which": true, "does": true, "this": true, "that": true,
	"with": true, "from": true, "are": true, "how": true, "its": true, "into": true, "one": true, "line": true,
	"say": true, "says": true, "about": true, "can": true, "you": true, "please": true, "tell": true, "read": true,
	"pdf": true, "docx": true, "document": true, "file": true, "case": true, "mean": true, "means": true, "have": true,
}

// words lowercases s into the terms used for matching, dropping short
// words, stop words and file names.
func words(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(w) < 2 || stop[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}
