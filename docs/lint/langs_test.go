// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package lint

import (
	"strings"
	"testing"
)

const pyproject = `[project]
name = "app"
dependencies = [
  "fastapi>=0.110",  # web
  "uvicorn[standard]",
]

[project.scripts]
serve = "app.main:run"

[dependency-groups]
dev = ["pytest>=8", "ruff"]

[tool.poe.tasks]
lint = "ruff check ."
test.cmd = "pytest"

[tool.poe.tasks.docs]
cmd = "mkdocs build"

[tool.pdm.scripts]
start = "python -m app"
`

const toxINI = `[tox]
envlist =
    py{311,312}
    lint

[testenv:docs]
commands = sphinx-build docs out
`

// Each case is a doc line with a positive (stale) and negative (valid)
// command for one tool, over the same project files.
func TestLangCommands(t *testing.T) {
	cases := []struct {
		name     string
		files    map[string]string
		pos, neg string
	}{
		{"cargo builtin", map[string]string{"Cargo.toml": "[package]\nname = \"x\"\n"}, "Run `cargo tset`.", "Run `cargo test` and `cargo +nightly fmt`."},
		{"cargo alias", map[string]string{"Cargo.toml": "[package]\n", ".cargo/config.toml": "[alias]\nxt = \"run -p xtask --\"\n"}, "```sh\ncargo xtt dist\n```", "```sh\ncargo xt dist\n```"},
		{"go", map[string]string{"go.mod": "module x\n"}, "Run `go tset ./...`.", "Run `go test ./...` and `go vet ./...`. Go to the docs to go ahead."},
		{"uv run", map[string]string{"pyproject.toml": pyproject, "uv.lock": ""}, "Start it with `uv run srve-zzq`.", "Start it with `uv run serve`, `uv run uvicorn app:app` or `uv run python -m app`."},
		{"poetry run", map[string]string{"pyproject.toml": pyproject, "poetry.lock": ""}, "```bash\npoetry run nope-zzq\n```", "```bash\npoetry run pytest -q\n```"},
		{"pdm run", map[string]string{"pyproject.toml": pyproject, "pdm.lock": ""}, "`pdm run strt-zzq`", "`pdm run start`"},
		{"poe", map[string]string{"pyproject.toml": pyproject}, "Run `poe tset` or `uv run poe fmt`.", "Run `poe test`, `poe docs` or `uv run poe lint`."},
		{"pytest path", map[string]string{"pyproject.toml": pyproject, "tests/test_api.py": ""}, "Run `pytest tests/test_gone.py -q`.", "Run `pytest tests/test_api.py::test_ok -k slow`."},
		{"tox env", map[string]string{"tox.ini": toxINI}, "Run `tox -e typing`.", "Run `tox -e py312,lint`, `tox -e docs` or `tox -e py310-lint`."},
		{"tox in pyproject", map[string]string{"pyproject.toml": "[tool.tox]\nenv_list = [\"3.13\", \"type\"]\n\n[tool.tox.env.type]\ncommands = [[\"mypy\"]]\n"}, "`tox -e lint`", "`tox -e type`"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pos := copyFiles(c.files)
			pos["CLAUDE.md"] = c.pos + "\n"
			if rules(t, pos)["stale/command"] == 0 {
				t.Errorf("positive fixture not reported: %s", c.pos)
			}
			neg := copyFiles(c.files)
			neg["CLAUDE.md"] = c.neg + "\n"
			if n := rules(t, neg)["stale/command"]; n != 0 {
				t.Errorf("negative fixture reported %d times: %s", n, c.neg)
			}
		})
	}
}

// Without the tool's project file nothing is checked: unknown tools are
// never flagged.
func TestLangCommandsNeedProjectFiles(t *testing.T) {
	doc := "Run `cargo tset`, `go tset`, `uv run zzq-nope`, `poe zzq`, `tox -e zzq` and `pytest tests/gone.py`.\n"
	if n := rules(t, map[string]string{"CLAUDE.md": doc})["stale/command"]; n != 0 {
		t.Fatalf("flagged %d commands with no project files", n)
	}
	// Inline poe tasks can't be listed, so poe isn't checked.
	inline := map[string]string{"CLAUDE.md": "`poe zzq`\n", "pyproject.toml": "[tool.poe]\ntasks = { a = \"x\" }\n"}
	if n := rules(t, inline)["stale/command"]; n != 0 {
		t.Fatalf("inline poe tasks flagged %d", n)
	}
	// Code blocks in other languages aren't commands.
	code := map[string]string{"CLAUDE.md": "```python\ngo tset = 1\n```\n", "go.mod": "module x\n"}
	if n := rules(t, code)["stale/command"]; n != 0 {
		t.Fatalf("python block flagged %d", n)
	}
}

func TestLanguagesReadsPython(t *testing.T) {
	root := project(t, map[string]string{"pyproject.toml": pyproject, "uv.lock": "", "tests/x.py": ""})
	p := Languages(root).Python
	if p == nil || p.Runner != "uv" || !p.Scripts["serve"] || !p.Deps["fastapi"] || !p.Deps["uvicorn"] || !p.Deps["pytest"] {
		t.Fatalf("python: %+v", p)
	}
	for _, task := range []string{"lint", "test", "docs"} {
		if !p.Poe[task] {
			t.Errorf("poe task %s missing: %v", task, p.Poe)
		}
	}
	if !p.PDM["start"] || !p.Ruff || !p.Tests {
		t.Errorf("pdm %v ruff %v tests %v", p.PDM, p.Ruff, p.Tests)
	}
	envs := Languages(project(t, map[string]string{"tox.ini": toxINI})).Tox
	got := []string{}
	for _, e := range []string{"py311", "py312", "lint", "docs"} {
		if !envs[e] {
			got = append(got, e)
		}
	}
	if len(got) > 0 {
		t.Fatalf("tox envs missing %s: %v", strings.Join(got, ","), envs)
	}
}

func copyFiles(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
