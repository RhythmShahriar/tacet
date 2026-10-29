// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/tokens"
)

// Large tool output (feature bigoutput, off by default): a Bash or
// WebFetch result past bigTokens is kept in .tacet/out and the agent
// gets its head, tail and error lines with the path to Grep for more.
const (
	bigTokens    = 8000
	bigEdge      = 40  // lines kept from the start and from the end
	bigErrLines  = 40  // error-looking lines kept from the middle
	bigLineChars = 200 // longer lines are cut
	bigKeep      = 7 * 24 * time.Hour
)

var errLineRe = regexp.MustCompile(`(?i)\b(error|errors|fail|failed|failure|failing|fatal|panic|exception|traceback|denied|not found|undefined|cannot|can't)\b`)

func outDir(root string) string { return filepath.Join(config.Dir(root), "out") }

// postOutput replaces a big Bash or WebFetch result with a short view
// through PostToolUse updatedToolOutput. The replacement keeps the
// tool's output shape: only the long string fields change.
func postOutput(root string, cfg *config.Config, in *hookInput) any {
	if !cfg.On("bigoutput") || len(in.ToolResponse) == 0 {
		return nil
	}
	var resp any
	if json.Unmarshal(in.ToolResponse, &resp) != nil {
		return nil
	}
	changed := false
	var saved []string
	shrink := func(s string) (string, bool) {
		if tokens.Estimate(s) <= bigTokens {
			return s, false
		}
		p, err := storeOutput(root, s)
		if err != nil {
			logHook(root, "post-output", err)
			return s, false
		}
		saved = append(saved, p)
		return shortView(s, p), true
	}
	switch v := resp.(type) {
	case string:
		if s, ok := shrink(v); ok {
			resp, changed = s, true
		}
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if str, ok := v[k].(string); ok {
				if s, ok := shrink(str); ok {
					v[k], changed = s, true
				}
			}
		}
	}
	if !changed {
		return nil
	}
	return map[string]any{
		"systemMessage": fmt.Sprintf("tacet: long %s output kept in %s; the agent sees its start, end and error lines", in.ToolName, strings.Join(saved, ", ")),
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PostToolUse", "updatedToolOutput": resp,
		}}
}

// storeOutput writes the full text to .tacet/out/<hash>.md and drops
// files older than bigKeep.
func storeOutput(root, s string) (string, error) {
	dir := outDir(root)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	if es, err := os.ReadDir(dir); err == nil {
		for _, e := range es {
			if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > bigKeep {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	sum := sha256.Sum256([]byte(s))
	p := filepath.Join(dir, hex.EncodeToString(sum[:8])+".md")
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		return "", err
	}
	return p, nil
}

// shortView is the first and last bigEdge lines, the error-looking lines
// between them with their line numbers and where the rest is.
func shortView(s, path string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	cut := func(l string) string {
		if len(l) <= bigLineChars {
			return l
		}
		r := []rune(l)
		if len(r) <= bigLineChars {
			return l
		}
		return string(r[:bigLineChars]) + "…"
	}
	note := fmt.Sprintf("[tacet: %d lines, about %d tokens; full output: %s, Grep it or Read it with offset and limit]", len(lines), tokens.Estimate(s), path)
	var b strings.Builder
	if len(lines) <= 2*bigEdge {
		for _, l := range lines {
			b.WriteString(cut(l) + "\n")
		}
		b.WriteString(note)
		return b.String()
	}
	for _, l := range lines[:bigEdge] {
		b.WriteString(cut(l) + "\n")
	}
	mid := lines[bigEdge : len(lines)-bigEdge]
	var errs []string
	for i, l := range mid {
		if errLineRe.MatchString(l) {
			errs = append(errs, fmt.Sprintf("%d: %s", bigEdge+i+1, cut(strings.TrimSpace(l))))
		}
	}
	fmt.Fprintf(&b, "[... %d lines omitted", len(mid))
	if len(errs) > 0 {
		b.WriteString("; error-looking lines among them:")
	}
	b.WriteString(" ...]\n")
	if len(errs) > bigErrLines {
		errs = append(errs[:bigErrLines/2:bigErrLines/2], errs[len(errs)-bigErrLines/2:]...)
	}
	for _, e := range errs {
		b.WriteString(e + "\n")
	}
	if len(errs) > 0 {
		b.WriteString("[...]\n")
	}
	for _, l := range lines[len(lines)-bigEdge:] {
		b.WriteString(cut(l) + "\n")
	}
	b.WriteString(note)
	return b.String()
}
