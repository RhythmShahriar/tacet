// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package skills

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"

	"github.com/RhythmShahriar/tacet/claude"
)

// MaxDescription is the length a drafted or shortened description keeps.
const MaxDescription = 300

// boilerplate opens most descriptions and says nothing about the
// trigger, so overlap checks skip it. Longest first.
var boilerplate = []string{
	"use this skill when the user asks to ", "use this skill when the user wants to ", "use this skill when you need to ",
	"use this skill when asked to ", "use this skill when ", "use this skill to ", "use this skill for ",
	"use when the user asks to ", "use when the user wants to ", "use when you need to ", "use when asked to ",
	"use when ", "use this when ", "use for ", "use to ", "when the user asks to ", "when asked to ", "when ",
}

// trigger is a description without its boilerplate opening, normalized.
func trigger(desc string) string {
	s := normalize(desc) + " "
	for _, p := range boilerplate {
		if strings.HasPrefix(s, p) {
			s = s[len(p):]
			break
		}
	}
	return strings.TrimSpace(s)
}

// Similarity is the word 3-gram Jaccard of two skill descriptions,
// leaving out the "Use when" opening they all share.
func Similarity(a, b string) float64 {
	sa, sb := shingles(trigger(a)), shingles(trigger(b))
	if len(sa) == 0 || len(sb) == 0 {
		return 0
	}
	return jaccard(sa, sb)
}

// leadWords is how many words make a description's trigger phrase.
const leadWords = 3

// Leading is the first words after the boilerplate: two skills that
// open the same way fight over the same requests. Empty when the
// description is too short to tell.
func Leading(desc string) string {
	ws := strings.Fields(trigger(desc))
	if len(ws) < leadWords {
		return ""
	}
	content := 0
	for _, w := range ws[:leadWords] {
		if !stop[w] {
			content++
		}
	}
	if content == 0 {
		return ""
	}
	return strings.Join(ws[:leadWords], " ")
}

// A sentence ends at . ! or ? before a capital or the end, so "e.g. x"
// stays in one sentence.
var firstEnd = regexp.MustCompile(`[.!?](\s+[A-Z"'(\[]|\s*$)`)

// FirstSentence splits text after its first sentence, cut to max runes
// at a word boundary; rest is everything after what was kept.
func FirstSentence(text string, max int) (first, rest string) {
	text = strings.Join(strings.Fields(text), " ")
	end := len(text)
	if loc := firstEnd.FindStringIndex(text); loc != nil {
		end = loc[0] + 1
	}
	first = text[:end]
	if r := []rune(first); len(r) > max {
		cut := string(r[:max-3])
		if i := strings.LastIndex(cut, " "); i > 0 {
			cut = cut[:i]
		}
		first = strings.TrimRight(cut, ",;: ")
		rest = strings.TrimSpace(text[len(first):])
		return first + "...", rest
	}
	return first, strings.TrimSpace(text[end:])
}

// Shorten keeps the first sentence of a SKILL.md's description (and
// when_to_use) in the frontmatter, at most MaxDescription characters,
// and moves the rest into the body under "## When to use". ok is false
// when there is nothing to move.
func Shorten(b []byte) (out []byte, ok bool) {
	s := string(b)
	if !strings.HasPrefix(s, "---\n") {
		return b, false
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return b, false
	}
	fm := claude.Frontmatter(bufio.NewReader(bytes.NewReader(b)))
	full := unescape(strings.TrimSpace(fm["description"] + " " + fm["when_to_use"]))
	first, rest := FirstSentence(full, MaxDescription)
	if rest == "" {
		return b, false
	}
	head, after := s[4:4+end], s[4+end+4:] // after the closing "---"
	var kept []string
	skipping := false
	for _, l := range strings.Split(head, "\n") {
		indented := strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")
		if skipping && (indented || strings.TrimSpace(l) == "") {
			continue
		}
		skipping = false
		switch {
		case strings.HasPrefix(l, "description:"):
			kept = append(kept, "description: "+quote(first))
			skipping = true
		case strings.HasPrefix(l, "when_to_use:"):
			skipping = true
		default:
			kept = append(kept, l)
		}
	}
	nl := ""
	if strings.HasPrefix(after, "\n") {
		nl, after = "\n", after[1:]
	}
	section := "## When to use\n\n" + rest + "\n"
	lines := strings.SplitAfter(after, "\n")
	i := 0
	for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "<!--") {
		i++ // keep header comments first
	}
	j := i
	for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
		j++
	}
	if j < len(lines) && strings.HasPrefix(lines[j], "# ") {
		i = j + 1 // and the title
	}
	for k := range lines {
		if strings.TrimSpace(lines[k]) == "## When to use" {
			lines[k] = "## When to use\n\n" + rest + "\n"
			return []byte("---\n" + strings.Join(kept, "\n") + "\n---" + nl + strings.Join(lines, "")), true
		}
	}
	bodyRest := strings.TrimLeft(strings.Join(lines[i:], ""), "\n")
	body := strings.Join(lines[:i], "")
	if strings.TrimSpace(body) != "" {
		body = strings.TrimRight(body, "\n") + "\n\n"
	}
	body += section
	if bodyRest != "" {
		body += "\n" + bodyRest
	}
	return []byte("---\n" + strings.Join(kept, "\n") + "\n---" + nl + body), true
}

func unescape(s string) string {
	return strings.NewReplacer(`\\`, `\`, `\"`, `"`).Replace(s)
}
