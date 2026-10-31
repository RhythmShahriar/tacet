// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package clean

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePorcelain(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Entry
	}{
		{"empty", "", nil},
		{"main only", "worktree /r\nHEAD abc\nbranch refs/heads/main\n", []Entry{{Path: "/r", Head: "abc", Branch: "main"}}},
		{"bare", "worktree /r.git\nbare\n", []Entry{{Path: "/r.git", Bare: true}}},
		{
			"agent, detached, locked, prunable",
			"worktree /r\nHEAD a\nbranch refs/heads/main\n\n" +
				"worktree /r/.claude/worktrees/agent-1\nHEAD b\nbranch refs/heads/worktree-agent-1\n\n" +
				"worktree /r/w2\nHEAD c\ndetached\nlocked moved to a usb disk\n\n" +
				"worktree /gone\nHEAD d\nbranch refs/heads/feat/x\nlocked\nprunable gitdir file points to non-existent location\n\n",
			[]Entry{
				{Path: "/r", Head: "a", Branch: "main"},
				{Path: "/r/.claude/worktrees/agent-1", Head: "b", Branch: "worktree-agent-1"},
				{Path: "/r/w2", Head: "c", Detached: true, Locked: true, LockReason: "moved to a usb disk"},
				{Path: "/gone", Head: "d", Branch: "feat/x", Locked: true, Prunable: true, PrunableReason: "gitdir file points to non-existent location"},
			},
		},
		{"crlf and unknown lines", "worktree /r\r\nHEAD a\r\nfuture-field yes\r\n\r\n", []Entry{{Path: "/r", Head: "a"}}},
		{"path with spaces", "worktree /my repo/x y\nHEAD a\n", []Entry{{Path: "/my repo/x y", Head: "a"}}},
		{"lines before any worktree", "HEAD a\nbranch refs/heads/x\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParsePorcelain(c.in); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestIsAgent(t *testing.T) {
	cases := []struct {
		path, branch string
		want         bool
	}{
		{"/r/.claude/worktrees/agent-a1", "worktree-agent-a1", true},
		{"/r/.claude/worktrees/agent-a1", "feat/excuse-late", true}, // an agent checked out a branch
		{"/r/.claude/worktrees/wf", "worktree-wf_9ece55de-1f0-8", true},
		{"/tmp/x", "agent-abc", true},
		{"/r-wt-keys", "wt/keys", false},
		{"/r/claude/worktrees/x", "main", false},
		{"/r", "main", false},
	}
	for _, c := range cases {
		if got := IsAgent(c.path, c.branch); got != c.want {
			t.Errorf("IsAgent(%q, %q) = %v, want %v", c.path, c.branch, got, c.want)
		}
	}
	if AgentBranch("feat/excuse-late") || !AgentBranch("worktree-agent-x") {
		t.Error("AgentBranch")
	}
}

// FuzzParsePorcelain: any input parses without a panic, every entry
// has the path of a worktree line, and a rendered entry reads back.
func FuzzParsePorcelain(f *testing.F) {
	f.Add("worktree /r\nHEAD a\nbranch refs/heads/main\n\nworktree /r/w\ndetached\nlocked why\nprunable gone\n")
	f.Add("")
	f.Add("\n\n\nworktree \n")
	f.Fuzz(func(t *testing.T, in string) {
		es := ParsePorcelain(in)
		n := 0
		for _, l := range strings.Split(strings.ReplaceAll(in, "\r\n", "\n"), "\n") {
			if k, _, _ := strings.Cut(l, " "); k == "worktree" {
				n++
			}
		}
		if len(es) != n {
			t.Fatalf("%d entries for %d worktree lines", len(es), n)
		}
		for _, e := range es {
			if strings.ContainsAny(e.Path+e.Branch+e.Head, "\n") {
				t.Fatalf("newline in a field: %+v", e)
			}
			if e.Path == "" || strings.ContainsAny(e.Path, "\r") || strings.ContainsAny(e.Branch+e.Head, " \r") || e.Bare || e.Detached {
				continue
			}
			out := "worktree " + e.Path + "\n"
			if e.Head != "" {
				out += "HEAD " + e.Head + "\n"
			}
			if e.Branch != "" {
				out += "branch refs/heads/" + e.Branch + "\n"
			}
			back := ParsePorcelain(out)
			if len(back) != 1 || back[0].Path != e.Path || back[0].Branch != e.Branch || back[0].Head != e.Head {
				t.Fatalf("round trip of %+v gave %+v", e, back)
			}
		}
	})
}
