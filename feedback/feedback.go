// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package feedback runs the project's own fast checkers on a file the
// agent just changed and returns only the failures, so the agent sees a
// broken import or type error right after the edit instead of at the end.
// It runs tools already on PATH or in the project's venv or node_modules
// and never installs anything.
package feedback

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Limits on what a check may cost and return.
const (
	MaxLines  = 40               // lines of failures handed to the agent
	maxOutput = 1 << 20          // bytes of tool output read
	waitDelay = 2 * time.Second  // grace after a kill before pipes close
	maxTotal  = 45 * time.Second // every check for one edit together
)

// CheckTimeout caps one checker run. A slow checker says nothing.
var CheckTimeout = 20 * time.Second

// Check is one checker command for one changed file.
type Check struct {
	Name   string   // shown to the agent, e.g. "ruff check"
	Dir    string   // working directory
	Args   []string // Args[0] is the resolved binary
	Filter func(lines []string) []string
}

// Result is what a set of checks found.
type Result struct {
	Text     string   // failures for the agent, empty when clean
	TimedOut []string // checks that hit the time cap
}

// skipDirs hold code the project doesn't own or generated output.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "target": true, ".venv": true, "venv": true,
	".git": true, "dist": true, "build": true, "testdata": true, "__pycache__": true,
	".tacet": true, ".next": true, ".tox": true,
}

// Source reports whether path is a source file under root that a
// checker covers.
func Source(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	for _, part := range strings.Split(filepath.Dir(rel), string(filepath.Separator)) {
		if skipDirs[part] {
			return false
		}
	}
	switch lang(path) {
	case "python", "rust", "typescript", "go":
		return true
	}
	return false
}

func lang(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py", ".pyi":
		return "python"
	case ".rs":
		return "rust"
	case ".ts", ".tsx", ".mts", ".cts":
		if strings.HasSuffix(path, ".d.ts") {
			return ""
		}
		return "typescript"
	case ".go":
		return "go"
	}
	return ""
}

// Detect returns the checks that cover file, using only the tools the
// project already has.
func Detect(root, file string) []Check {
	if !Source(root, file) {
		return nil
	}
	dir := filepath.Dir(file)
	switch lang(file) {
	case "python":
		return python(root, dir, file)
	case "rust":
		return rust(root, dir)
	case "typescript":
		return typescript(root, dir, file)
	case "go":
		return golang(root, dir)
	}
	return nil
}

func python(root, dir, file string) []Check {
	proj := nearest(root, dir, "pyproject.toml", "ruff.toml", ".ruff.toml", "pyrightconfig.json", "setup.cfg")
	if proj == "" {
		proj = root
	}
	var out []Check
	if ruff := Tool(root, dir, "ruff"); ruff != "" {
		// --no-fix: a config with fix = true must not rewrite the file,
		// or an import the agent is about to use (F401) disappears.
		out = append(out, Check{
			Name: "ruff check", Dir: proj,
			Args:   []string{ruff, "check", "--no-fix", "--force-exclude", "--output-format", "concise", file},
			Filter: dropMatching(`^Found \d+ errors?`, `fixable with the`, `^No fixes available`, `^All checks passed`),
		})
	}
	if pyright := Tool(root, dir, "pyright"); pyright != "" {
		out = append(out, Check{
			Name: "pyright", Dir: proj, Args: []string{pyright, file},
			Filter: keepMatching(` - error: `),
		})
	}
	return out
}

func rust(root, dir string) []Check {
	crate := nearest(root, dir, "Cargo.toml")
	cargo := Tool(root, dir, "cargo")
	if crate == "" || cargo == "" {
		return nil
	}
	return []Check{{
		Name: "cargo check", Dir: crate,
		Args: []string{cargo, "check", "--quiet", "--message-format", "short", "--manifest-path", filepath.Join(crate, "Cargo.toml")},
		Filter: func(lines []string) []string {
			var out []string
			for _, l := range lines {
				t := strings.TrimSpace(l)
				switch {
				case strings.Contains(t, ": error"):
					out = append(out, t)
				case strings.HasPrefix(t, "error") && !strings.Contains(t, "could not compile") && !strings.Contains(t, "aborting due"):
					out = append(out, t)
				}
			}
			return out
		},
	}}
}

func typescript(root, dir, file string) []Check {
	proj := nearest(root, dir, "tsconfig.json")
	tsc := Tool(root, dir, "tsc")
	if proj == "" || tsc == "" {
		return nil
	}
	rel, err := filepath.Rel(proj, file)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	return []Check{{
		Name: "tsc", Dir: proj,
		Args: []string{tsc, "--noEmit", "--pretty", "false", "-p", filepath.Join(proj, "tsconfig.json")},
		// tsc checks the whole project; keep only the changed file's
		// errors and their indented continuation lines.
		Filter: func(lines []string) []string {
			var out []string
			keep := false
			for _, l := range lines {
				switch {
				case strings.HasPrefix(filepath.ToSlash(l), rel+"("):
					keep = true
					out = append(out, l)
				case keep && (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")):
					out = append(out, l)
				default:
					keep = false
				}
			}
			return out
		},
	}}
}

func golang(root, dir string) []Check {
	goTool := Tool(root, dir, "go")
	if goTool == "" || nearest(root, dir, "go.mod", "go.work") == "" {
		return nil
	}
	return []Check{{
		Name: "go vet", Dir: dir, Args: []string{goTool, "vet", "."},
		Filter: dropMatching(`^# `),
	}}
}

// nearest walks from dir up to root and returns the first folder that
// holds one of names.
func nearest(root, dir string, names ...string) string {
	for d := dir; ; d = filepath.Dir(d) {
		for _, n := range names {
			if _, err := os.Stat(filepath.Join(d, n)); err == nil {
				return d
			}
		}
		if d == root || filepath.Dir(d) == d || !within(root, d) {
			return ""
		}
	}
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Tool finds a binary in the project's venv or node_modules/.bin between
// dir and root, then on PATH. It returns "" when the tool isn't there.
func Tool(root, dir, name string) string {
	local := [][]string{{".venv", "bin"}, {"venv", "bin"}, {".venv", "Scripts"}, {"venv", "Scripts"}, {"node_modules", ".bin"}}
	exts := []string{""}
	if runtime.GOOS == "windows" {
		exts = []string{".exe", ".cmd", ".bat"}
	}
	if within(root, dir) {
		for d := dir; ; d = filepath.Dir(d) {
			for _, sub := range local {
				for _, ext := range exts {
					p := filepath.Join(append(append([]string{d}, sub...), name+ext)...)
					if fi, err := os.Stat(p); err == nil && !fi.IsDir() && (runtime.GOOS == "windows" || fi.Mode()&0o111 != 0) {
						return p
					}
				}
			}
			if d == root || filepath.Dir(d) == d {
				break
			}
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// Run runs checks one after another, each capped at CheckTimeout, and
// returns the failures in at most MaxLines lines. A checker that exits
// 0, can't start or runs out of time adds nothing.
func Run(ctx context.Context, file string, checks []Check) Result {
	var res Result
	ctx, cancel := context.WithTimeout(ctx, maxTotal)
	defer cancel()
	var blocks []string
	seen := map[string]bool{}
	for _, c := range checks {
		lines, timedOut := run(ctx, c, CheckTimeout)
		if timedOut {
			res.TimedOut = append(res.TimedOut, c.Name)
			continue
		}
		var fresh []string
		for _, l := range lines {
			if !seen[l] {
				seen[l] = true
				fresh = append(fresh, l)
			}
		}
		if len(fresh) > 0 {
			blocks = append(blocks, c.Name+":\n"+strings.Join(fresh, "\n"))
		}
	}
	if len(blocks) == 0 {
		return res
	}
	head := fmt.Sprintf("tacet feedback: checks failed after editing %s. Fix these before moving on:", filepath.Base(file))
	res.Text = Cap(head + "\n" + strings.Join(blocks, "\n"))
	return res
}

// run executes one check and returns its failure lines, cleaned. Nil
// means it passed or couldn't run.
func run(ctx context.Context, c Check, limit time.Duration) (lines []string, timedOut bool) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Args[0], c.Args[1:]...) // #nosec G204 -- the project's own checker, resolved from PATH or its venv
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "CLICOLOR=0", "FORCE_COLOR=0", "CARGO_TERM_COLOR=never", "PY_COLORS=0", "TERM=dumb")
	cmd.WaitDelay = waitDelay
	killGroup(cmd)
	var buf capped
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, true
	}
	var exit *exec.ExitError
	if err == nil || !errors.As(err, &exit) {
		return nil, false
	}
	lines = Clean(buf.String())
	if c.Filter != nil {
		if kept := c.Filter(lines); len(kept) > 0 {
			return kept, false
		}
		// The tool failed but nothing matched: show its tail so the
		// agent still sees why (a broken config, a missing module).
		if len(lines) > 5 {
			lines = lines[len(lines)-5:]
		}
	}
	return lines, false
}

type capped struct{ bytes.Buffer }

func (c *capped) Write(p []byte) (int, error) {
	if room := maxOutput - c.Len(); room > 0 {
		if len(p) > room {
			c.Buffer.Write(p[:room])
		} else {
			c.Buffer.Write(p)
		}
	}
	return len(p), nil
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// Clean strips ANSI escapes and carriage returns and drops blank lines.
func Clean(s string) []string {
	s = ansiRe.ReplaceAllString(s, "")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if i := strings.LastIndexByte(l, '\r'); i >= 0 {
			l = l[i+1:] // progress lines redraw after \r
		}
		l = strings.TrimRight(l, " \t")
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// Cap trims text to MaxLines, keeping the start and the end, where test
// runners print their summary.
func Cap(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) <= MaxLines {
		return strings.Join(lines, "\n")
	}
	tail := 10
	head := MaxLines - tail - 1
	skipped := len(lines) - head - tail
	out := append([]string{}, lines[:head]...)
	out = append(out, fmt.Sprintf("... %d more lines ...", skipped))
	out = append(out, lines[len(lines)-tail:]...)
	return strings.Join(out, "\n")
}

func dropMatching(patterns ...string) func([]string) []string {
	res := compile(patterns)
	return func(lines []string) []string {
		var out []string
	next:
		for _, l := range lines {
			for _, re := range res {
				if re.MatchString(l) {
					continue next
				}
			}
			out = append(out, l)
		}
		return out
	}
}

func keepMatching(patterns ...string) func([]string) []string {
	res := compile(patterns)
	return func(lines []string) []string {
		var out []string
		for _, l := range lines {
			for _, re := range res {
				if re.MatchString(l) {
					out = append(out, strings.TrimSpace(l))
					break
				}
			}
		}
		return out
	}
}

func compile(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		out[i] = regexp.MustCompile(p)
	}
	return out
}
