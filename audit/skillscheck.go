// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package audit

import (
	"fmt"
	"os"
	"sort"
	"unicode/utf8"

	"github.com/RhythmShahriar/tacet/claude"
	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/skills"
	"github.com/RhythmShahriar/tacet/tokens"
)

// Skill description limits: Claude Code cuts a listing at DescriptionCap
// characters; past nearCap one more paragraph loses the end.
const (
	DescriptionCap = 1536
	nearCap        = 1300
	// Overlap is the description 3-gram Jaccard at which two skills
	// compete for the same requests.
	Overlap = 0.5
)

// SkillChecks is every skills rule: never triggered, long descriptions
// and skills that overlap.
func SkillChecks(in Input) []core.Finding {
	out := unusedSkills(in)
	out = append(out, longSkills(in.Root)...)
	out = append(out, overlapSkills(in)...)
	return out
}

// shortenFix keeps the description's first sentence and moves the rest
// into the body under "## When to use".
func shortenFix(sk claude.Skill) *core.Fix {
	b, err := os.ReadFile(sk.Path) // #nosec G304 -- a skill file Claude Code lists
	if err != nil {
		return nil
	}
	if _, ok := skills.Shorten(b); !ok {
		return nil
	}
	first, _ := skills.FirstSentence(sk.Description, skills.MaxDescription)
	return &core.Fix{
		ID: "skill-shorten-" + sk.Name, Title: "Shorten skill " + sk.Name + "'s description to its first sentence", Safe: false,
		Tokens: listed(sk) - tokens.Estimate(sk.Name+": "+first),
		Edits: []core.Edit{{Path: sk.Path, Change: func(cur []byte) ([]byte, error) {
			next, _ := skills.Shorten(cur)
			return next, nil
		}}},
	}
}

// listed is what a skill's entry costs in the listing, as measure counts it.
func listed(sk claude.Skill) int {
	d := sk.Description
	if utf8.RuneCountInString(d) > DescriptionCap {
		d = string([]rune(d)[:DescriptionCap])
	}
	return tokens.Estimate(sk.Name + ": " + d)
}

// overlapSkills flags pairs of listed skills whose descriptions say
// nearly the same thing or open with the same trigger phrase. The fix
// hides the one used less, which stays callable by its slash command.
func overlapSkills(in Input) []core.Finding {
	var sks []claude.Skill
	for _, sk := range claude.Skills(in.Root) {
		if !sk.ModelHidden && sk.Plugin == "" {
			sks = append(sks, sk)
		}
	}
	sort.SliceStable(sks, func(i, j int) bool { return sks[i].Name < sks[j].Name })
	uses := func(sk claude.Skill) int {
		if in.Usage == nil {
			return 0
		}
		return in.Usage.SkillCalls[sk.Name]
	}
	hidden := map[string]bool{}
	var out []core.Finding
	for i := range sks {
		for j := i + 1; j < len(sks); j++ {
			a, b := sks[i], sks[j]
			if a.Name == b.Name || hidden[a.Name] || hidden[b.Name] {
				continue
			}
			why := ""
			if sim := skills.Similarity(a.Description, b.Description); sim >= Overlap {
				why = fmt.Sprintf("their descriptions are %.0f%% alike", sim*100)
			} else if la := skills.Leading(a.Description); la != "" && la == skills.Leading(b.Description) {
				why = fmt.Sprintf("both trigger on %q", la)
			}
			if why == "" {
				continue
			}
			keep, weak := a, b
			if uses(b) > uses(a) || (uses(b) == uses(a) && listed(a) > listed(b)) {
				keep, weak = b, a
			}
			hidden[weak.Name] = true
			saves := listed(weak)
			out = append(out, core.Finding{
				Rule: "skills/overlap", Severity: core.Low, Path: weak.Path,
				Message: fmt.Sprintf("skills %q and %q overlap: %s", keep.Name, weak.Name, why),
				Hint:    fmt.Sprintf("hide %s (used %d times, %s %d) so requests go to one skill, or merge them", weak.Name, uses(weak), keep.Name, uses(keep)),
				Tokens:  saves,
				Fix: &core.Fix{
					ID: "skill-overlap-" + weak.Name, Title: "Hide skill " + weak.Name + " from the model (still /" + weak.Name + "); " + keep.Name + " covers it", Safe: false,
					Tokens: saves,
					Edits:  []core.Edit{{Path: weak.Path, Change: func(cur []byte) ([]byte, error) { return setFrontmatter(cur, "disable-model-invocation", "true"), nil }}},
				},
			})
		}
	}
	return out
}
