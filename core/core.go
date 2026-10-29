// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package core holds the types every engine shares: findings, fixes and
// the edits a fix makes. Engines only add to it.
package core

// Severity ranks a finding by how much it costs or risks.
type Severity string

// Severities, highest first.
const (
	High   Severity = "high"
	Medium Severity = "medium"
	Low    Severity = "low"
	Info   Severity = "info"
)

// Finding is one thing tacet noticed. Tokens is the estimated saving on
// every request if the finding is fixed; 0 when it isn't about tokens.
type Finding struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Path     string   `json:"path,omitempty"`
	Line     int      `json:"line,omitempty"`
	Message  string   `json:"message"`
	Hint     string   `json:"hint,omitempty"`
	Tokens   int      `json:"tokens,omitempty"`
	Fix      *Fix     `json:"fix,omitempty"`
}

// Fix is a change tacet can make for a finding. Safe fixes are applied by
// init after one confirmation; the rest wait as suggestions.
type Fix struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Safe   bool   `json:"safe"`
	Edits  []Edit `json:"-"`
	Tokens int    `json:"tokens,omitempty"`
}

// Edit changes one file. With Change set, the edit is the function from
// the file's current bytes (nil when missing) to its new bytes, so
// several fixes to one file compose. Without it, the file must still
// hold Old (nil: must not exist) and becomes New (nil: removed).
type Edit struct {
	Path   string
	Old    []byte
	New    []byte
	Change func(cur []byte) ([]byte, error)
}

// Rank orders findings by tokens saved, then severity.
func Rank(fs []Finding) {
	order := map[Severity]int{High: 0, Medium: 1, Low: 2, Info: 3}
	for i := 1; i < len(fs); i++ {
		for j := i; j > 0; j-- {
			a, b := fs[j-1], fs[j]
			if a.Tokens > b.Tokens || (a.Tokens == b.Tokens && order[a.Severity] <= order[b.Severity]) {
				break
			}
			fs[j-1], fs[j] = b, a
		}
	}
}
