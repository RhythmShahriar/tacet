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
// monospace font produced, and hard line wraps. Paragraph breaks,
// headings, lists, tables and code fences stay.
func Tighten(md string) string {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")

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
