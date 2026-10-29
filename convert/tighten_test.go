// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package convert

import (
	"strings"
	"testing"
)

func TestTighten(t *testing.T) {
	var in strings.Builder
	for p := 1; p <= 5; p++ {
		in.WriteString("`ACME Spec v2`\n")
		if p == 3 {
			in.WriteString("`The retry policy backs off expo-`\n`nentially and gives up after five`\n`tries.`\n\n")
		} else {
			in.WriteString("`Section " + string(rune('0'+p)) + " talks about something else entirely.`\n\n")
		}
		in.WriteString("- keep this item\n- and this one\n\n")
		in.WriteString("```\ncode   stays\n```\n")
		in.WriteString("`" + strings.Repeat("x", 3) + "`\n")
		in.WriteString(string(rune('0'+p)) + "\n\n")
	}
	got := Tighten(in.String())
	for _, bad := range []string{"ACME Spec v2", "\n3\n", "expo-"} {
		if strings.Contains(got, bad) {
			t.Errorf("should be gone: %q\n%s", bad, got)
		}
	}
	for _, want := range []string{"The retry policy backs off exponentially and gives up after five tries.", "- keep this item\n- and this one", "```\ncode   stays\n```"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q\n%s", want, got)
		}
	}
}

func TestTightenKeepsRealInlineCode(t *testing.T) {
	in := "Run the tests first.\n`make check`\nThen push the branch and open the pull request.\nAsk for review.\n"
	if got := Tighten(in); !strings.Contains(got, "`make check`") {
		t.Fatalf("inline code lost:\n%s", got)
	}
}

func TestTightenEscapedHeadings(t *testing.T) {
	got := Tighten("\\## 10. Exit codes\nbody\n\\#notaheading\n")
	if !strings.HasPrefix(got, "## 10. Exit codes\n") {
		t.Fatalf("got %q", got)
	}
}

func TestTightenMergesHeadingFragments(t *testing.T) {
	in := "**Implementer note: the deployment is code-named HARBOR**\n\n#### LANTERN-17.\n\nNext paragraph.\n" +
		"The flow is described in\n\n#### Section 4.\n\n## 4.3 Real heading\nbody\n"
	got := Tighten(in)
	if !strings.Contains(got, "code-named HARBOR-LANTERN-17.**") {
		t.Fatalf("code name not rejoined:\n%s", got)
	}
	if !strings.Contains(got, "The flow is described in Section 4.") {
		t.Fatalf("plain fragment not rejoined:\n%s", got)
	}
	if !strings.Contains(got, "## 4.3 Real heading") {
		t.Fatalf("a real heading was merged:\n%s", got)
	}
}

func TestTightenKeepsHeadingsAfterFinishedLines(t *testing.T) {
	in := "A finished sentence.\n\n## Overview.\nbody\n"
	if got := Tighten(in); !strings.Contains(got, "## Overview.") {
		t.Fatalf("heading after a finished line was merged:\n%s", got)
	}
}
