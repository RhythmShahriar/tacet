// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/claude"
	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/fix"
)

// transcripts writes one JSONL session per entry under a fake Claude
// config dir and returns the project root they belong to.
func transcripts(t *testing.T, sessions map[string][]string) string {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	dir := claude.ProjectDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for id, prompts := range sessions {
		writeSession(t, filepath.Join(dir, id+".jsonl"), id, prompts)
	}
	return root
}

func writeSession(t *testing.T, path, id string, prompts []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i, p := range prompts {
		text, _ := json.Marshal(p)
		b.WriteString(`{"type":"user","timestamp":"2026-10-29T01:00:0` + string(rune('0'+i%10)) + `Z","sessionId":"` + id + `","origin":{"kind":"human"},"message":{"role":"user","content":[{"type":"text","text":` + string(text) + `}]}}` + "\n")
		b.WriteString(`{"type":"assistant","timestamp":"2026-10-29T01:00:05Z","message":{"id":"m` + id + string(rune('a'+i%26)) + `","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":1,"output_tokens":1}}}` + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

const commitRule = "Run make check and fix every lint error before you commit anything"

func TestMineRepeated(t *testing.T) {
	root := transcripts(t, map[string][]string{
		"s1": {commitRule, "yes", "add a dark mode toggle to the settings page"},
		"s2": {"please run make check and fix every lint error before you commit anything.", "why is parseConfig() returning nil?"},
		"s3": {"Run make check and fix every lint error before you commit and keep the subject under 72 chars"},
	})
	cs := Candidates(root)
	if len(cs) != 1 {
		t.Fatalf("want 1 candidate, got %+v", cs)
	}
	c := cs[0]
	if c.Count != 3 || c.Sessions != 3 || c.Score != 9 {
		t.Fatalf("counts %+v", c)
	}
	if c.Name != "run-check-fix-lint" {
		t.Fatalf("name %q", c.Name)
	}
	if !strings.HasPrefix(c.Description, "Use when asked to run make check") {
		t.Fatalf("description %q", c.Description)
	}
}

func TestOneOffsDontQualify(t *testing.T) {
	cases := map[string]map[string][]string{
		"one session":  {"s1": {commitRule, commitRule + " now", commitRule + " please"}},
		"twice":        {"s1": {commitRule}, "s2": {commitRule}},
		"all distinct": {"s1": {"add a dark mode toggle to the settings page"}, "s2": {"write a migration for the orders table"}, "s3": {"rename the billing service to payments everywhere"}},
	}
	for name, sessions := range cases {
		t.Run(name, func(t *testing.T) {
			if cs := Candidates(transcripts(t, sessions)); len(cs) != 0 {
				t.Fatalf("want none, got %+v", cs)
			}
		})
	}
}

func TestSubagentsIgnored(t *testing.T) {
	root := transcripts(t, map[string][]string{"s1": {commitRule}, "s2": {commitRule}})
	dir := claude.ProjectDir(root)
	writeSession(t, filepath.Join(dir, "s1", "subagents", "a.jsonl"), "s1", []string{commitRule, commitRule})
	writeSession(t, filepath.Join(dir, "s2", "subagents", "b.jsonl"), "s2", []string{commitRule})
	if cs := Candidates(root); len(cs) != 0 {
		t.Fatalf("subagent prompts counted: %+v", cs)
	}
}

func TestInstructionFilter(t *testing.T) {
	yes := []string{
		commitRule,
		"can you update the changelog and bump the version?",
		"Always write tests for every new handler you add",
	}
	no := []string{
		"yes",
		"check now",
		"why does the login form submit twice?",
		"is the cache_key still used anywhere?",
		"can you tell me what parseConfig() returns?",
		"ok do it now",
		"panic: runtime error: index out of range [3] with length 3 goroutine 1 [running]: main.go:42 +0x1d",
	}
	for _, s := range yes {
		if !instruction(s) {
			t.Errorf("should count: %q", s)
		}
	}
	for _, s := range no {
		if instruction(s) {
			t.Errorf("should skip: %q", s)
		}
	}
	log := strings.Repeat("2026-10-29T01:00:00Z ERROR db.go:88 conn=0x1f refused 10.0.0.4:5432\n", 20)
	for _, s := range []string{"This session is being continued from a previous conversation.", "/review the diff", log, strings.Repeat("word ", 500)} {
		if !skipPrompt(s) {
			t.Errorf("prompt should be skipped: %.40q", s)
		}
	}
	if skipPrompt(commitRule) {
		t.Error("a plain instruction was skipped")
	}
}

func TestName(t *testing.T) {
	cases := []struct {
		texts []string
		want  string
	}{
		{[]string{"please update the CHANGELOG before every release"}, "update-changelog-before-release"},
		{[]string{"Run the e2e tests on chrome", "run the e2e tests on firefox too", "run e2e tests"}, "run-e2e-tests"},
		{[]string{"[r2805] Read prd.docx and quote its first heading"}, "read-prd-docx-quote"},
		{[]string{"yes ok"}, "repeated-request"},
		{nil, "repeated-request"},
	}
	for _, c := range cases {
		if got := Name(c.texts); got != c.want {
			t.Errorf("Name(%q) = %q, want %q", c.texts, got, c.want)
		}
	}
	used := map[string]bool{"deploy": true}
	if got := uniqueName("deploy", used); got != "deploy-2" {
		t.Fatalf("unique %q", got)
	}
}

func TestDescribe(t *testing.T) {
	cases := map[string]string{
		"Can you please run the full test suite?":   "Use when asked to run the full test suite.",
		"Then summarize the document in 4 bullets.": "Use when asked to summarize the document in 4 bullets.",
		"CHANGELOG first, then tag":                 "Use when asked to CHANGELOG first, then tag.",
		"please":                                    "Use when asked to repeat this request.",
	}
	for in, want := range cases {
		if got := Describe(in); got != want {
			t.Errorf("Describe(%q) = %q, want %q", in, got, want)
		}
	}
}

// Fake keys are assembled at run time so no literal key sits in the repo.
func fakeKeys() map[string]string {
	return map[string]string{
		"github": "gh" + "p_" + strings.Repeat("a1B2", 10),
		"openai": "sk" + "-proj-" + strings.Repeat("Xy9", 8),
		"aws":    "AK" + "IA" + strings.Repeat("Q7", 8),
		"slack":  "xo" + "xb-" + strings.Repeat("12345-", 3) + "abcdef",
		"blob":   strings.Repeat("aZ3k", 9),
	}
}

func TestMask(t *testing.T) {
	for name, key := range fakeKeys() {
		got := Mask("use " + key + " for the deploy")
		if strings.Contains(got, key) || !strings.Contains(got, Masked) {
			t.Errorf("%s key not masked: %q", name, got)
		}
	}
	pw := "hunter" + "2hunter2"
	if got := Mask("set DB_PASSWORD=" + pw + " then run it"); strings.Contains(got, pw) || !strings.Contains(got, "DB_PASSWORD="+Masked) {
		t.Errorf("assignment: %q", got)
	}
	if got := Mask("clone https://bob:" + pw + "@git.example.com/x.git"); strings.Contains(got, pw) {
		t.Errorf("url credentials: %q", got)
	}
	if got := Mask("open /home/alice/ws/app/main.go and /Users/bob/notes.md"); got != "open ~/ws/app/main.go and ~/notes.md" {
		t.Errorf("home paths: %q", got)
	}
	for _, keep := range []string{
		commitRule,
		"the token: yes",
		"see can-you-check-this-encapsulated-nest for the plan",
		"run go test ./... before pushing",
	} {
		if got := Mask(keep); got != keep {
			t.Errorf("changed plain text %q to %q", keep, got)
		}
	}
}

func TestDraft(t *testing.T) {
	key := fakeKeys()["github"]
	c := Candidate{
		Name: "deploy-staging", Description: Describe("deploy to staging with token " + key),
		Count:   4,
		Example: "Deploy to staging with token " + key + " from /home/alice/app",
		Variants: []string{
			"deploy to staging with the token",                         // a rewording: adds nothing
			"deploy to staging and post the preview URL in #releases",  // adds detail
			"Deploy to staging and post the preview URL in #releases!", // same detail again
		},
	}
	got := Draft(c)
	for _, want := range []string{
		"---\nname: deploy-staging\ndescription: \"Use when asked to deploy to staging with token [masked].\"\n---\n",
		"<!-- tacet · drafted from 4 repeated requests -->",
		"# Deploy staging",
		"Deploy to staging with token [masked] from ~/app",
		"## Details from similar requests\n\n- deploy to staging and post the preview URL in #releases\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("draft missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, key) || strings.Contains(got, "/home/alice") || strings.Count(got, "preview URL") != 1 {
		t.Errorf("draft leaks or repeats:\n%s", got)
	}
	if !strings.HasPrefix(got, "---\n") {
		t.Error("frontmatter must open the file")
	}
}

func TestFindingsApply(t *testing.T) {
	root := transcripts(t, map[string][]string{
		"s1": {commitRule}, "s2": {commitRule}, "s3": {commitRule + "!"},
	})
	fs := Suggest(root, nil, nil, true)
	if len(fs) != 1 {
		t.Fatalf("findings %+v", fs)
	}
	f := fs[0]
	if f.Rule != "skills/repeated" || f.Fix == nil || f.Fix.Safe || f.Fix.ID != "skill-run-check-fix-lint" {
		t.Fatalf("finding %+v fix %+v", f, f.Fix)
	}
	if _, err := fix.Apply(root, []*core.Fix{f.Fix}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, ".claude", "skills", "run-check-fix-lint", "SKILL.md"))
	if err != nil || !strings.Contains(string(b), "drafted from 3 repeated requests") {
		t.Fatalf("skill %q %v", b, err)
	}
	// Once the skill exists it is no longer suggested.
	if fs := Suggest(root, nil, nil, true); len(fs) != 0 {
		t.Fatalf("still suggested: %+v", fs)
	}
}

func TestSuggestOff(t *testing.T) {
	root := transcripts(t, map[string][]string{
		"s1": {commitRule}, "s2": {commitRule}, "s3": {commitRule},
	})
	if fs := Suggest(root, nil, nil, false); fs != nil {
		t.Fatalf("feature off still suggests: %+v", fs)
	}
}

func FuzzMask(f *testing.F) {
	f.Add(commitRule)
	f.Add("token=" + strings.Repeat("aZ3k", 9))
	f.Fuzz(func(t *testing.T, s string) {
		_ = Mask(s)
		_ = Name([]string{s})
		_ = Describe(s)
	})
}
