// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package gen

import (
	"strings"

	"github.com/RhythmShahriar/tacet/docs/lint"
)

// verbCmd is a language command and the job it does (build, test, lint).
type verbCmd struct {
	verb string
	cmd  Command
}

// langCommands adds the Go, Rust and Python commands for every job the
// Makefile, justfile or package.json didn't already cover, so a Makefile
// with only a build target still lists cargo test.
func langCommands(root string, have []Command) []Command {
	covered := map[string]bool{}
	for _, c := range have {
		f := strings.Fields(c.Run)
		covered[f[len(f)-1]] = true
	}
	var out []Command
	for _, v := range langVerbs(lint.Languages(root)) {
		if covered[v.verb] {
			continue
		}
		covered[v.verb] = true
		out = append(out, v.cmd)
	}
	return out
}

func langVerbs(lg *lint.Lang) []verbCmd {
	var out []verbCmd
	add := func(verb, run, desc string) { out = append(out, verbCmd{verb, Command{Run: run, Desc: desc}}) }
	if lg.Go {
		add("build", "go build ./...", "")
		add("test", "go test ./...", "")
		add("lint", "go vet ./...", "")
	}
	if lg.Cargo != nil {
		for _, n := range preferred {
			if lg.Cargo[n] {
				add(n, "cargo "+n, "alias in .cargo/config.toml")
			}
		}
		ws := ""
		if lg.CargoWorkspace {
			ws = " --workspace"
		}
		add("build", "cargo build"+ws, "")
		add("test", "cargo test"+ws, "")
		add("lint", "cargo clippy"+ws+" --all-targets", "")
		add("fmt", "cargo fmt --check", "")
	}
	if p := lg.Python; p != nil {
		run := map[string]string{"uv": "uv run ", "poetry": "poetry run ", "pdm": "pdm run "}[p.Runner]
		switch p.Runner {
		case "uv":
			add("install", "uv sync", "install the dependencies")
		case "poetry":
			add("install", "poetry install", "install the dependencies")
		case "pdm":
			add("install", "pdm install", "install the dependencies")
		}
		for _, n := range preferred {
			if p.Poe[n] {
				add(n, run+"poe "+n, "")
			} else if p.PDM[n] && p.Runner == "pdm" {
				add(n, "pdm run "+n, "")
			}
		}
		if p.Tests {
			add("test", run+"pytest", "")
		}
		if p.Ruff {
			add("lint", run+"ruff check .", "")
			add("fmt", run+"ruff format .", "")
		} else if p.Deps["black"] {
			add("fmt", run+"black .", "")
		}
		switch {
		case p.Deps["mypy"]:
			add("typecheck", run+"mypy .", "")
		case p.Deps["pyright"]:
			add("typecheck", run+"pyright", "")
		}
	}
	if lg.Tox != nil {
		add("tox", "tox", "run every tox environment")
	}
	return out
}
