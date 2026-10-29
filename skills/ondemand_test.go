// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package skills

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/claude"
)

// line is one transcript entry for a synthetic session.
type line map[string]any

func human(text string) line {
	return line{"type": "user", "sessionId": "s1", "origin": map[string]any{"kind": "human"},
		"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": text}}}}
}

func tool(id, name string, input map[string]any) line {
	return line{"type": "assistant", "sessionId": "s1", "message": map[string]any{
		"id": id, "role": "assistant", "model": "claude-opus-5-5",
		"content": []any{map[string]any{"type": "tool_use", "id": "t" + id, "name": name, "input": input}},
	}}
}

func result() line {
	return line{"type": "user", "sessionId": "s1", "message": map[string]any{"role": "user",
		"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "x", "content": "ok"}}}}
}

// session writes a transcript into root's Claude project folder.
func session(t *testing.T, root string, ls []line) string {
	t.Helper()
	dir := claude.ProjectDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, l := range ls {
		j, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(j)
		b.WriteByte('\n')
	}
	p := filepath.Join(dir, "s1.jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeToken is assembled at run time so no literal key sits in the repo.
func fakeToken() string { return "gh" + "p_" + strings.Repeat("a1B2", 10) }

func releaseSession(t *testing.T) (root, path string) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	root = t.TempDir()
	path = session(t, root, []line{
		human("fix the typo in the readme please"),
		tool("m0", "Edit", map[string]any{"file_path": filepath.Join(root, "README.md")}),
		result(),
		human("Bump the version in version.go and write the changelog entry for the release"),
		tool("m1", "Read", map[string]any{"file_path": filepath.Join(root, "CHANGELOG.md")}),
		result(),
		tool("m2", "Bash", map[string]any{"command": "git log --oneline -20"}),
		result(),
		tool("m3", "Edit", map[string]any{"file_path": filepath.Join(root, "version.go")}),
		tool("m4", "Edit", map[string]any{"file_path": filepath.Join(root, "CHANGELOG.md")}),
		tool("m5", "TodoWrite", map[string]any{}),
		tool("m6", "Bash", map[string]any{"command": "cd " + root + " && make check"}),
		result(),
		human("yes"),
		tool("m7", "Bash", map[string]any{"command": "GH_TOKEN=" + fakeToken() + " gh release create v1.2.0"}),
		tool("m8", "Bash", map[string]any{"command": "cd " + root + " && make check"}),
		human("now make this a skill"),
		tool("m9", "Bash", map[string]any{"command": "tacet skills new --from-session --json"}),
	})
	return root, path
}

func TestNewFromSession(t *testing.T) {
	root, _ := releaseSession(t)
	d, err := NewFromSession(root, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "bump-version-go-write" {
		t.Errorf("name %q", d.Name)
	}
	if d.Description != "Use when asked to bump the version in version.go and write the changelog entry for the release." {
		t.Errorf("description %q", d.Description)
	}
	want := []string{
		"Edit `README.md`",
		"Read `CHANGELOG.md`",
		"Edit `version.go`, `CHANGELOG.md`",
		"Run `make check`",
		"Run `GH_TOKEN=[masked] gh release create v1.2.0`",
	}
	if strings.Join(d.Steps, "\n") != strings.Join(want, "\n") {
		t.Errorf("steps:\n%s", strings.Join(d.Steps, "\n"))
	}
	if len(d.Requests) != 2 || !strings.HasPrefix(d.Requests[0], "fix the typo") {
		t.Errorf("requests %q", d.Requests)
	}
	golden(t, "ondemand-session.md", d.Skill)
	if strings.Contains(d.Skill, fakeToken()) || strings.Contains(d.Skill, root) {
		t.Error("draft leaks a secret or the home path")
	}
}

func TestNewFromSessionHintAndNoSession(t *testing.T) {
	root, path := releaseSession(t)
	d, err := NewFromSession(root, path, "cut a release of the cli", "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "cut-release-cli" || d.Description != "Use when asked to cut a release of the cli." || len(d.Steps) == 0 {
		t.Errorf("%+v", d)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if _, err := NewFromSession(t.TempDir(), "", "", ""); !errors.Is(err, ErrNoSession) {
		t.Errorf("want ErrNoSession, got %v", err)
	}
}

func TestNewFromDescription(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	d, err := NewFromDescription(root, "Please run the e2e tests against staging; then post the summary to the team channel with token="+fakeToken(), "")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "ondemand-description.md", d.Skill)
	if strings.Contains(d.Skill, fakeToken()) {
		t.Error("secret in draft")
	}
	if _, err := NewFromDescription(root, "  ", ""); err == nil {
		t.Error("empty description accepted")
	}
}

func TestSentence(t *testing.T) {
	long := "Use when asked to " + strings.Repeat("deploy the service ", 30) + ". Then more."
	for in, want := range map[string]string{
		"Use when the build breaks on main. It reads the CI log.": "Use when the build breaks on main.",
		"please deploy the app to staging":                        "Use when asked to deploy the app to staging.",
	} {
		if got := Sentence(in); got != want {
			t.Errorf("Sentence(%q) = %q", in, got)
		}
	}
	got := Sentence(long)
	if len([]rune(got)) > MaxDescription || !strings.HasPrefix(got, "Use when") || strings.Count(got, ". ") > 0 {
		t.Errorf("long: %d %q", len(got), got)
	}
}

func TestWriteNeverOverwrites(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	d, err := NewFromDescription(root, "deploy the app to staging", "deploy")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(d.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.Path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(); !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	if b, _ := os.ReadFile(d.Path); string(b) != "mine" {
		t.Fatal("existing skill changed")
	}
	// A derived name steps around the existing skill instead.
	d2, _ := NewFromDescription(root, "deploy", "")
	if d2.Name != "deploy-2" {
		t.Fatalf("name %q", d2.Name)
	}
	if err := d2.Write(); err != nil {
		t.Fatal(err)
	}
}

// golden compares got with testdata/name; TACET_GOLDEN=1 rewrites it.
func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if os.Getenv("TACET_GOLDEN") != "" {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs:\n--- got\n%s--- want\n%s", name, got, want)
	}
}
