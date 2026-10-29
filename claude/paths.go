// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package claude knows where Claude Code keeps its files and how to read
// them: settings, instruction files, MCP config and session transcripts.
package claude

import (
	"os"
	"path/filepath"
	"strings"
)

// Home is Claude Code's config directory: $CLAUDE_CONFIG_DIR, else ~/.claude.
func Home() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(h, ".claude")
}

// EncodeDir turns a project path into the folder name Claude Code uses
// under projects/: every character other than a letter or digit becomes
// a dash, so /home/me/my.app is -home-me-my-app.
func EncodeDir(dir string) string {
	var b strings.Builder
	for _, r := range dir {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ProjectDir is where Claude Code keeps the transcripts and auto memory
// of the project rooted at root.
func ProjectDir(root string) string {
	return filepath.Join(Home(), "projects", EncodeDir(root))
}

// MemoryDir is the project's auto-memory folder.
func MemoryDir(root string) string {
	return filepath.Join(ProjectDir(root), "memory")
}

// Transcripts lists the project's session files, newest last.
func Transcripts(root string) []string {
	m, _ := filepath.Glob(filepath.Join(ProjectDir(root), "*.jsonl"))
	sortByMtime(m)
	return m
}

// Subagents lists the subagent transcripts of one session, at any depth
// under <session>/subagents.
func Subagents(sessionPath string) []string {
	dir := strings.TrimSuffix(sessionPath, ".jsonl")
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".jsonl") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func sortByMtime(paths []string) {
	mt := make(map[string]int64, len(paths))
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil {
			mt[p] = fi.ModTime().UnixNano()
		}
	}
	for i := 1; i < len(paths); i++ {
		for j := i; j > 0 && mt[paths[j-1]] > mt[paths[j]]; j-- {
			paths[j-1], paths[j] = paths[j], paths[j-1]
		}
	}
}
