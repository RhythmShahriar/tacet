// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"

	"github.com/RhythmShahriar/tacet/audit"
	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/fix"
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
		if own := audit.ProjectExplore(root); own != "" && (own != path || !audit.IsTacetExplore(cur)) {
			return fmt.Errorf("%s already defines an Explore agent; tacet leaves it as it is", own)
		}
		if exists {
			return nil
		}
		if _, err := fix.Apply(root, []*core.Fix{audit.ExploreOnFix(root)}); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "wrote %s: Explore runs on Haiku, cheaper but it may miss on hard searches (tacet undo reverts it)\n", path)
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
