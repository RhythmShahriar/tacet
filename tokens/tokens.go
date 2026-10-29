// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package tokens estimates how many tokens a text costs. The offline
// estimate is labeled as one everywhere it is shown.
package tokens

import "unicode/utf8"

// Estimate returns the approximate token count of s. Claude's tokenizer
// isn't published; about 3.5 characters a token matches English prose
// and code closely enough to rank costs.
func Estimate(s string) int {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return 0
	}
	return (n*2 + 6) / 7
}

// EstimateBytes is Estimate for a file's contents.
func EstimateBytes(b []byte) int { return Estimate(string(b)) }
