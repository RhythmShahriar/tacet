// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package handoff writes SESSION.md, a short note of where a session
// left off, from the transcript and git alone (no model call), and
// decides when the next session should see it.
package handoff

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/claude"
)

// MaxLines caps SESSION.md so it stays cheap to inject.
const MaxLines = 60

// Fresh is how long a note is worth injecting into a new session.
const Fresh = 72 * time.Hour

// File is the note's name at the project root.
const File = "SESSION.md"

// Header marks the file as tacet's, so tacet only ever rewrites its own.
const Header = "<!-- tacet · SESSION.md is written by tacet at compact and session end; edits are kept until the next write -->"

// Path is the note's path in root.
func Path(root string) string { return filepath.Join(root, File) }

// Write builds the note for a session and writes it, unless a SESSION.md
// that tacet didn't write is already there.
func Write(root string, s *claude.Session, now time.Time) error {
	p := Path(root)
	if b, err := os.ReadFile(p); err == nil && !strings.HasPrefix(string(b), Header) { // #nosec G304 -- the project's own note
		return nil
	}
	note := Build(root, s, now)
	if note == "" {
		return nil
	}
	tmp := p + ".tacet-tmp"
	if err := os.WriteFile(tmp, []byte(note), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Build renders the note; empty when the session has nothing to hand on.
func Build(root string, s *claude.Session, now time.Time) string {
	var asks []string
	lastReply := ""
	for _, t := range s.Turns {
		if t.Human && t.Text != "" {
			asks = append(asks, t.Text)
		}
		if t.Role == "assistant" && strings.TrimSpace(t.Text) != "" {
			lastReply = t.Text
		}
	}
	if len(asks) == 0 {
		return ""
	}
	var b strings.Builder
	line := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	line(Header)
	line("# Session handoff")
	line("")
	branch := git(root, "rev-parse", "--abbrev-ref", "HEAD")
	meta := "Updated " + now.UTC().Format("2006-01-02 15:04 UTC")
	if branch != "" {
		meta += " on branch `" + branch + "`"
	}
	line("%s. Session `%s`.", meta, short(s.ID, 8))
	line("")
	line("## Goal")
	line("%s", clip(asks[0], 400))
	if len(asks) > 1 {
		line("")
		line("## Latest asks")
		from := len(asks) - 3
		if from < 1 {
			from = 1
		}
		for _, a := range asks[from:] {
			line("- %s", clip(oneLine(a), 200))
		}
	}
	if st := git(root, "status", "--porcelain"); st != "" {
		line("")
		line("## Uncommitted changes")
		files := strings.Split(st, "\n")
		for i, f := range files {
			if i == 12 {
				line("- and %d more", len(files)-12)
				break
			}
			line("- `%s`", strings.TrimSpace(f))
		}
	}
	if lg := git(root, "log", "-5", "--format=%h %s", "--since=3 days ago"); lg != "" {
		line("")
		line("## Recent commits")
		for _, l := range strings.Split(lg, "\n") {
			line("- %s", l)
		}
	}
	if lastReply != "" {
		line("")
		line("## Where it stopped")
		lines := strings.Split(strings.TrimSpace(lastReply), "\n")
		if len(lines) > 12 {
			lines = lines[len(lines)-12:]
		}
		for _, l := range lines {
			line("> %s", clip(l, 240))
		}
	}
	out := strings.Split(b.String(), "\n")
	if len(out) > MaxLines {
		out = append(out[:MaxLines-1], "")
	}
	return strings.Join(out, "\n")
}

// ForSession returns the note to inject at session start: only when it
// is tacet's, fresh and from another session.
func ForSession(root, sessionID string, now time.Time) string {
	p := Path(root)
	fi, err := os.Stat(p)
	if err != nil || now.Sub(fi.ModTime()) > Fresh {
		return ""
	}
	b, err := os.ReadFile(p) // #nosec G304 -- the project's own note
	if err != nil || !strings.HasPrefix(string(b), Header) {
		return ""
	}
	if sessionID != "" && strings.Contains(string(b), "Session `"+short(sessionID, 8)+"`") {
		return ""
	}
	return string(b)
}

func git(root string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output() // #nosec G204 -- fixed git subcommands
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func short(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
