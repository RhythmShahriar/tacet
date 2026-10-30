// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package audit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/RhythmShahriar/tacet/core"
)

// The built-in Explore subagent runs on the main conversation's model,
// often Opus. A project agent with the same name overrides it and keeps
// its own model field (code.claude.com/docs/en/sub-agents), so one file
// moves search work to Haiku and leaves general-purpose and Plan alone.
// CLAUDE_CODE_SUBAGENT_MODEL can't do that: it doesn't change Explore
// unless forced; forcing moves every subagent.

// ExploreFixID is the suggestion and the feature's fix id.
const ExploreFixID = "subagents-explore-haiku"

// ExploreAgent is the agent file tacet writes. The YAML comment marks it
// as tacet's without costing the agent a token.
const ExploreAgent = `---
# written by tacet: tacet off subagents or tacet undo removes this file
name: Explore
description: Fast read-only search agent for finding files, code and answers across a codebase. Use it for broad searches when you only need the conclusion and not the file contents. Say how thorough to be, "quick", "medium" or "very thorough".
model: haiku
tools: Read, Grep, Glob, Bash, mcp__plugin_tacet_tacet__symbols, mcp__plugin_tacet_tacet__callers, mcp__plugin_tacet_tacet__outline, mcp__plugin_tacet_tacet__map
---
You are a read-only search agent. Find what the caller asked for in this codebase and report it.

- Search before you read: use the code index tools when they exist, then Grep and Glob, then Read only the lines that matter.
- Try more than one name or pattern before you conclude something is missing.
- Never create, edit or delete files.
- Answer with exact file paths and line numbers. Quote only the code that proves the point.
- Say plainly what you didn't find or aren't sure about, so the caller can check it.
`

// ExploreAgentPath is where the project's Explore agent lives.
func ExploreAgentPath(root string) string {
	return filepath.Join(root, ".claude", "agents", "explore.md")
}

// IsTacetExplore reports whether b is an agent file tacet wrote.
func IsTacetExplore(b []byte) bool {
	return bytes.HasPrefix(b, []byte("---\n# written by tacet:"))
}

// ProjectExplore returns the project agent file named Explore, if any.
func ProjectExplore(root string) string {
	files, _ := filepath.Glob(filepath.Join(root, ".claude", "agents", "*.md"))
	sort.Strings(files)
	for _, p := range files {
		b, err := os.ReadFile(p) // #nosec G304 -- the project's own agent files
		if err != nil {
			continue
		}
		if agentName(string(b)) == "Explore" {
			return p
		}
	}
	return ""
}

func agentName(s string) string {
	if !strings.HasPrefix(s, "---\n") {
		return ""
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return ""
	}
	for _, l := range strings.Split(s[4:4+end], "\n") {
		if v, ok := strings.CutPrefix(l, "name:"); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// SubagentModels are the models the Explore agent can be set to, cheapest
// first. "main" means no agent file: Explore runs on the main model.
var SubagentModels = []string{"haiku", "sonnet", "main"}

// ExploreAgentFor is the agent file for model.
func ExploreAgentFor(model string) string {
	return strings.Replace(ExploreAgent, "\nmodel: haiku\n", "\nmodel: "+model+"\n", 1)
}

// ExploreModelOf reads the model from an agent file tacet wrote, or "".
func ExploreModelOf(b []byte) string {
	if !IsTacetExplore(b) {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(l, "model:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ExploreOnFix writes the Haiku Explore agent. It refuses to replace an
// Explore agent the person wrote.
func ExploreOnFix(root string) *core.Fix { return ExploreModelFix(root, "haiku") }

// ExploreModelFix writes tacet's Explore agent on model, replacing an
// earlier one tacet wrote. It refuses to replace one the person wrote.
func ExploreModelFix(root, model string) *core.Fix {
	title := "Run the Explore subagent on " + displayModel(model)
	if model == "haiku" {
		title += " (cheaper, may miss on hard searches)"
	}
	return &core.Fix{
		ID: ExploreFixID, Title: title, Safe: false,
		Edits: []core.Edit{{Path: ExploreAgentPath(root), Change: func(cur []byte) ([]byte, error) {
			if cur != nil && !IsTacetExplore(cur) {
				return nil, fmt.Errorf("an agent file of your own is already there")
			}
			return []byte(ExploreAgentFor(model)), nil
		}}},
	}
}

func displayModel(m string) string {
	if m == "" {
		return m
	}
	return strings.ToUpper(m[:1]) + m[1:]
}

// ExploreOffFix removes the agent file tacet wrote, so Explore runs on
// the main model again. A file the person changed stays.
func ExploreOffFix(root string) *core.Fix {
	return &core.Fix{
		ID: "subagents-explore-main", Title: "Run the Explore subagent on the main model again", Safe: false,
		Edits: []core.Edit{{Path: ExploreAgentPath(root), Change: func(cur []byte) ([]byte, error) {
			if cur != nil && !IsTacetExplore(cur) {
				return nil, fmt.Errorf("not written by tacet; left as it is")
			}
			return nil, nil
		}}},
	}
}

// exploreModel flags heavy subagent use where the Explore agents ran on
// a model above Haiku and the project doesn't define its own Explore.
func exploreModel(in Input) []core.Finding {
	u := in.Usage
	share, ok := subagentHeavy(u)
	if !ok || u.ExploreTokens == 0 || ProjectExplore(in.Root) != "" {
		return nil
	}
	pricey, top, topN := 0, "", 0
	for m, n := range u.ExploreModels {
		if m == "" || strings.Contains(m, "haiku") || strings.HasPrefix(m, "<") {
			continue
		}
		pricey += n
		if n > topN || (n == topN && m < top) {
			top, topN = m, n
		}
	}
	if pricey*2 < u.ExploreTokens {
		return nil
	}
	return []core.Finding{{
		Rule: "subagents/model", Severity: core.Medium,
		Message: fmt.Sprintf("Explore subagents ran on %s for %s tokens; subagents used %.0f%% of all tokens", top, short(pricey), share),
		Hint:    "an Explore agent on Haiku costs a fraction per token but may miss on hard searches; general-purpose and Plan stay on the main model",
		Fix:     ExploreOnFix(in.Root),
	}}
}
