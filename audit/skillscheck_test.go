// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/measure"
)

func skillMD(name, desc string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\n# " + name + "\n\nsteps\n"
}

func checks(t *testing.T, root string, u *measure.Usage) []core.Finding {
	t.Helper()
	st, _, err := measure.MeasureStartup(root, measure.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return SkillChecks(Input{Root: root, Startup: st, Usage: u})
}

func byRule(fs []core.Finding, rule string) []core.Finding {
	var out []core.Finding
	for _, f := range fs {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

func TestOverlapByJaccard(t *testing.T) {
	root := env(t, map[string]string{
		".claude/skills/test-fix/SKILL.md":   skillMD("test-fix", "Use when asked to run the unit tests and fix every failure before a commit."),
		".claude/skills/test-fix-2/SKILL.md": skillMD("test-fix-2", "Use when asked to run the unit tests and fix every failure before you push."),
		".claude/skills/notes/SKILL.md":      skillMD("notes", "Use when asked to write the release notes from merged pull requests."),
	})
	fs := byRule(checks(t, root, usage(6, nil, map[string]int{"test-fix-2": 4, "test-fix": 1})), "skills/overlap")
	if len(fs) != 1 {
		t.Fatalf("want one overlap, got %+v", fs)
	}
	f := fs[0]
	if !strings.Contains(filepath.ToSlash(f.Path), "/test-fix/") || f.Fix.ID != "skill-overlap-test-fix" || f.Tokens <= 0 || f.Fix.Tokens != f.Tokens {
		t.Fatalf("the less used one should be hidden: %+v", f)
	}
	before, _ := os.ReadFile(f.Path)
	after, err := f.Fix.Edits[0].Change(before)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nname: test-fix\ndescription: Use when asked to run the unit tests and fix every failure before a commit.\ndisable-model-invocation: true\n---\n# test-fix\n\nsteps\n"
	if string(after) != want {
		t.Fatalf("fix gave\n%s", after)
	}
}

func TestOverlapByLeadingPhrase(t *testing.T) {
	root := env(t, map[string]string{
		".claude/skills/a/SKILL.md": skillMD("a", "Use when asked to deploy the app to staging, with smoke tests."),
		".claude/skills/b/SKILL.md": skillMD("b", "Use when asked to deploy the app on a Friday: check the freeze calendar first."),
	})
	fs := byRule(checks(t, root, nil), "skills/overlap")
	if len(fs) != 1 || !strings.Contains(fs[0].Message, `both trigger on "deploy the app"`) {
		t.Fatalf("got %+v", fs)
	}
}

func TestOverlapNegatives(t *testing.T) {
	root := env(t, map[string]string{
		".claude/skills/a/SKILL.md": skillMD("a", "Use when asked to deploy the app to staging."),
		".claude/skills/b/SKILL.md": skillMD("b", "Use when asked to write the release notes."),
		// Hidden skills don't compete for requests.
		".claude/skills/c/SKILL.md": "---\nname: c\ndescription: Use when asked to deploy the app to staging.\ndisable-model-invocation: true\n---\n",
	})
	if fs := byRule(checks(t, root, nil), "skills/overlap"); len(fs) != 0 {
		t.Fatalf("false positive: %+v", fs)
	}
}

func TestDescriptionLengthAndShorten(t *testing.T) {
	sentence := "It checks the build, runs the migrations, warms the cache and posts a note to the release channel. "
	over := "Use when asked to deploy the app. " + strings.Repeat(sentence, 17)  // > 1,536
	near := "Use when asked to release the app. " + strings.Repeat(sentence, 13) // 1,300 to 1,536
	long := "Use when asked to review the code. " + strings.Repeat(sentence, 6)  // 500 to 1,300
	root := env(t, map[string]string{
		".claude/skills/over/SKILL.md":  skillMD("over", over),
		".claude/skills/near/SKILL.md":  skillMD("near", near),
		".claude/skills/long/SKILL.md":  skillMD("long", long),
		".claude/skills/short/SKILL.md": skillMD("short", "Use when asked to lint the code."),
	})
	got := map[string]core.Finding{}
	for _, f := range byRule(checks(t, root, nil), "skills/description") {
		p := filepath.ToSlash(f.Path)
		got[p[strings.LastIndex(p, "skills/")+7:strings.LastIndex(p, "/")]] = f
	}
	if len(got) != 3 {
		t.Fatalf("want over, near and long; got %v", got)
	}
	if got["over"].Severity != core.Medium || got["near"].Severity != core.Medium || got["long"].Severity != core.Info {
		t.Fatalf("severities %+v", got)
	}
	if !strings.Contains(got["near"].Message, "near the 1,536 cap") {
		t.Errorf("near: %s", got["near"].Message)
	}
	for name, f := range got {
		if f.Fix == nil || f.Fix.ID != "skill-shorten-"+name || f.Tokens <= 0 {
			t.Fatalf("%s: no shorten fix: %+v", name, f)
		}
	}
	before, _ := os.ReadFile(got["over"].Path)
	after, err := got["over"].Fix.Edits[0].Change(before)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nname: over\ndescription: \"Use when asked to deploy the app.\"\n---\n# over\n\n## When to use\n\n" + strings.TrimSpace(strings.Repeat(sentence, 17)) + "\n\nsteps\n"
	if string(after) != want {
		t.Fatalf("shorten gave\n%s", after)
	}
}
