// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package slug turns titles into URL slugs.
package slug

import (
	"strings"
	"unicode"
)

// Slugify lowercases s, keeps letters and digits and joins the words
// with single hyphens.
func Slugify(s string) string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return strings.Join(words, "-")
}
