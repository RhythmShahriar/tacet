// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/docs/lint"
)

// Used is the set of skill names the project already has, so drafts
// never collide with one.
func Used(root string) map[string]bool {
	used := map[string]bool{}
	m, _ := filepath.Glob(filepath.Join(root, ".claude", "skills", "*", "SKILL.md"))
	for _, p := range m {
		used[filepath.Base(filepath.Dir(p))] = true
	}
	return used
}

// Findings turns the repeated instructions in root's transcripts into
// suggestions, each with a draft skill as an unsafe fix.
func Findings(root string, used map[string]bool) []core.Finding {
	return findings(root, Mine(Prompts(root)), used)
}

func findings(root string, cs []Cluster, used map[string]bool) []core.Finding {
	var out []core.Finding
	for _, c := range FromClusters(root, cs) {
		if c.Exists {
			continue
		}
		c.Name = uniqueName(c.Name, used)
		c.Path = filepath.Join(root, ".claude", "skills", c.Name, "SKILL.md")
		out = append(out, core.Finding{
			Rule: "skills/repeated", Severity: core.Low, Path: rel(root, c.Path),
			Message: fmt.Sprintf("you asked %d times in %d sessions: %q", c.Count, c.Sessions, excerpt(c.Example)),
			Hint:    "save it as a skill: Claude picks it up when you ask, or type /" + c.Name,
			Fix: &core.Fix{
				ID: "skill-" + c.Name, Title: fmt.Sprintf("Draft skill %s from %d repeated requests", c.Name, c.Count), Safe: false,
				Edits: []core.Edit{{Path: c.Path, Old: nil, New: []byte(Draft(c))}},
			},
		})
	}
	return out
}

// Suggest is everything this package adds to a scan: drafted skills
// from repeated requests plus fixes on the move/procedure findings in
// docs. It is a no-op when the skills feature is off.
func Suggest(root string, set *lint.Set, docs []core.Finding, on bool) []core.Finding {
	if !on {
		return nil
	}
	used := Used(root)
	if set != nil {
		AttachProcedureFixes(set, docs, used)
	}
	if _, err := os.Stat(root); err != nil {
		return nil
	}
	return Findings(root, used)
}

func excerpt(s string) string {
	s = strings.Join(strings.Fields(Mask(s)), " ")
	if r := []rune(s); len(r) > 100 {
		return string(r[:100]) + "..."
	}
	return s
}
