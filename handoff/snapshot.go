// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package handoff

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/claude"
)

// MaxSnapshot caps the compaction snapshot, so injecting it after a
// compaction costs a few hundred tokens at most.
const MaxSnapshot = 2048

// SnapshotFresh is how long after a compaction its snapshot is injected.
const SnapshotFresh = 30 * time.Minute

// Markers fence the snapshot inside SESSION.md, so a later write can
// replace it and the session-start hook can find it.
const (
	snapStart = "<!-- tacet:snapshot -->"
	snapEnd   = "<!-- /tacet:snapshot -->"
)

// Snapshot is what a compaction summary tends to drop: the goal, the
// files touched, the last error and the open todo items.
type Snapshot struct {
	SessionID string
	Goal      string
	Latest    string // the newest ask, when it differs from the goal
	Edited    []string
	LastError string
	Todos     []string // open items, "[ ]" pending or "[~]" in progress
}

type snapLine struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	Message   *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type snapBlock struct {
	Type    string          `json:"type"`
	Name    string          `json:"name"`
	IsError bool            `json:"is_error"`
	Content json.RawMessage `json:"content"`
	Input   struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Todos        []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	} `json:"input"`
}

// ReadSnapshot builds the snapshot of the transcript at path. File paths
// under root are shown relative to it.
func ReadSnapshot(root, path string) (*Snapshot, error) {
	f, err := os.Open(path) // #nosec G304 -- the transcript Claude Code named
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	snap, err := ParseSnapshot(root, f)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err == nil {
		if s, err := claude.ParseSession(f); err == nil {
			var asks []string
			for _, t := range s.Turns {
				if t.Human && t.Text != "" {
					asks = append(asks, t.Text)
				}
			}
			if len(asks) > 0 {
				snap.Goal = asks[0]
				if last := asks[len(asks)-1]; len(asks) > 1 && last != asks[0] {
					snap.Latest = last
				}
			}
			if snap.SessionID == "" {
				snap.SessionID = s.ID
			}
		}
	}
	return snap, nil
}

// ParseSnapshot reads the tool calls and results of a transcript: the
// files Edit, Write and NotebookEdit changed, the last failing tool
// result and the newest TodoWrite list. Goal and Latest stay empty.
func ParseSnapshot(root string, r io.Reader) (*Snapshot, error) {
	s := &Snapshot{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	edited := map[string]int{}
	var order []string
	todosSeen := false
	for sc.Scan() {
		var l snapLine
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Message == nil {
			continue
		}
		if s.SessionID == "" {
			s.SessionID = l.SessionID
		}
		var bs []snapBlock
		if json.Unmarshal(l.Message.Content, &bs) != nil {
			continue
		}
		for _, b := range bs {
			switch {
			case l.Type == "assistant" && b.Type == "tool_use":
				switch b.Name {
				case "Edit", "MultiEdit", "Write", "NotebookEdit":
					p := b.Input.FilePath
					if p == "" {
						p = b.Input.NotebookPath
					}
					if p == "" {
						continue
					}
					p = relTo(root, p)
					if _, ok := edited[p]; !ok {
						order = append(order, p)
					}
					edited[p] = len(order)
				case "TodoWrite":
					todosSeen = true
					s.Todos = s.Todos[:0]
					for _, t := range b.Input.Todos {
						switch t.Status {
						case "pending":
							s.Todos = append(s.Todos, "[ ] "+oneLine(t.Content))
						case "in_progress":
							s.Todos = append(s.Todos, "[~] "+oneLine(t.Content))
						}
					}
				}
			case l.Type == "user" && b.Type == "tool_result" && b.IsError:
				if t := resultText(b.Content); t != "" {
					s.LastError = t
				}
			}
		}
	}
	if !todosSeen {
		s.Todos = nil
	}
	s.Edited = order
	return s, sc.Err()
}

// resultText is a tool result's text: a string or text blocks.
func resultText(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return strings.TrimSpace(str)
	}
	var bs []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &bs) != nil {
		return ""
	}
	var parts []string
	for _, b := range bs {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func relTo(root, p string) string {
	if root != "" && filepath.IsAbs(p) {
		if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
	}
	return filepath.ToSlash(p)
}

// Render writes the snapshot as Markdown of at most MaxSnapshot bytes,
// fenced by its markers; empty when there is nothing worth keeping.
func (s *Snapshot) Render(now time.Time) string {
	if s.Goal == "" && len(s.Edited) == 0 && s.LastError == "" && len(s.Todos) == 0 {
		return ""
	}
	head := fmt.Sprintf("%s\n## Before compaction\nTaken %s. Session `%s`.\n", snapStart, now.UTC().Format("2006-01-02 15:04 UTC"), short(s.SessionID, 8))
	var parts []string
	if s.Goal != "" {
		g := "Goal: " + clip(oneLine(s.Goal), 300)
		if s.Latest != "" {
			g += "\nLatest ask: " + clip(oneLine(s.Latest), 240)
		}
		parts = append(parts, g)
	}
	if len(s.Edited) > 0 {
		files := s.Edited
		more := ""
		if len(files) > 15 {
			more = fmt.Sprintf(" and %d more", len(files)-15)
			files = files[len(files)-15:] // the newest matter most
		}
		parts = append(parts, "Files edited: `"+strings.Join(files, "`, `")+"`"+more)
	}
	if len(s.Todos) > 0 {
		todos := s.Todos
		if len(todos) > 8 {
			todos = todos[:8]
		}
		parts = append(parts, "Open todos:\n- "+strings.Join(todos, "\n- "))
	}
	if s.LastError != "" {
		parts = append(parts, "Last error:\n> "+strings.Join(errorLines(s.LastError), "\n> "))
	}
	body := head
	room := MaxSnapshot - len(head) - len(snapEnd) - 2
	for _, p := range parts {
		if len(body)-len(head)+len(p)+1 > room {
			p = clipBytes(p, room-(len(body)-len(head))-1)
			if p == "" {
				break
			}
		}
		body += p + "\n"
	}
	return body + snapEnd + "\n"
}

// errorLines keeps the head and tail of an error, short lines only.
func errorLines(e string) []string {
	ls := strings.Split(strings.TrimSpace(e), "\n")
	if len(ls) > 8 {
		ls = append(append(ls[:4:4], "…"), ls[len(ls)-3:]...)
	}
	for i, l := range ls {
		ls[i] = clip(l, 160)
	}
	return ls
}

// clipBytes cuts s to at most n bytes on a rune boundary.
func clipBytes(s string, n int) string {
	if n <= 1 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	for len(string(r)) > n-len("…") {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// WriteSnapshot puts the snapshot into SESSION.md, replacing an older
// one. It only touches a SESSION.md tacet wrote and starts one when
// there is none.
func WriteSnapshot(root, snap string) error {
	if snap == "" {
		return nil
	}
	p := Path(root)
	b, err := os.ReadFile(p) // #nosec G304 -- the project's own note
	note := ""
	switch {
	case err == nil && !strings.HasPrefix(string(b), Header):
		return nil
	case err == nil:
		note = stripSnapshot(string(b))
	default:
		note = Header + "\n# Session handoff\n"
	}
	note = strings.TrimRight(note, "\n") + "\n\n" + snap
	tmp := p + ".tacet-tmp"
	if err := os.WriteFile(tmp, []byte(note), 0o600); err != nil { // #nosec G703 -- the project's own note
		return err
	}
	return os.Rename(tmp, p)
}

func stripSnapshot(note string) string {
	i := strings.Index(note, snapStart)
	if i < 0 {
		return note
	}
	j := strings.Index(note[i:], snapEnd)
	if j < 0 {
		return note[:i]
	}
	return note[:i] + note[i+j+len(snapEnd):]
}

// SnapshotFor returns the snapshot in SESSION.md for the session that
// just compacted: only a fresh one from that session.
func SnapshotFor(root, sessionID string, now time.Time) string {
	p := Path(root)
	fi, err := os.Stat(p)
	if err != nil || now.Sub(fi.ModTime()) > SnapshotFresh {
		return ""
	}
	b, err := os.ReadFile(p) // #nosec G304 -- the project's own note
	if err != nil || !strings.HasPrefix(string(b), Header) {
		return ""
	}
	note := string(b)
	i := strings.Index(note, snapStart)
	if i < 0 {
		return ""
	}
	j := strings.Index(note[i:], snapEnd)
	if j < 0 {
		return ""
	}
	snap := note[i : i+j+len(snapEnd)]
	if sessionID != "" && !strings.Contains(snap, "Session `"+short(sessionID, 8)+"`") {
		return ""
	}
	if len(snap) > MaxSnapshot {
		return ""
	}
	return snap
}
