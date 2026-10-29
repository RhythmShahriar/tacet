// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package lint

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/RhythmShahriar/tacet/core"
)

// Lang is what a project's Cargo, Go, Python and tox files say it can
// run. A nil set means tacet can't tell, so those commands aren't checked.
type Lang struct {
	Cargo          map[string]bool // cargo aliases from .cargo/config.toml; nil without Cargo.toml
	CargoWorkspace bool
	Go             bool // go.mod or go.work at the root
	Python         *Python
	Tox            map[string]bool // tox environments; nil without a tox config
}

// Python is what pyproject.toml and the lock files say.
type Python struct {
	Runner  string          // uv, poetry, pdm or "" for a plain venv
	Scripts map[string]bool // [project.scripts] and [tool.poetry.scripts]
	Poe     map[string]bool // [tool.poe.tasks]; nil when absent or unreadable
	PDM     map[string]bool // [tool.pdm.scripts]
	Deps    map[string]bool // every declared dependency name
	Ruff    bool            // ruff configured or declared
	Tests   bool            // pytest declared or a tests folder exists
}

// Languages reads the language build files at root.
func Languages(root string) *Lang {
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(root, name)) // #nosec G304 -- project build files
		return string(b)
	}
	has := func(name string) bool { _, err := os.Stat(filepath.Join(root, name)); return err == nil }
	lg := &Lang{Go: has("go.mod") || has("go.work")}
	if cargo := read("Cargo.toml"); cargo != "" {
		lg.Cargo = map[string]bool{}
		_, tables := scanTOML(cargo)
		for _, t := range tables {
			if t == "workspace" {
				lg.CargoWorkspace = true
			}
		}
		for _, name := range []string{".cargo/config.toml", ".cargo/config"} {
			entries, tables := scanTOML(read(name))
			for k := range keysOf(entries, tables, "alias") {
				lg.Cargo[k] = true
			}
		}
	}
	py := read("pyproject.toml")
	if py != "" {
		lg.Python = readPython(root, py, has)
	}
	lg.Tox = toxEnvs(read("tox.ini"), read("setup.cfg"), read("tox.toml"), py)
	return lg
}

var depNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-]*`)

func readPython(root, text string, has func(string) bool) *Python {
	entries, tables := scanTOML(text)
	p := &Python{Scripts: map[string]bool{}, PDM: map[string]bool{}, Deps: map[string]bool{}}
	addDep := func(s string) {
		if n := depNameRe.FindString(strings.TrimSpace(s)); n != "" {
			n = strings.ToLower(n)
			p.Deps[n] = true
			p.Deps[strings.ReplaceAll(n, "_", "-")] = true
			p.Deps[strings.ReplaceAll(n, "-", "_")] = true
		}
	}
	poeInline := false
	for _, e := range entries {
		switch {
		case e.table == "project" && e.key == "dependencies",
			e.table == "project.optional-dependencies",
			e.table == "dependency-groups",
			e.table == "tool.uv" && e.key == "dev-dependencies",
			e.table == "tool.pdm.dev-dependencies":
			for _, s := range tomlStrings(e.value) {
				addDep(s)
			}
		case e.table == "tool.poetry.dependencies", e.table == "tool.poetry.dev-dependencies",
			strings.HasPrefix(e.table, "tool.poetry.group.") && strings.HasSuffix(e.table, ".dependencies"):
			addDep(e.key)
		case e.table == "tool.poe" && strings.HasPrefix(e.key, "tasks"):
			poeInline = true
		}
	}
	for _, t := range []string{"project.scripts", "project.gui-scripts", "tool.poetry.scripts"} {
		for k := range keysOf(entries, tables, t) {
			p.Scripts[k] = true
		}
	}
	for k := range keysOf(entries, tables, "tool.pdm.scripts") {
		if k != "_" {
			p.PDM[k] = true
		}
	}
	for _, t := range tables {
		if t == "tool.poe.tasks" || strings.HasPrefix(t, "tool.poe.tasks.") {
			p.Poe = keysOf(entries, tables, "tool.poe.tasks")
		}
		if t == "tool.ruff" || strings.HasPrefix(t, "tool.ruff.") {
			p.Ruff = true
		}
	}
	if poeInline {
		p.Poe = nil // tasks written inline: tacet can't list them, so it checks none
	}
	switch {
	case has("uv.lock"):
		p.Runner = "uv"
	case has("poetry.lock") || hasTable(tables, "tool.poetry"):
		p.Runner = "poetry"
	case has("pdm.lock") || hasTable(tables, "tool.pdm"):
		p.Runner = "pdm"
	}
	p.Ruff = p.Ruff || p.Deps["ruff"] || has("ruff.toml") || has(".ruff.toml")
	p.Tests = p.Deps["pytest"] || has("tests") || has("conftest.py") || has("pytest.ini")
	return p
}

func hasTable(tables []string, name string) bool {
	for _, t := range tables {
		if t == name || strings.HasPrefix(t, name+".") {
			return true
		}
	}
	return false
}

// toxEnvs reads the environments tox.ini, setup.cfg, tox.toml or
// pyproject.toml define. Nil means there is no tox config.
func toxEnvs(toxINI, setupCfg, toxTOML, pyproject string) map[string]bool {
	var out map[string]bool
	add := func(names ...string) {
		if out == nil {
			out = map[string]bool{}
		}
		for _, n := range names {
			for _, e := range expandBraces(strings.TrimSpace(n)) {
				if e != "" {
					out[e] = true
				}
			}
		}
	}
	ini := func(text, toxSection string) {
		secs := iniSections(text)
		if _, ok := secs[toxSection]; !ok && !strings.Contains(text, "[testenv") {
			return
		}
		add()
		for _, k := range []string{"envlist", "env_list"} {
			add(splitEnvList(secs[toxSection][k])...)
		}
		for name := range secs {
			if env, ok := strings.CutPrefix(name, "testenv:"); ok {
				add(env)
			}
		}
	}
	if toxINI != "" {
		ini(toxINI, "tox")
	}
	if setupCfg != "" {
		ini(setupCfg, "tox:tox")
	}
	toml := func(text, prefix string) {
		entries, tables := scanTOML(text)
		found := false
		for _, e := range entries {
			if e.table == strings.TrimSuffix(prefix, ".") {
				switch e.key {
				case "env_list", "envlist":
					found = true
					add(tomlStrings(e.value)...)
				case "legacy_tox_ini":
					found = true
					ini(strings.Trim(e.value, `"'`), "tox")
				}
			}
		}
		for k := range keysOf(entries, tables, prefix+"env") {
			found = true
			add(k)
		}
		if found {
			add()
		}
	}
	if toxTOML != "" {
		toml(toxTOML, "")
	}
	if pyproject != "" && strings.Contains(pyproject, "[tool.tox") {
		toml(pyproject, "tool.tox.")
	}
	return out
}

func splitEnvList(v string) []string {
	var out []string
	depth := 0
	cur := ""
	for _, r := range v {
		switch {
		case r == '{':
			depth++
		case r == '}':
			depth--
		case (r == ',' || r == '\n') && depth == 0:
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

// expandBraces turns py{311,312}-lint into py311-lint and py312-lint.
func expandBraces(s string) []string {
	i := strings.IndexByte(s, '{')
	j := strings.IndexByte(s, '}')
	if i < 0 || j < i {
		return []string{s}
	}
	var out []string
	for _, alt := range strings.Split(s[i+1:j], ",") {
		out = append(out, expandBraces(s[:i]+strings.TrimSpace(alt)+s[j+1:])...)
	}
	return out
}

var pyFactorRe = regexp.MustCompile(`^(py|pypy)(\d+(\.\d+)?)?$`)

// toxKnown accepts an env tox defines or can build from known factors.
func toxKnown(envs map[string]bool, env string) bool {
	if envs[env] || env == "ALL" || pyFactorRe.MatchString(env) {
		return true
	}
	factors := map[string]bool{}
	for e := range envs {
		for _, f := range strings.Split(e, "-") {
			factors[f] = true
		}
	}
	for _, f := range strings.Split(env, "-") {
		if !factors[f] && !pyFactorRe.MatchString(f) {
			return false
		}
	}
	return true
}

// Cargo's own subcommands, its one-letter aliases and the external
// subcommands people commonly install.
var cargoSubs = toSet(`add b bench build c check clean clippy config d doc fetch fix fmt generate-lockfile help info
init install locate-project login logout metadata miri new owner package pkgid publish r read-manifest remove report
rm run rustc rustdoc search t test tree uninstall update vendor verify-project version yank
about audit binstall bloat bundle careful component criterion cyclonedx deny dist dylint edit expand flamegraph fuzz
geiger hack insta lambda leptos llvm-cov machete make msrv mutants ndk nextest outdated public-api readme release
semver-checks set-version shuttle sort sqlx tarpaulin tauri udeps upgrade watch wasi xtask xwin zigbuild`)

var goSubs = toSet(`bug build clean doc env fix fmt generate get help install list mod run telemetry test tool version vet work`)

// Python tools that run through uv run or poetry run without being
// declared, or that come with Python itself.
var commonPy = toSet(`python python3 pip pip3 pytest ruff mypy pyright black isort flake8 pylint coverage tox nox
pre-commit ipython jupyter sphinx-build mkdocs django-admin uvicorn gunicorn alembic celery flask fastapi hatch
maturin pdm poe pyinstaller twine build bandit vulture pydoc idle`)

func toSet(words string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(words) {
		out[w] = true
	}
	return out
}

var (
	backtickRe = regexp.MustCompile("`([^`]+)`")
	segSplitRe = regexp.MustCompile(`&&|\|\||;|\|`)
	envAssign  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=\S*$`)
	subWordRe  = regexp.MustCompile(`^[a-z][a-z0-9\-]*$`)
)

// shellFences are fence languages whose lines are commands.
var shellFences = toSet(`sh bash shell console zsh fish text terminal`)

// checkLangCommands flags cargo, go, uv run, poetry run, pdm run, poe,
// tox -e and pytest commands that name something the project doesn't
// define. It reads commands from backtick spans and shell code blocks
// only, so prose like "go to the docs" isn't a command.
func checkLangCommands(s *Set, d *Doc) []core.Finding {
	if d.Kind == User {
		return nil
	}
	lg := Languages(s.Root)
	if lg.Cargo == nil && !lg.Go && lg.Python == nil && lg.Tox == nil {
		return nil
	}
	base := filepath.Dir(d.Path)
	if d.Kind == Rule {
		base = s.Root
	}
	var out []core.Finding
	inFence, shell := false, false
	for n, line := range strings.Split(d.Body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") {
			inFence = !inFence
			info := strings.ToLower(strings.TrimSpace(strings.TrimLeft(t, "`")))
			shell = inFence && (info == "" || shellFences[info])
			continue
		}
		var cands []string
		if inFence {
			if shell {
				cands = append(cands, t)
			}
		} else {
			for _, m := range backtickRe.FindAllStringSubmatch(line, -1) {
				cands = append(cands, m[1])
			}
		}
		for _, c := range cands {
			for _, seg := range segSplitRe.Split(c, -1) {
				if msg := lg.check(s.Root, base, commandFields(seg)); msg != "" {
					out = append(out, core.Finding{
						Rule: "stale/command", Severity: core.Medium, Path: rel(s, d.Path), Line: n + 1,
						Message: msg, Hint: "name a command that exists, or define this one",
					})
				}
			}
		}
	}
	return out
}

func commandFields(seg string) []string {
	f := strings.Fields(strings.TrimSpace(seg))
	if len(f) > 0 && (f[0] == "$" || f[0] == ">") {
		f = f[1:]
	}
	for len(f) > 0 && envAssign.MatchString(f[0]) {
		f = f[1:]
	}
	return f
}

// check returns why the command in f is stale, or "".
func (lg *Lang) check(root, base string, f []string) string {
	arg := func(i int) string {
		if i < len(f) {
			return f[i]
		}
		return ""
	}
	switch arg(0) {
	case "cargo":
		i := 1
		if strings.HasPrefix(arg(i), "+") {
			i++ // cargo +nightly build
		}
		sub := arg(i)
		if lg.Cargo == nil || !subWordRe.MatchString(sub) || cargoSubs[sub] || lg.Cargo[sub] || onPath("cargo-"+sub) {
			return ""
		}
		return fmt.Sprintf("cargo %s isn't a cargo command or an alias in .cargo/config.toml", sub)
	case "go":
		sub := arg(1)
		if !lg.Go || !subWordRe.MatchString(sub) || goSubs[sub] {
			return ""
		}
		return fmt.Sprintf("go %s isn't a go command", sub)
	case "uv", "poetry", "pdm":
		if arg(1) != "run" {
			return ""
		}
		return lg.checkPyRun(root, base, arg(0), f[2:])
	case "poe":
		return lg.checkPoe(arg(1))
	case "pytest":
		return lg.checkPytest(root, base, f[1:])
	case "python", "python3":
		if arg(1) == "-m" && arg(2) == "pytest" {
			return lg.checkPytest(root, base, f[3:])
		}
	case "tox":
		return lg.checkTox(f[1:])
	}
	return ""
}

func (lg *Lang) checkPyRun(root, base, runner string, f []string) string {
	p := lg.Python
	if p == nil || len(f) == 0 || strings.HasPrefix(f[0], "-") {
		return ""
	}
	x := f[0]
	switch x {
	case "poe":
		if len(f) > 1 {
			return lg.checkPoe(f[1])
		}
		return ""
	case "pytest":
		return lg.checkPytest(root, base, f[1:])
	case "python", "python3":
		if len(f) > 2 && f[1] == "-m" && f[2] == "pytest" {
			return lg.checkPytest(root, base, f[3:])
		}
	}
	if strings.ContainsAny(x, `/\$`) || strings.HasSuffix(x, ".py") || commonPy[x] || p.Scripts[x] || p.Deps[strings.ToLower(x)] {
		return ""
	}
	if runner == "pdm" && p.PDM[x] {
		return ""
	}
	for _, venv := range []string{".venv/bin", "venv/bin", ".venv/Scripts"} {
		if exists(filepath.Join(root, filepath.FromSlash(venv), x)) {
			return ""
		}
	}
	if onPath(x) {
		return "" // uv run and poetry run fall back to PATH
	}
	where := "[project.scripts] or the dependencies in pyproject.toml"
	if runner == "pdm" {
		where = "[tool.pdm.scripts], " + where
	}
	return fmt.Sprintf("%s run %s: %s isn't in %s", runner, x, x, where)
}

func (lg *Lang) checkPoe(task string) string {
	if lg.Python == nil || lg.Python.Poe == nil || task == "" || strings.HasPrefix(task, "-") || lg.Python.Poe[task] {
		return ""
	}
	return fmt.Sprintf("poe %s isn't a task in [tool.poe.tasks]", task)
}

func (lg *Lang) checkPytest(root, base string, args []string) string {
	if lg.Python == nil && lg.Tox == nil && !exists(filepath.Join(root, "pytest.ini")) && !exists(filepath.Join(root, "setup.py")) {
		return ""
	}
	for i, a := range args {
		if strings.HasPrefix(a, "-") || (i > 0 && optTakesValue[args[i-1]]) {
			continue
		}
		p := strings.SplitN(a, "::", 2)[0]
		if !strings.Contains(p, "/") && !strings.HasSuffix(p, ".py") || strings.ContainsAny(p, "*<>{}$") {
			continue
		}
		if !exists(filepath.Join(base, p)) && !exists(filepath.Join(root, p)) {
			return fmt.Sprintf("pytest %s: the path doesn't exist", p)
		}
	}
	return ""
}

// pytest options whose value is the next word, not a test path.
var optTakesValue = map[string]bool{"-k": true, "-m": true, "-c": true, "-p": true, "--rootdir": true, "--confcutdir": true, "-o": true}

func (lg *Lang) checkTox(args []string) string {
	if lg.Tox == nil {
		return ""
	}
	for i, a := range args {
		var v string
		switch {
		case a == "-e" && i+1 < len(args):
			v = args[i+1]
		case strings.HasPrefix(a, "-e="):
			v = a[3:]
		default:
			continue
		}
		for _, env := range strings.Split(v, ",") {
			if env != "" && !strings.ContainsAny(env, "{}$") && !toxKnown(lg.Tox, env) {
				return fmt.Sprintf("tox env %s isn't defined in the tox config", env)
			}
		}
	}
	return ""
}

func onPath(name string) bool { _, err := exec.LookPath(name); return err == nil }
