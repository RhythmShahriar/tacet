// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package convert

import (
	"regexp"
	"strings"
)

var (
	pageNumRe = regexp.MustCompile(`^\s*(page\s+)?\d{1,4}(\s*(/|of)\s*\d{1,4})?\s*$`)
	blockRe   = regexp.MustCompile(`^\s*([-*+>|#]|\d+[.)]\s|` + "```" + `)`)
)

// Tighten removes what costs tokens and carries nothing: page numbers,
// headers and footers repeated on every page, whole-line code marks a
// monospace font produced and hard line wraps. Paragraph breaks,
// headings, lists, tables and code fences stay.
func Tighten(md string) string {
	lines := mergeFragments(strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n"))

	// Lines wrapped whole in backticks: a monospace font, not code. Only
	// unwrap when it is the document's habit, so real inline code stays.
	wrapped, text := 0, 0
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		text++
		if isWrapped(t) {
			wrapped++
		}
	}
	if text > 0 && wrapped*3 > text {
		for i, l := range lines {
			if t := strings.TrimSpace(l); isWrapped(t) {
				lines[i] = t[1 : len(t)-1]
			}
		}
	}

	// Short lines repeated about once a page are running headers and
	// footers. Pages are counted from the page-number lines.
	count := map[string]int{}
	pages := 0
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" && pageNumRe.MatchString(t) {
			pages++
		}
		if t != "" && len(t) < 60 && !blockRe.MatchString(t) {
			count[t]++
		}
	}
	headerMin := pages / 2
	if headerMin < 4 {
		headerMin = 4
	}

	var out []string
	inFence := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		// "\## Title": a heading typed as text, which converters escape.
		if strings.HasPrefix(t, "\\#") {
			t = strings.TrimLeft(t, "\\")
		}
		if strings.HasPrefix(t, "```") {
			inFence = !inFence
			out = append(out, l)
			continue
		}
		if inFence {
			out = append(out, l)
			continue
		}
		if pageNumRe.MatchString(t) && t != "" {
			continue
		}
		if pages >= 4 && count[t] >= headerMin {
			continue
		}
		if t == "" {
			if n := len(out); n > 0 && out[n-1] != "" {
				out = append(out, "")
			}
			continue
		}
		// Join a wrapped line onto the paragraph it continues.
		if n := len(out); n > 0 && joinable(out[n-1], t) {
			prev := out[n-1]
			if strings.HasSuffix(prev, "-") && startsLower(t) {
				out[n-1] = prev[:len(prev)-1] + t
			} else {
				out[n-1] = prev + " " + t
			}
			continue
		}
		out = append(out, t)
	}
	return strings.TrimSpace(strings.Join(out, "\n")) + "\n"
}

func isWrapped(t string) bool {
	return len(t) > 2 && t[0] == '`' && t[len(t)-1] == '`' && !strings.HasPrefix(t, "```")
}

func joinable(prev, next string) bool {
	if prev == "" || blockRe.MatchString(next) || strings.HasPrefix(prev, "#") || strings.HasPrefix(prev, "|") {
		return false
	}
	if strings.HasSuffix(prev, "  ") || strings.HasSuffix(prev, ":") && blockRe.MatchString(next) {
		return false
	}
	return true
}

func startsLower(s string) bool { return s != "" && s[0] >= 'a' && s[0] <= 'z' }

var (
	fragmentRe = regexp.MustCompile(`^#{1,6}\s+(\S.{0,60}[.;,)])$`)
	capsRe     = regexp.MustCompile(`[A-Z0-9][A-Z0-9-]*$`)
	capsHeadRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]*`)
)

// mergeFragments undoes a converter mistake: the wrapped end of a line,
// often bold, comes out as a heading of its own ("#### LANTERN-17."),
// which splits a fact in two. A "heading" that is a short fragment ending
// in punctuation, right after a line cut off mid-sentence, joins that
// line again. Two code-like capitalized words meet at a hyphen, the one
// the line break took.
func mergeFragments(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		m := fragmentRe.FindStringSubmatch(strings.TrimSpace(l))
		prev := lastText(out)
		if m == nil || prev < 0 || !unfinished(out[prev]) {
			out = append(out, l)
			continue
		}
		frag := m[1]
		p := strings.TrimRight(out[prev], " ")
		bold := strings.HasSuffix(p, "**")
		p = strings.TrimSuffix(p, "**")
		sep := " "
		if capsRe.MatchString(p) && capsHeadRe.MatchString(frag) {
			sep = "-"
		}
		joined := p + sep + frag
		if bold {
			joined += "**"
		}
		out[prev] = joined
		out = out[:prev+1] // drop the blank lines between them
	}
	return out
}

// lastText is the index of the last non-blank line, or -1.
func lastText(lines []string) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return i
		}
	}
	return -1
}

// unfinished reports whether a line stops mid-sentence: no closing
// punctuation once bold marks are removed and not itself a heading,
// list item or table row.
func unfinished(l string) bool {
	t := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(l), "**"), "**"))
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "|") || blockRe.MatchString(t) {
		return false
	}
	switch t[len(t)-1] {
	case '.', ':', '?', '!', ';', ')', '"':
		return false
	}
	return true
}
