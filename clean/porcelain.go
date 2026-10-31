// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package clean finds what Claude Code leaves behind on disk, git
// worktrees from agents and workflows, old session transcripts and
// tacet's own caches, and removes only what holds no unsaved work.
package clean

import (
	"path/filepath"
	"strings"
)

// Entry is one block of `git worktree list --porcelain`.
type Entry struct {
	Path           string
	Head           string
	Branch         string // short name, "" when detached or bare
	Bare           bool
	Detached       bool
	Locked         bool
	LockReason     string
	Prunable       bool
	PrunableReason string
}

// ParsePorcelain reads `git worktree list --porcelain`. Blocks are
// separated by blank lines; the first is the main worktree. Lines it
// does not know are ignored, so newer git versions still parse.
func ParsePorcelain(out string) []Entry {
	var es []Entry
	var cur *Entry
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if line == "" {
			cur = nil
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		if key == "worktree" {
			es = append(es, Entry{Path: val})
			cur = &es[len(es)-1]
			continue
		}
		if cur == nil {
			continue
		}
		switch key {
		case "HEAD":
			cur.Head = val
		case "branch":
			cur.Branch = strings.TrimPrefix(val, "refs/heads/")
		case "bare":
			cur.Bare = true
		case "detached":
			cur.Detached = true
		case "locked":
			cur.Locked, cur.LockReason = true, val
		case "prunable":
			cur.Prunable, cur.PrunableReason = true, val
		}
	}
	return es
}

// agentBranchPrefixes are the branch names Claude Code gives the
// worktrees it makes: worktree-agent-<id> for agents, worktree-wf_<id>
// for workflows, and agent-<id> in older versions.
var agentBranchPrefixes = []string{"worktree-agent-", "worktree-wf_", "agent-"}

// IsAgent reports whether a worktree is one Claude Code made: it sits
// under a .claude/worktrees folder or its branch has an agent name.
func IsAgent(path, branch string) bool {
	if strings.Contains(filepath.ToSlash(path)+"/", "/.claude/worktrees/") {
		return true
	}
	return AgentBranch(branch)
}

// AgentBranch reports whether branch is named the way Claude Code names
// the branches it makes for a worktree. Only such branches are ever
// deleted; a branch an agent checked out by its own name is left alone.
func AgentBranch(branch string) bool {
	for _, p := range agentBranchPrefixes {
		if strings.HasPrefix(branch, p) {
			return true
		}
	}
	return false
}
