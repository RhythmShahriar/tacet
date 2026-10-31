// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package clean

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Summary is what one inspection found, kept in .tacet/clean.json so
// the audit can report sizes without walking every worktree again.
type Summary struct {
	Time   time.Time `json:"time"`
	Agent  int       `json:"agent_worktrees"`
	Safe   int       `json:"safe"`
	Bytes  int64     `json:"bytes"`
	Shared int64     `json:"shared_bytes,omitempty"`
	Capped bool      `json:"size_capped,omitempty"`
}

// SummaryTTL is how long a saved summary stands in for a fresh look.
const SummaryTTL = 24 * time.Hour

// Summarize counts the repository's agent worktrees and the space they
// take.
func (r *Repo) Summarize(now time.Time) Summary {
	s := Summary{Time: now.UTC()}
	for _, w := range r.Worktrees {
		if !w.Agent {
			continue
		}
		s.Agent++
		s.Bytes += w.Bytes
		s.Shared += w.Shared
		s.Capped = s.Capped || w.Capped
		if w.Safe {
			s.Safe++
		}
	}
	return s
}

func summaryPath(root string) string { return filepath.Join(root, ".tacet", "clean.json") }

// SaveSummary writes the summary when root has a .tacet folder; it
// never makes one.
func SaveSummary(root string, s Summary) error {
	if !isDir(filepath.Join(root, ".tacet")) {
		return nil
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(summaryPath(root), append(b, '\n'), 0o600)
}

// LoadSummary reads a summary younger than SummaryTTL.
func LoadSummary(root string, now time.Time) (Summary, bool) {
	var s Summary
	b, err := os.ReadFile(summaryPath(root)) // #nosec G304 -- tacet's own state file
	if err != nil || json.Unmarshal(b, &s) != nil || now.Sub(s.Time) > SummaryTTL || s.Time.After(now.Add(time.Minute)) {
		return Summary{}, false
	}
	return s, true
}

// Count lists the repository's worktrees without checking them: the
// main worktree and how many agent worktrees it has, stale entries
// included. One git call, fast enough for every scan.
func Count(dir string) (root string, agent int, err error) {
	r, err := List(dir, Options{})
	if err != nil {
		return "", 0, err
	}
	for _, w := range r.Worktrees {
		if w.Agent {
			agent++
		}
	}
	for _, w := range r.Stale {
		if w.Agent {
			agent++
		}
	}
	return r.Root, agent, nil
}
