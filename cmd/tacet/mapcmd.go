// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/RhythmShahriar/tacet/codegraph"
	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/mcpserver"
)

// runMap updates the code index and prints the ranked map.
//
//	tacet map [--budget N] [--dir path] [--json]
//	tacet map --update    only refresh the index, quietly
func runMap(a *app, args []string) error {
	budget, dir, update := codegraph.DefaultBudget, "", false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--budget":
			if i+1 >= len(args) {
				return fmt.Errorf("%w: --budget needs a number of tokens", errUsage)
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				return fmt.Errorf("%w: --budget needs a number of tokens", errUsage)
			}
			budget = n
			i++
		case "--dir":
			if i+1 >= len(args) {
				return fmt.Errorf("%w: --dir needs a folder", errUsage)
			}
			dir = args[i+1]
			i++
		case "--update":
			update = true
		default:
			return fmt.Errorf("%w: tacet map [--budget N] [--dir path] [--json]", errUsage)
		}
	}
	root := a.root()
	_ = config.Exclude(root)
	ix, err := codegraph.Open(root)
	if err != nil {
		return err
	}
	defer func() { _ = ix.Close() }()
	ctx := context.Background()
	st, err := ix.Update(ctx)
	if err != nil {
		return err
	}
	if update {
		return nil
	}
	files, err := ix.Map(ctx, budget, dir)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(map[string]any{"index": st, "files": files})
	}
	if len(files) == 0 {
		fmt.Fprintln(a.out, "no code tacet can index here")
		return nil
	}
	fmt.Fprint(a.out, codegraph.Render(files))
	return nil
}

// runMCP serves the code tools over MCP on stdin and stdout. Claude Code
// starts it from the plugin in every project, so it offers no tools (and
// costs no context) where tacet init didn't run or map is off.
func runMCP(a *app, _ []string) error {
	dir := a.dir
	if d := os.Getenv("CLAUDE_PROJECT_DIR"); d != "" && dir == "." {
		dir = d
	}
	root := config.Root(dir)
	on := config.Initialized(root) && config.Load(root).On("map")
	return mcpserver.Run(context.Background(), mcpserver.Options{Root: root, Version: version, Map: on})
}

// refreshMap updates the index in the background at session start, so
// the first tool call finds it ready. A variable so tests can see it.
var refreshMap = func(root string) {
	self, err := os.Executable()
	if err != nil || strings.HasSuffix(self, ".test") {
		return // never re-run a test binary as tacet
	}
	cmd := exec.Command(self, "-C", root, "map", "--update") // #nosec G204 -- tacet itself
	detach(cmd)
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}
