// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Candidate is a skill tacet would draft from a repeated instruction.
type Candidate struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Count       int      `json:"count"`
	Sessions    int      `json:"sessions"`
	Score       int      `json:"score"`
	Example     string   `json:"example"`
	Variants    []string `json:"variants,omitempty"`
	Path        string   `json:"path"`
	Exists      bool     `json:"exists,omitempty"` // a skill by that name is already there
}

// maxDetails caps the extra lines a draft takes from the variants.
const maxDetails = 8

// Candidates mines root's transcripts and names a skill for each
// repeated instruction, best first.
func Candidates(root string) []Candidate {
	return FromClusters(root, Mine(Prompts(root)))
}

// FromClusters names and describes clusters, keeping names unique.
func FromClusters(root string, cs []Cluster) []Candidate {
	used := map[string]bool{}
	var out []Candidate
	for _, c := range cs {
		name := uniqueName(Name(c.Texts), used)
		path := filepath.Join(root, ".claude", "skills", name, "SKILL.md")
		_, err := os.Stat(path)
		out = append(out, Candidate{
			Name: name, Description: Describe(c.Texts[0]),
			Count: c.Count, Sessions: c.Sessions, Score: c.Score(),
			Example: c.Texts[0], Variants: c.Texts[1:],
			Path: path, Exists: err == nil,
		})
	}
	return out
}

func uniqueName(name string, used map[string]bool) string {
	n := name
	for i := 2; used[n]; i++ {
		n = fmt.Sprintf("%s-%d", name, i)
	}
	used[n] = true
	return n
}

// maxNameWords keeps a skill name short enough to type.
const maxNameWords = 4

// Name is a kebab-case skill name from the words the requests share
// most, in the order the first request uses them.
func Name(texts []string) string {
	if len(texts) == 0 {
		return "repeated-request"
	}
	df := map[string]int{}
	for _, t := range texts {
		seen := map[string]bool{}
		for _, w := range strings.Fields(normalize(t)) {
			w = strings.ReplaceAll(w, "'", "")
			if stop[w] || len(w) < 2 || seen[w] || hasDigit(w) {
				continue
			}
			seen[w] = true
			df[w]++
		}
	}
	var order []string
	pos := map[string]int{}
	for _, w := range strings.Fields(normalize(texts[0])) {
		w = strings.ReplaceAll(w, "'", "")
		if n, ok := df[w]; ok && n*2 >= len(texts) { // shared by at least half
			if _, dup := pos[w]; !dup {
				pos[w] = len(order)
				order = append(order, w)
			}
		}
	}
	ranked := append([]string{}, order...)
	sort.SliceStable(ranked, func(i, j int) bool { return df[ranked[i]] > df[ranked[j]] })
	if len(ranked) > maxNameWords {
		ranked = ranked[:maxNameWords]
	}
	sort.SliceStable(ranked, func(i, j int) bool { return pos[ranked[i]] < pos[ranked[j]] })
	name := strings.Join(ranked, "-")
	if len(name) > 48 {
		name = strings.TrimRight(name[:48], "-")
	}
	if name == "" {
		return "repeated-request"
	}
	return name
}

// hasDigit is true for ids and numbers (r2805, 42) but not for short
// terms like e2e or s3: digits at least as many as letters.
func hasDigit(w string) bool {
	d, l := 0, 0
	for _, r := range w {
		if r >= '0' && r <= '9' {
			d++
		} else {
			l++
		}
	}
	return d > 0 && (d >= l || len(w) > 6)
}

var politeness = []string{
	"can you please ", "could you please ", "would you please ", "can you ", "could you ", "would you ", "will you ",
	"please ", "pls ", "i want you to ", "i need you to ", "i'd like you to ", "make sure to ", "make sure you ",
	"ensure to ", "remember to ", "always ", "also ", "and ", "then ", "so ", "next ", "ok ", "okay ", "now ",
}

// Describe is a one-sentence description that triggers the skill,
// built from the representative request.
func Describe(text string) string {
	s := unpolite(strings.Split(strings.TrimSpace(text), "\n")[0])
	if r := []rune(s); len(r) > 200 {
		s = strings.TrimSpace(string(r[:200])) + "..."
	}
	if s == "" {
		return "Use when asked to repeat this request."
	}
	return "Use when asked to " + lowerFirst(s) + "."
}

// unpolite drops the courtesy and filler a request opens with.
func unpolite(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), ".!? ")
	for changed := true; changed; {
		changed = false
		low := strings.ToLower(s) + " "
		for _, p := range politeness {
			if strings.HasPrefix(low, p) {
				s = strings.TrimSpace((s + " ")[len(p):])
				changed = true
				break
			}
		}
	}
	return s
}

func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) > 1 && r[0] >= 'A' && r[0] <= 'Z' && (r[1] < 'A' || r[1] > 'Z') {
		r[0] += 'a' - 'A'
	}
	return string(r)
}

// Draft is the SKILL.md for a candidate: the representative request
// plus the details its variants added, deduplicated and masked.
func Draft(c Candidate) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + c.Name + "\n")
	b.WriteString("description: " + quote(Mask(c.Description)) + "\n")
	b.WriteString("---\n")
	fmt.Fprintf(&b, "<!-- tacet · drafted from %d repeated requests -->\n\n", c.Count)
	b.WriteString("# " + title(c.Name) + "\n\n")
	b.WriteString(Mask(c.Example) + "\n")
	if d := details(c.Example, c.Variants); len(d) > 0 {
		b.WriteString("\n## Details from similar requests\n\n")
		for _, x := range d {
			b.WriteString("- " + Mask(x) + "\n")
		}
	}
	return b.String()
}

// details are the variants' words the representative doesn't already
// say: a variant whose content words mostly appear in it adds nothing.
func details(rep string, variants []string) []string {
	have := contentWords(rep)
	var out []string
	for _, v := range variants {
		ws := contentWords(v)
		if len(ws) == 0 {
			continue
		}
		fresh := 0
		for w := range ws {
			if !have[w] {
				fresh++
			}
		}
		if fresh*3 < len(ws) { // under a third new: a rewording
			continue
		}
		out = append(out, strings.TrimSpace(v))
		for w := range ws {
			have[w] = true
		}
		if len(out) == maxDetails {
			break
		}
	}
	return out
}

func contentWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(normalize(s)) {
		if !stop[w] {
			out[w] = true
		}
	}
	return out
}

func title(kebab string) string {
	ws := strings.Split(kebab, "-")
	if len(ws) > 0 && ws[0] != "" {
		ws[0] = strings.ToUpper(ws[0][:1]) + ws[0][1:]
	}
	return strings.Join(ws, " ")
}

// quote writes a YAML double-quoted scalar.
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + strings.ReplaceAll(s, "\n", " ") + `"`
}
