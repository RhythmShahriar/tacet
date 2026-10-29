// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/RhythmShahriar/tacet/audit"
	"github.com/RhythmShahriar/tacet/claude"
	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/docs/lint"
	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/measure"
	"github.com/RhythmShahriar/tacet/skills"
)

// recentSessions is how many past transcripts a scan reads.
const recentSessions = 30

// scan is everything one look at a project finds.
type scan struct {
	Root    string           `json:"root"`
	Startup *measure.Startup `json:"startup"`
	Usage   *measure.Usage   `json:"usage"`
	Docs    []core.Finding   `json:"docs"`
	Audit   []core.Finding   `json:"audit"`
	set     *lint.Set
}

func (a *app) root() string { return config.Root(a.dir) }

func doScan(root string, since time.Time) (*scan, error) {
	st, set, err := measure.MeasureStartup(root, measure.Options{UserHome: claude.Home()})
	if err != nil {
		return nil, err
	}
	u := measure.MeasureUsage(root, since, recentSessions)
	s := &scan{Root: root, Startup: st, Usage: u, set: set}
	s.Docs = lint.Check(set)
	s.Audit = audit.Run(audit.Input{Root: root, Startup: st, Usage: u})
	s.Audit = append(s.Audit, skills.Suggest(root, set, s.Docs, config.Load(root).On("skills"))...)
	core.Rank(s.Audit)
	return s, nil
}

func (s *scan) all() []core.Finding {
	out := append(append([]core.Finding{}, s.Docs...), s.Audit...)
	core.Rank(out)
	return out
}

// fixes splits the scan's fixes into safe ones and suggestions, leaving
// out suggestions the person dismissed.
func (s *scan) fixes() (safe, suggested []core.Finding) {
	dismissed := loadDismissed(s.Root)
	for _, f := range s.all() {
		if f.Fix == nil {
			continue
		}
		if f.Fix.Safe {
			safe = append(safe, f)
		} else if !dismissed[f.Fix.ID] {
			suggested = append(suggested, f)
		}
	}
	return safe, suggested
}

// Baseline is what the project cost when tacet init first ran.
type Baseline struct {
	Time       time.Time `json:"time"`
	Startup    int       `json:"startup_tokens"`
	PerRequest int       `json:"per_request_tokens"`
	HitRate    float64   `json:"cache_hit_rate"`
	Sessions   int       `json:"sessions"`
}

func baselinePath(root string) string { return filepath.Join(config.Dir(root), "baseline.json") }

func saveBaseline(root string, s *scan) error {
	if _, err := os.Stat(baselinePath(root)); err == nil {
		return nil // the first init is the baseline; later runs keep it
	}
	b := Baseline{Time: time.Now().UTC(), Startup: s.Startup.Total, PerRequest: s.Usage.PerRequest(), HitRate: s.Usage.HitRate, Sessions: s.Usage.Sessions}
	return writeFileJSON(baselinePath(root), b)
}

func loadBaseline(root string) *Baseline {
	var b Baseline
	if readFileJSON(baselinePath(root), &b) != nil {
		return nil
	}
	return &b
}

func dismissedPath(root string) string { return filepath.Join(config.Dir(root), "dismissed.json") }

func loadDismissed(root string) map[string]bool {
	m := map[string]bool{}
	_ = readFileJSON(dismissedPath(root), &m)
	return m
}

func writeFileJSON(p string, v any) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

func readFileJSON(p string, v any) error {
	b, err := os.ReadFile(p) // #nosec G304 -- tacet's own state files
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
