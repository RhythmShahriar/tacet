// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/RhythmShahriar/tacet/audit"
	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/fix"
	"github.com/RhythmShahriar/tacet/internal/config"
)

// featureFiles makes the file changes a feature needs when it turns on
// or off, through fix.Apply so tacet undo reverts them. Features that
// work from hooks need none.
func featureFiles(a *app, root, feature string, on bool) error {
	if feature != "subagents" {
		return nil
	}
	path := audit.ExploreAgentPath(root)
	cur, err := os.ReadFile(path) // #nosec G304 -- tacet's own agent file path
	exists := err == nil
	if on {
		model := config.Load(root).ExploreModel()
		if own := audit.ProjectExplore(root); own != "" && (own != path || !audit.IsTacetExplore(cur)) {
			return fmt.Errorf("%s already defines an Explore agent; tacet leaves it as it is", own)
		}
		if exists && audit.ExploreModelOf(cur) == model {
			return nil
		}
		if _, err := fix.Apply(root, []*core.Fix{audit.ExploreModelFix(root, model)}); err != nil {
			return err
		}
		note := ""
		if model == "haiku" {
			note = ", cheaper but it may miss on hard searches"
		}
		fmt.Fprintf(a.out, "wrote %s: Explore runs on %s%s; your main model still plans and writes the code (tacet undo reverts it)\n", path, model, note)
		return nil
	}
	if !exists || !audit.IsTacetExplore(cur) {
		return nil
	}
	if _, err := fix.Apply(root, []*core.Fix{audit.ExploreOffFix(root)}); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "removed %s: Explore runs on the main model again\n", path)
	return nil
}

// runSubagents shows or sets the model of the Explore subagent: haiku,
// sonnet or main (the main conversation's model, no agent file).
func runSubagents(a *app, args []string) error {
	root := a.root()
	c := config.Load(root)
	if len(args) == 0 {
		cur, _ := os.ReadFile(audit.ExploreAgentPath(root)) // #nosec G304 -- tacet's own agent file
		model := audit.ExploreModelOf(cur)
		switch {
		case model != "":
			fmt.Fprintf(a.out, "search subagents (Explore) run on %s; your main model plans and writes the code\n", model)
		case audit.ProjectExplore(root) != "":
			fmt.Fprintf(a.out, "%s defines your own Explore agent; tacet leaves it alone\n", audit.ProjectExplore(root))
		default:
			fmt.Fprintln(a.out, "search subagents (Explore) run on your main model")
		}
		fmt.Fprintln(a.out, "change with: tacet subagents haiku|sonnet|main")
		return nil
	}
	model := strings.ToLower(args[0])
	if !slices.Contains(audit.SubagentModels, model) {
		return fmt.Errorf("%w: tacet subagents haiku|sonnet|main", errUsage)
	}
	on := model != "main"
	if on {
		c.SubagentModel = model
	}
	if err := c.Set("subagents", on); err != nil {
		return err
	}
	if err := c.Save(root); err != nil {
		return err
	}
	return featureFiles(a, root, "subagents", on)
}
