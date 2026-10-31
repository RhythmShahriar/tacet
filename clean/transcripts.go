// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package clean

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Transcript is one old session: its .jsonl file and the folder beside
// it that holds its subagents' transcripts.
type Transcript struct {
	Path     string    `json:"path"`
	Folder   string    `json:"folder,omitempty"`
	Bytes    int64     `json:"bytes"`
	Modified time.Time `json:"modified"`
}

// OldTranscripts lists the sessions in one Claude Code project folder
// last written before now minus olderThan. The memory folder and other
// files are never listed.
func OldTranscripts(projectDir string, olderThan time.Duration, now time.Time) []Transcript {
	m, _ := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
	var out []Transcript
	for _, p := range m {
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() || now.Sub(fi.ModTime()) < olderThan {
			continue
		}
		t := Transcript{Path: p, Bytes: fi.Size(), Modified: fi.ModTime()}
		dir := strings.TrimSuffix(p, ".jsonl")
		if d, err := os.Lstat(dir); err == nil && d.IsDir() && filepath.Base(dir) != "memory" {
			s := Measure(dir, time.Minute, nil)
			if s.Newest.After(t.Modified) && now.Sub(s.Newest) < olderThan {
				continue // a subagent wrote to it lately
			}
			t.Folder, t.Bytes = dir, t.Bytes+s.Bytes+s.Shared
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.Before(out[j].Modified) })
	return out
}

// RemoveTranscripts deletes the sessions and returns the bytes freed.
func RemoveTranscripts(ts []Transcript) (int64, error) {
	var freed int64
	var first error
	for _, t := range ts {
		if t.Folder != "" {
			if err := os.RemoveAll(t.Folder); err != nil {
				first = keep(first, err)
				continue
			}
		}
		if err := os.Remove(t.Path); err != nil && !os.IsNotExist(err) {
			first = keep(first, err)
			continue
		}
		freed += t.Bytes
	}
	return freed, first
}

func keep(first, err error) error {
	if first == nil {
		return err
	}
	return first
}

// ProjectDirs lists the project folders under Claude Code's projects/.
func ProjectDirs(home string) []string {
	es, err := os.ReadDir(filepath.Join(home, "projects"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range es {
		if e.IsDir() {
			out = append(out, filepath.Join(home, "projects", e.Name()))
		}
	}
	return out
}

// Repos finds the repositories Claude Code has worked in: for each
// project folder, the cwd a transcript recorded (the folder name is a
// lossy encoding, so it is only a fallback), resolved to the main
// worktree. Each repository appears once.
func Repos(home string) []string {
	seen := map[string]bool{}
	var out []string
	for _, pd := range ProjectDirs(home) {
		dir := projectCwd(pd)
		if dir == "" {
			continue
		}
		main := mainWorktree(dir)
		if main == "" || seen[main] {
			continue
		}
		seen[main] = true
		out = append(out, main)
	}
	sort.Strings(out)
	return out
}

// projectCwd is the folder a project's sessions ran in that still
// exists: read from the newest transcripts first, then from the name.
func projectCwd(pd string) string {
	m, _ := filepath.Glob(filepath.Join(pd, "*.jsonl"))
	mt := map[string]time.Time{}
	for _, p := range m {
		if fi, err := os.Stat(p); err == nil {
			mt[p] = fi.ModTime()
		}
	}
	sort.Slice(m, func(i, j int) bool { return mt[m[i]].After(mt[m[j]]) })
	for i, p := range m {
		if i == 5 {
			break
		}
		if d := TranscriptCwd(p); d != "" && isDir(d) {
			return d
		}
	}
	// Decoding the name: every dash may have been a slash. Only a plain
	// decode that names a real folder is trusted.
	if d := "/" + strings.ReplaceAll(strings.TrimPrefix(filepath.Base(pd), "-"), "-", "/"); isDir(d) {
		return d
	}
	return ""
}

// cwdKey is how a transcript line records the session's folder.
var cwdKey = []byte(`"cwd":`)

// TranscriptCwd reads the cwd recorded in the first part of a
// transcript; "" when there is none.
func TranscriptCwd(p string) string {
	f, err := os.Open(p) // #nosec G304 -- a transcript under Claude Code's projects folder
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	b, _ := io.ReadAll(io.LimitReader(f, 256<<10))
	for {
		i := bytes.Index(b, cwdKey)
		if i < 0 {
			return ""
		}
		b = b[i+len(cwdKey):]
		var s string
		dec := json.NewDecoder(bytes.NewReader(b))
		if dec.Decode(&s) == nil && filepath.IsAbs(s) {
			return filepath.Clean(s)
		}
	}
}

// mainWorktree is the main worktree of the repository dir is in; "" when
// dir is in none.
func mainWorktree(dir string) string {
	out, err := git(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	es := ParsePorcelain(out)
	if len(es) == 0 || !isDir(es[0].Path) {
		return ""
	}
	return filepath.Clean(es[0].Path)
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
