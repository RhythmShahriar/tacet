// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Command tacet makes Claude Code lean on any project: run tacet init
// once and it measures, fixes what is safe and keeps working through
// Claude Code's hooks.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// version is stamped by the release build.
var version = "dev"

// Exit codes.
const (
	exitOK       = 0
	exitFindings = 1 // tacet check found something at or above --fail-on
	exitUsage    = 2
	exitError    = 3
)

type command struct {
	name    string
	summary string
	run     func(a *app, args []string) error
	hidden  bool // hooks and servers Claude Code calls, not people
}

var commands []command

func init() {
	commands = []command{
		{"init", "set tacet up on this project (run once)", runInit, false},
		{"status", "what tacet saved and what is waiting for you", runStatus, false},
		{"on", "turn a feature on: tacet on speak", runOn, false},
		{"off", "turn a feature off: tacet off convert", runOff, false},
		{"undo", "revert tacet's last change", runUndo, false},
		{"uninstall", "remove tacet from this project and restore your files", runUninstall, false},
		{"measure", "context loaded at startup and what past sessions billed", runMeasure, false},
		{"audit", "setup findings, each with a fix", runAudit, false},
		{"map", "ranked code map: tacet map [--budget N] [--dir path]", runMap, false},
		{"docs", "check CLAUDE.md, AGENTS.md and rules: tacet docs lint", runDocs, false},
		{"check", "docs and audit for CI; exits 1 on findings", runCheck, false},
		{"version", "print the version", runVersion, false},
		{"apply", "apply one suggestion by id (used by /tacet)", runApply, true},
		{"dismiss", "stop suggesting one fix here (used by /tacet)", runDismiss, true},
		{"hook", "run a hook event (called by Claude Code)", runHook, true},
		{"mcp", "serve the code tools over MCP (called by Claude Code)", runMCP, true},
		{"plugin", "write the plugin to a folder: tacet plugin write <dir>", runPlugin, true},
	}
}

type app struct {
	out, err io.Writer
	json     bool
	yes      bool
	dir      string
}

// errUsage marks a mistake in how tacet was called.
var errUsage = errors.New("usage")

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, errw io.Writer) int {
	a := &app{out: out, err: errw, dir: "."}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch x := args[i]; {
		case x == "--json":
			a.json = true
		case x == "-y" || x == "--yes":
			a.yes = true
		case x == "-C" && i+1 < len(args):
			a.dir = args[i+1]
			i++
		case x == "-h" || x == "--help" || x == "help":
			help(out)
			return exitOK
		case x == "--version":
			return code(runVersion(a, nil))
		default:
			rest = append(rest, x)
		}
	}
	name := "status"
	if len(rest) > 0 {
		name, rest = rest[0], rest[1:]
	}
	for _, c := range commands {
		if c.name == name {
			err := c.run(a, rest)
			if err != nil && !errors.Is(err, errFindings) {
				fmt.Fprintln(errw, "tacet:", err)
			}
			return code(err)
		}
	}
	fmt.Fprintf(errw, "tacet: no command %q%s\n", name, suggest(name))
	return exitUsage
}

var errFindings = errors.New("findings")

func code(err error) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errFindings):
		return exitFindings
	case errors.Is(err, errUsage):
		return exitUsage
	}
	return exitError
}

func suggest(name string) string {
	for _, c := range commands {
		if !c.hidden && (strings.HasPrefix(c.name, name) || strings.HasPrefix(name, c.name)) {
			return "; did you mean " + c.name + "?"
		}
	}
	return "; run tacet help"
}

func help(w io.Writer) {
	fmt.Fprintf(w, `tacet %s: make Claude Code lean on any project

Most people only ever run:

  tacet init        set it up once; it then works on its own inside Claude Code

Then, when you want to look or change something:

`, version)
	for _, c := range commands {
		if c.hidden || c.name == "init" {
			continue
		}
		fmt.Fprintf(w, "  tacet %-10s %s\n", c.name, c.summary)
	}
	fmt.Fprint(w, `
Flags:  --json  machine output    -y, --yes  no prompts    -C <dir>  run in <dir>

Inside Claude Code, type /tacet for the same status and suggestions.
`)
}

func runVersion(a *app, _ []string) error {
	fmt.Fprintln(a.out, "tacet", version)
	return nil
}
