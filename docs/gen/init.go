// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package gen writes instruction files: lean templates for the ones a
// project lacks, and copies of AGENTS.md for the other agents' formats.
package gen

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/docs/lint"
)

// MaxCommands caps the commands a template lists.
const MaxCommands = 8

// Init plans the templates root is missing: AGENTS.md, CLAUDE.md and
// ARCHITECTURE.md. Each edit requires the file still be absent, so an
// existing file is never overwritten.
func Init(root string) []core.Edit {
	var out []core.Edit
	add := func(name, body string) {
		p := filepath.Join(root, name)
		if _, err := os.Lstat(p); err == nil {
			return
		}
		out = append(out, core.Edit{Path: p, Old: nil, New: []byte(body)})
	}
	add("AGENTS.md", Agents(root))
	add("CLAUDE.md", Claude())
	add("ARCHITECTURE.md", Architecture(root))
	return out
}

// Agents is the AGENTS.md template: the commands the project defines,
// a place for conventions and the rules the agent must never break.
func Agents(root string) string {
	var b strings.Builder
	b.WriteString("<!-- tacet · lean AGENTS.md: commands, conventions and the never rules. Keep it under 60 lines; every line loads in every session. -->\n")
	b.WriteString("# AGENTS.md\n\n## Commands\n")
	cmds := Commands(root)
	if len(cmds) == 0 {
		b.WriteString("<!-- The commands to build, test and lint, one per line: - `make test`: run every test -->\n")
	}
	for _, c := range cmds {
		if c.Desc != "" {
			fmt.Fprintf(&b, "- `%s`: %s\n", c.Run, c.Desc)
		} else {
			fmt.Fprintf(&b, "- `%s`\n", c.Run)
		}
	}
	b.WriteString(`
## Conventions
<!-- Only what the code and tooling don't already show, one line each. For example: errors wrap with %w; tests are table-driven. -->

## Never
- Never commit secrets, keys or .env files.
- Never push, force-push or rewrite history unless asked.
- Never skip, delete or weaken a failing test to get a green run.
- Never edit generated or vendored files by hand.
`)
	return b.String()
}

// Claude is the CLAUDE.md template: an import of AGENTS.md and a short
// section for what only Claude Code needs.
func Claude() string {
	return `<!-- tacet · AGENTS.md is the single source; this file imports it and adds only what is specific to Claude Code. -->
@AGENTS.md

## Claude Code
<!-- Claude-only lines, for example which skills or subagents to prefer. Keep it short. -->
`
}

// Architecture is ARCHITECTURE.md in matklad's style: a codemap of the
// top-level folders with one line each, then invariants and boundaries.
// It names things instead of linking them, so it doesn't rot.
func Architecture(root string) string {
	var b strings.Builder
	b.WriteString("<!-- tacet · ARCHITECTURE.md in matklad's style: a bird's-eye view, a codemap, invariants and boundaries. Name things, don't link them. -->\n")
	b.WriteString("# Architecture\n\n<!-- One paragraph: the problem this project solves and the shape of the answer. -->\n\n## Codemap\n\n")
	dirs := Codemap(root)
	if len(dirs) == 0 {
		b.WriteString("<!-- One line per top-level folder: what lives there and what it owns. -->\n")
	}
	for _, d := range dirs {
		if d.Desc != "" {
			fmt.Fprintf(&b, "- `%s/`: %s\n", d.Name, d.Desc)
		} else {
			fmt.Fprintf(&b, "- `%s/`: <!-- what lives here, in one line -->\n", d.Name)
		}
	}
	b.WriteString(`
## Invariants
<!-- What is always true and the code relies on, often an absence: "core imports nothing from cmd". -->

## Boundaries
<!-- The layers and the APIs between them: what may depend on what. -->
`)
	return b.String()
}

// Command is one way to build, test or run the project.
type Command struct {
	Run  string
	Desc string
}

// preferred orders the targets worth listing.
var preferred = []string{"build", "dev", "test", "lint", "typecheck", "check", "fmt", "format", "run", "start"}

var makeDescRe = regexp.MustCompile(`(?m)^([A-Za-z0-9][A-Za-z0-9_.\-]*)\s*:[^=\n]*##\s*(.+)$`)

// Commands detects the project's commands from its Makefile, justfile,
// package.json, go.mod or Cargo.toml, at most MaxCommands of them.
func Commands(root string) []Command {
	targets := lint.Targets(root)
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(root, name)) // #nosec G304 -- project build files
		return string(b)
	}
	var out []Command
	if set, ok := targets["make"]; ok {
		mk := read("Makefile") + read("makefile") + read("GNUmakefile")
		descs := map[string]string{}
		var documented []string
		for _, m := range makeDescRe.FindAllStringSubmatch(mk, -1) {
			descs[m[1]] = strings.TrimSpace(m[2])
			documented = append(documented, m[1])
		}
		names := documented
		if len(names) == 0 {
			names = pick(set)
		}
		for _, n := range names {
			out = append(out, Command{Run: "make " + n, Desc: descs[n]})
		}
	}
	if set, ok := targets["just"]; ok {
		for _, n := range pick(set) {
			out = append(out, Command{Run: "just " + n})
		}
	}
	if pkg := read("package.json"); pkg != "" {
		var p struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal([]byte(pkg), &p) == nil {
			set := map[string]bool{}
			for k := range p.Scripts {
				set[k] = true
			}
			pm := packageManager(root)
			for _, n := range pick(set) {
				out = append(out, Command{Run: pm + " " + n})
			}
		}
	}
	if len(out) == 0 && read("go.mod") != "" {
		out = append(out, Command{Run: "go build ./..."}, Command{Run: "go test ./..."}, Command{Run: "go vet ./..."})
	}
	if len(out) == 0 && read("Cargo.toml") != "" {
		out = append(out, Command{Run: "cargo build"}, Command{Run: "cargo test"}, Command{Run: "cargo clippy"})
	}
	if len(out) > MaxCommands {
		out = out[:MaxCommands]
	}
	return out
}

func pick(set map[string]bool) []string {
	var out []string
	for _, n := range preferred {
		if set[n] {
			out = append(out, n)
		}
	}
	return out
}

func packageManager(root string) string {
	for _, l := range [][2]string{{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"bun.lockb", "bun run"}, {"bun.lock", "bun run"}} {
		if _, err := os.Stat(filepath.Join(root, l[0])); err == nil {
			return l[1]
		}
	}
	return "npm run"
}

// Dir is one codemap entry.
type Dir struct {
	Name string
	Desc string
}

var skipDirs = []string{"node_modules", "vendor", "bin", "dist", "build", "out", "target", "coverage", "tmp"}

// Codemap lists the top-level folders, described by a Go package comment
// or a package.json description when one exists.
func Codemap(root string) []Dir {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []Dir
	for _, e := range ents {
		n := e.Name()
		if !e.IsDir() || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_") || slices.Contains(skipDirs, n) {
			continue
		}
		out = append(out, Dir{Name: n, Desc: describe(filepath.Join(root, n))})
	}
	return out
}

func describe(dir string) string {
	if b, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil { // #nosec G304 -- a project file
		var p struct {
			Description string `json:"description"`
		}
		if json.Unmarshal(b, &p) == nil && p.Description != "" {
			return sentence(p.Description)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	slices.SortFunc(files, func(a, b string) int {
		// doc.go holds the package comment by convention.
		if filepath.Base(a) == "doc.go" {
			return -1
		}
		if filepath.Base(b) == "doc.go" {
			return 1
		}
		return strings.Compare(a, b)
	})
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		if d := packageDoc(f); d != "" {
			return d
		}
	}
	return ""
}

var pkgDocRe = regexp.MustCompile(`^// Package \S+ (.*)`)

// packageDoc is the first sentence of a Go file's package comment.
func packageDoc(path string) string {
	f, err := os.Open(path) // #nosec G304 -- a project source file
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	text := ""
	for sc.Scan() {
		l := sc.Text()
		if text == "" {
			if m := pkgDocRe.FindStringSubmatch(l); m != nil {
				text = m[1]
			}
			continue
		}
		if !strings.HasPrefix(l, "//") {
			break
		}
		text += " " + strings.TrimSpace(strings.TrimPrefix(l, "//"))
	}
	if text == "" {
		return ""
	}
	return sentence(text)
}

// sentence keeps the first sentence and capitalizes it.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i+1]
	}
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
