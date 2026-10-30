// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const fullStatus = `{"session_id":"abc","workspace":{"current_dir":"%s","project_dir":"%s"},
"cost":{"total_cost_usd":1.234},
"context_window":{"used_percentage":8.4,"current_usage":{"input_tokens":10,"cache_creation_input_tokens":100,"cache_read_input_tokens":3000}},
"prompt_cache":{"caching_observed":true,"hit_ratio":0.97},
"rate_limits":{"five_hour":{"used_percentage":38.2,"resets_at":1738425600},"seven_day":{"used_percentage":12,"resets_at":1738857600}}}`

// statusRun feeds input to tacet statusline and returns what it printed.
func statusRun(t *testing.T, input string, args ...string) string {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	old := stdin
	stdin = strings.NewReader(input)
	defer func() { stdin = old }()
	var out, errw bytes.Buffer
	if c := run(append([]string{"statusline"}, args...), &out, &errw); c != exitOK {
		t.Fatalf("exit %d: %s", c, errw.String())
	}
	return strings.TrimRight(out.String(), "\n")
}

// savingsProject is a folder with .tacet/savings.jsonl: 26k saved today,
// 1k read back. The 9k from yesterday must not count.
func savingsProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	now := time.Now().UTC()
	writeFile(t, filepath.Join(root, ".tacet", "savings.jsonl"),
		`{"time":"`+now.Format(time.RFC3339)+`","file":"spec.pdf","saved_tokens":26000,"method":"focus"}
{"time":"`+now.Format(time.RFC3339)+`","file":"spec.pdf","saved_tokens":-1000,"method":"follow-up"}
{"time":"`+now.Add(-48*time.Hour).Format(time.RFC3339)+`","file":"old.pdf","saved_tokens":9000,"method":"focus"}
not json
`)
	return root
}

func TestStatuslineFull(t *testing.T) {
	root := savingsProject(t)
	sub := filepath.Join(root, "src")
	in := strings.ReplaceAll(strings.Replace(fullStatus, "%s", jsonText(sub), 1), "%s", jsonText(root))
	got := statusRun(t, in)
	want := "tacet ▾25k saved · cache 97% · ctx 8% · 5h 38% · 7d 12% · $1.23"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestStatuslineEmpty(t *testing.T) {
	for _, in := range []string{"", "{}", "not json", `{"rate_limits":null,"context_window":{"used_percentage":null,"current_usage":null}}`} {
		if got := statusRun(t, in, "-C", t.TempDir()); got != "tacet" {
			t.Errorf("%q: got %q", in, got)
		}
	}
}

func TestStatuslinePartial(t *testing.T) {
	cases := map[string]string{
		`{"rate_limits":{"five_hour":{"used_percentage":38}}}`: "tacet 5h 38%",
		// No prompt_cache: the hit rate comes from the last request.
		`{"context_window":{"current_usage":{"input_tokens":0,"cache_creation_input_tokens":250,"cache_read_input_tokens":750}}}`: "tacet cache 75%",
		`{"cost":{"total_cost_usd":0}}`:                             "tacet",
		`{"prompt_cache":{"caching_observed":false,"hit_ratio":0}}`: "tacet",
	}
	for in, want := range cases {
		if got := statusRun(t, in, "-C", t.TempDir()); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}

func TestStatuslineColor(t *testing.T) {
	five := 91.0
	in := &statusInput{RateLimits: &struct {
		FiveHour *rateWindow `json:"five_hour"`
		SevenDay *rateWindow `json:"seven_day"`
	}{FiveHour: &rateWindow{UsedPercentage: &five}}}
	got := statusLine(in, "", time.Now(), true)
	if !strings.HasPrefix(got, ansiDot+"tacet"+ansiReset) || !strings.Contains(got, ansiWarn+"5h 91%") {
		t.Errorf("colors: %q", got)
	}
}

func TestStatuslineChain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	got := statusRun(t, `{"rate_limits":{"five_hour":{"used_percentage":5}}}`, "-C", t.TempDir(), "--after", "printf 'mine '; cat | head -c 5")
	if got != `mine {"rat  tacet 5h 5%` {
		t.Errorf("chained: %q", got)
	}
	// A failing command leaves tacet's part alone.
	if got := statusRun(t, `{}`, "-C", t.TempDir(), "--after", "exit 3"); got != "tacet" {
		t.Errorf("failed chain: %q", got)
	}
}

func readSettings(t *testing.T, home string) map[string]any {
	t.Helper()
	var m map[string]any
	b, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestStatuslineInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	root := t.TempDir()
	if len(statuslineFinding()) != 1 {
		t.Fatal("no statusline suggestion without a statusLine")
	}
	out := statusRun(t, "", "-C", root, "--install")
	if !strings.Contains(out, "tacet undo reverts it") {
		t.Errorf("install said %q", out)
	}
	sl, _ := readSettings(t, home)["statusLine"].(map[string]any)
	if sl["type"] != "command" || !strings.HasSuffix(sl["command"].(string), " statusline") || !filepath.IsAbs(strings.Trim(strings.TrimSuffix(sl["command"].(string), " statusline"), "'")) {
		t.Fatalf("statusLine %v", sl)
	}
	if len(statuslineFinding()) != 0 {
		t.Error("still suggested once installed")
	}
	if out := statusRun(t, "", "-C", root, "--install"); !strings.Contains(out, "already on") {
		t.Errorf("second install said %q", out)
	}
	var o, e bytes.Buffer
	if c := run([]string{"-C", root, "undo"}, &o, &e); c != exitOK {
		t.Fatalf("undo exit %d: %s", c, e.String())
	}
	if _, err := os.Stat(filepath.Join(home, "settings.json")); !os.IsNotExist(err) {
		t.Error("undo kept a settings file tacet created")
	}
}

func TestStatuslineInstallKeepsTheirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	root := t.TempDir()
	orig := `{"model":"opus","statusLine":{"type":"command","command":"~/bin/my line.sh","padding":2}}` + "\n"
	writeFile(t, filepath.Join(home, "settings.json"), orig)
	if len(statuslineFinding()) != 0 {
		t.Error("suggested over an existing statusLine")
	}

	out := statusRun(t, "", "-C", root, "--install")
	if !strings.Contains(out, "--chain") || !strings.Contains(out, `--after '~/bin/my line.sh'`) {
		t.Errorf("no chaining advice: %q", out)
	}
	if b, _ := os.ReadFile(filepath.Join(home, "settings.json")); string(b) != orig {
		t.Fatalf("replaced their statusLine: %s", b)
	}

	statusRun(t, "", "-C", root, "--install", "--chain")
	m := readSettings(t, home)
	sl, _ := m["statusLine"].(map[string]any)
	if c, _ := sl["command"].(string); !strings.HasSuffix(c, ` statusline --after '~/bin/my line.sh'`) {
		t.Errorf("chained command %q", c)
	}
	if sl["padding"] != 2.0 || m["model"] != "opus" {
		t.Errorf("lost other keys: %v", m)
	}

	var o, e bytes.Buffer
	if c := run([]string{"-C", root, "undo"}, &o, &e); c != exitOK {
		t.Fatalf("undo exit %d: %s", c, e.String())
	}
	if b, _ := os.ReadFile(filepath.Join(home, "settings.json")); string(b) != orig {
		t.Errorf("undo left %s", b)
	}
}

func TestStatuslineNotInCheck(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	var out, errw bytes.Buffer
	run([]string{"-C", t.TempDir(), "check", "--json", "--fail-on", "info"}, &out, &errw)
	if strings.Contains(out.String(), "statusline/") {
		t.Error("tacet check reports the user statusline")
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/local/bin/tacet": "/usr/local/bin/tacet",
		"/a b/tacet":           "'/a b/tacet'",
		"it's":                 `'it'\''s'`,
		"":                     "''",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// init only suggests the statusline; --yes applies safe fixes alone.
func TestInitSuggestsStatusline(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	root := t.TempDir()
	var out, errw bytes.Buffer
	if c := run([]string{"-C", root, "init", "--yes", "--no-plugin"}, &out, &errw); c != exitOK {
		t.Fatalf("exit %d: %s", c, errw.String())
	}
	if !strings.Contains(out.String(), "tacet statusline --install") {
		t.Errorf("init didn't offer the statusline:\n%s", out.String())
	}
	if _, ok := readSettings(t, home)["statusLine"]; ok {
		t.Error("init set the statusline without a yes")
	}
}
