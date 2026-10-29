// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package feedback

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// project writes files into a temp dir; keys are slash paths.
func project(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// fakeTools puts shell scripts first on PATH. Each script gets the body
// after its name; "$@" are the arguments the checker passed.
func fakeTools(t *testing.T, tools map[string]string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake tools are shell scripts")
	}
	bin := t.TempDir()
	for name, body := range tools {
		script := "#!/bin/sh\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	return bin
}

func TestSource(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"app.py", "src/lib.rs", "web/a.tsx", "cmd/main.go", "types.pyi"} {
		if !Source(root, filepath.Join(root, p)) {
			t.Errorf("%s should be source", p)
		}
	}
	for _, p := range []string{"README.md", "node_modules/x/a.ts", ".venv/lib/x.py", "target/debug/build.rs", "types.d.ts", "go/testdata/a.go", "../elsewhere.py"} {
		if Source(root, filepath.Join(root, p)) {
			t.Errorf("%s should not be source", p)
		}
	}
}

func TestToolPrefersProjectVenv(t *testing.T) {
	fakeTools(t, map[string]string{"ruff": "exit 0"})
	root := project(t, map[string]string{"pkg/app.py": ""})
	if got := Tool(root, filepath.Join(root, "pkg"), "ruff"); !strings.HasSuffix(got, "ruff") || strings.HasPrefix(got, root) {
		t.Fatalf("PATH ruff not found: %q", got)
	}
	venv := filepath.Join(root, ".venv", "bin")
	os.MkdirAll(venv, 0o755)
	os.WriteFile(filepath.Join(venv, "ruff"), []byte("#!/bin/sh\n"), 0o755)
	if got := Tool(root, filepath.Join(root, "pkg"), "ruff"); got != filepath.Join(venv, "ruff") {
		t.Fatalf("venv ruff not preferred: %q", got)
	}
	nm := filepath.Join(root, "node_modules", ".bin")
	os.MkdirAll(nm, 0o755)
	os.WriteFile(filepath.Join(nm, "tsc"), []byte("#!/bin/sh\n"), 0o755)
	if got := Tool(root, filepath.Join(root, "pkg"), "tsc"); got != filepath.Join(nm, "tsc") {
		t.Fatalf("node_modules tsc not found: %q", got)
	}
	if got := Tool(root, root, "pyright"); got != "" {
		t.Fatalf("missing tool found: %q", got)
	}
}

func TestPythonFailuresOnly(t *testing.T) {
	bin := fakeTools(t, map[string]string{
		"ruff": `echo "$@" > "$(dirname "$0")/ruff.args"
printf 'app.py:1:8: F401 [*] \033[31mos\033[0m imported but unused\n'
printf 'app.py:1:8: F401 [*] \033[31mos\033[0m imported but unused\n'
echo "Found 1 error."
echo "[*] 1 fixable with the --fix option."
exit 1`,
		"pyright": `echo "/x/app.py"
echo "  /x/app.py:3:5 - error: \"undefined_name\" is not defined"
echo "  /x/app.py:4:1 - warning: unused expression"
echo "1 error, 1 warning, 0 informations"
exit 1`,
	})
	root := project(t, map[string]string{"pyproject.toml": "[tool.ruff]\nfix = true\n", "app.py": "import os\n"})
	file := filepath.Join(root, "app.py")
	res := Run(context.Background(), file, Detect(root, file))
	for _, want := range []string{"ruff check:", "F401 [*] os imported but unused", "pyright:", `"undefined_name" is not defined`} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("missing %q in:\n%s", want, res.Text)
		}
	}
	for _, bad := range []string{"\x1b", "Found 1 error", "fixable", "warning", "informations"} {
		if strings.Contains(res.Text, bad) {
			t.Errorf("%q should be dropped:\n%s", bad, res.Text)
		}
	}
	if strings.Count(res.Text, "F401") != 1 {
		t.Errorf("duplicate lines kept:\n%s", res.Text)
	}
	args, _ := os.ReadFile(filepath.Join(bin, "ruff.args"))
	if !strings.Contains(string(args), "--no-fix") || strings.Contains(string(args), "--fix ") {
		t.Errorf("ruff must never autofix, args: %s", args)
	}
}

func TestCleanFileSaysNothing(t *testing.T) {
	fakeTools(t, map[string]string{"ruff": "echo 'All checks passed!'; exit 0"})
	root := project(t, map[string]string{"app.py": "print(1)\n"})
	file := filepath.Join(root, "app.py")
	if res := Run(context.Background(), file, Detect(root, file)); res.Text != "" || len(res.TimedOut) != 0 {
		t.Fatalf("clean file produced %+v", res)
	}
	// Not source, or no checker installed: no checks at all.
	if c := Detect(root, filepath.Join(root, "notes.md")); c != nil {
		t.Fatalf("markdown got checks: %+v", c)
	}
	if c := Detect(root, filepath.Join(root, "main.rs")); c != nil {
		t.Fatalf("rust without cargo or Cargo.toml got checks: %+v", c)
	}
}

func TestTimeoutSaysNothing(t *testing.T) {
	fakeTools(t, map[string]string{"ruff": "sleep 30; echo 'app.py:1:1: E999 late'; exit 1"})
	old := CheckTimeout
	CheckTimeout = 300 * time.Millisecond
	t.Cleanup(func() { CheckTimeout = old })
	root := project(t, map[string]string{"app.py": "x\n"})
	file := filepath.Join(root, "app.py")
	start := time.Now()
	res := Run(context.Background(), file, Detect(root, file))
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("timeout not enforced: %s", took)
	}
	if res.Text != "" || len(res.TimedOut) != 1 || res.TimedOut[0] != "ruff check" {
		t.Fatalf("timed out check should be skipped and reported: %+v", res)
	}
}

func TestRustKeepsErrorsOnly(t *testing.T) {
	fakeTools(t, map[string]string{"cargo": `echo "src/main.rs:1:5: warning: unused variable: x" >&2
echo "src/main.rs:2:5: error[E0425]: cannot find value y in this scope" >&2
echo "error: could not compile demo (bin demo) due to 1 previous error" >&2
exit 101`})
	root := project(t, map[string]string{"Cargo.toml": "[package]\nname = \"demo\"\n", "src/main.rs": "fn main() {}\n"})
	file := filepath.Join(root, "src", "main.rs")
	res := Run(context.Background(), file, Detect(root, file))
	if !strings.Contains(res.Text, "error[E0425]") || strings.Contains(res.Text, "warning") || strings.Contains(res.Text, "could not compile") {
		t.Fatalf("cargo output:\n%s", res.Text)
	}
}

func TestTypeScriptKeepsChangedFile(t *testing.T) {
	fakeTools(t, map[string]string{"tsc": `echo "src/a.ts(3,7): error TS2322: Type 'string' is not assignable to type 'number'."
echo "  The expected type comes from here."
echo "src/other.ts(1,1): error TS2304: Cannot find name 'zzz'."
exit 2`})
	root := project(t, map[string]string{"tsconfig.json": "{}", "src/a.ts": "", "src/other.ts": ""})
	file := filepath.Join(root, "src", "a.ts")
	res := Run(context.Background(), file, Detect(root, file))
	if !strings.Contains(res.Text, "TS2322") || !strings.Contains(res.Text, "expected type") || strings.Contains(res.Text, "other.ts") {
		t.Fatalf("tsc output:\n%s", res.Text)
	}
	// No tsconfig: tsc would check nothing useful, so it doesn't run.
	bare := project(t, map[string]string{"a.ts": ""})
	if c := Detect(bare, filepath.Join(bare, "a.ts")); c != nil {
		t.Fatalf("no tsconfig got checks: %+v", c)
	}
}

func TestGoVet(t *testing.T) {
	fakeTools(t, map[string]string{"go": `[ "$1" = vet ] || exit 9
echo "# example.com/x" >&2
echo "./a.go:3:2: fmt.Printf format %d has arg s of wrong type string" >&2
exit 1`})
	root := project(t, map[string]string{"go.mod": "module example.com/x\n", "a.go": "package x\n"})
	file := filepath.Join(root, "a.go")
	res := Run(context.Background(), file, Detect(root, file))
	if !strings.Contains(res.Text, "go vet:\n./a.go:3:2") || strings.Contains(res.Text, "# example.com") {
		t.Fatalf("go vet output:\n%s", res.Text)
	}
	noMod := project(t, map[string]string{"a.go": "package x\n"})
	if c := Detect(noMod, filepath.Join(noMod, "a.go")); c != nil {
		t.Fatalf("no go.mod got checks: %+v", c)
	}
}

func TestCap(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line")
	}
	lines[99] = "summary"
	got := strings.Split(Cap(strings.Join(lines, "\n")), "\n")
	if len(got) != MaxLines || got[len(got)-1] != "summary" || !strings.Contains(strings.Join(got, "\n"), "more lines") {
		t.Fatalf("cap kept %d lines, last %q", len(got), got[len(got)-1])
	}
	if Cap("a\nb") != "a\nb" {
		t.Fatal("short text changed")
	}
}

func TestTestCommand(t *testing.T) {
	fakeTools(t, map[string]string{"go": "exit 0", "cargo": "exit 0", "pytest": "exit 0", "npm": "exit 0"})
	cases := []struct {
		files map[string]string
		want  string
	}{
		{map[string]string{"go.mod": "module x\n"}, "go test ./..."},
		{map[string]string{"Cargo.toml": "[package]\n"}, "cargo test -q"},
		{map[string]string{"pyproject.toml": "[project]\n"}, "pytest -q --tb=short"},
		{map[string]string{"package.json": `{"scripts":{"test":"vitest run"}}`, "node_modules/.keep": ""}, "npm test"},
		{map[string]string{"package.json": `{"scripts":{"test":"echo \"Error: no test specified\" && exit 1"}}`, "node_modules/.keep": ""}, ""},
		{map[string]string{"package.json": `{"scripts":{"test":"vitest run"}}`}, ""}, // nothing installed
		{map[string]string{"README.md": "x"}, ""},
	}
	for _, c := range cases {
		got := ""
		if cmd := TestCommand(project(t, c.files)); cmd != nil {
			got = cmd.Name
		}
		if got != c.want {
			t.Errorf("%v: got %q want %q", c.files, got, c.want)
		}
	}
}

func TestGateRetriesAndStopHookActive(t *testing.T) {
	bin := fakeTools(t, map[string]string{"go": `echo run >> "$(dirname "$0")/runs"
if [ -f "$(dirname "$0")/green" ]; then echo "ok  	x	0.1s"; exit 0; fi
echo "--- FAIL: TestAdd (0.00s)"
echo "    add_test.go:9: got 3 want 4"
echo "FAIL"
exit 1`})
	runs := func() int {
		b, _ := os.ReadFile(filepath.Join(bin, "runs"))
		return strings.Count(string(b), "run")
	}
	root := project(t, map[string]string{"go.mod": "module x\n"})
	ctx := context.Background()

	if r, _ := Gate(ctx, root, "s1", false); r != "" || runs() != 0 {
		t.Fatalf("no edits: gate ran or blocked (%q, %d runs)", r, runs())
	}
	MarkEdit(root, "s1")
	r, _ := Gate(ctx, root, "s1", false)
	if !strings.Contains(r, "--- FAIL: TestAdd") || !strings.Contains(r, "check 1 of 2") || strings.Contains(r, "ok  ") {
		t.Fatalf("first block:\n%s", r)
	}
	// Continuing because of the block without any edit: let it stop.
	if r, _ := Gate(ctx, root, "s1", true); r != "" || runs() != 1 {
		t.Fatalf("stop_hook_active without edits should allow the stop (%q, %d runs)", r, runs())
	}
	MarkEdit(root, "s1")
	if r, _ := Gate(ctx, root, "s1", true); !strings.Contains(r, "check 2 of 2") {
		t.Fatalf("second block:\n%s", r)
	}
	MarkEdit(root, "s1")
	if r, _ := Gate(ctx, root, "s1", true); r != "" || runs() != 2 {
		t.Fatalf("retries exhausted: should allow without running (%q, %d runs)", r, runs())
	}
	// A new session starts with a fresh budget; green tests let it stop.
	os.WriteFile(filepath.Join(bin, "green"), nil, 0o644)
	MarkEdit(root, "s2")
	if r, _ := Gate(ctx, root, "s2", false); r != "" || runs() != 3 {
		t.Fatalf("green run should allow the stop (%q, %d runs)", r, runs())
	}
	if r, _ := Gate(ctx, root, "s2", false); r != "" || runs() != 3 {
		t.Fatalf("no edits since green: tests ran again (%d runs)", runs())
	}
}

func TestGateTimeout(t *testing.T) {
	fakeTools(t, map[string]string{"go": "sleep 30; exit 1"})
	old := TestTimeout
	TestTimeout = 300 * time.Millisecond
	t.Cleanup(func() { TestTimeout = old })
	root := project(t, map[string]string{"go.mod": "module x\n"})
	MarkEdit(root, "s")
	start := time.Now()
	r, timedOut := Gate(context.Background(), root, "s", false)
	if r != "" || !timedOut || time.Since(start) > 5*time.Second {
		t.Fatalf("slow tests should be skipped: %q %v %s", r, timedOut, time.Since(start))
	}
}
