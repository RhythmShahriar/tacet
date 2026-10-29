// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package convert

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxTracked caps how many tracked documents a session start lists.
const MaxTracked = 15

// Tracked lists the documents in root's git index that Claude Code
// refuses to read (Word, PowerPoint, Excel, EPUB), at most MaxTracked
// and none over MaxSize. Paths are absolute. Outside git it returns nil.
func Tracked(root string) []string {
	args := []string{"-C", root, "ls-files", "-z", "--"}
	for e := range Exts {
		if e == ".pdf" {
			continue
		}
		// Both cases: git pathspecs are case sensitive.
		args = append(args, "*"+e, "*"+strings.ToUpper(e))
	}
	out, err := exec.Command("git", args...).Output() // #nosec G204 -- fixed git subcommand
	if err != nil {
		return nil
	}
	rels := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	sort.Strings(rels)
	var docs []string
	for _, rel := range rels {
		if rel == "" || !Binary(rel) {
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(rel))
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() || fi.Size() > MaxSize {
			continue
		}
		docs = append(docs, p)
		if len(docs) == MaxTracked {
			break
		}
	}
	return docs
}

// Lookup returns the earlier conversion of path without converting it:
// nil when there is none yet or the document had no text.
func Lookup(root, path string) *Result {
	sum, err := Sum(root, path)
	if err != nil {
		return nil
	}
	r, _ := cached(path, CachePath(root, path, sum, ".md"))
	return r
}

// indexEntry remembers a file's hash for its size and modification time,
// so a session start doesn't hash every document again.
type indexEntry struct {
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
	Sum   string `json:"sum"`
}

var indexMu sync.Mutex

func indexPath(root string) string { return filepath.Join(Dir(root), "index.json") }

// Sum is the SHA-256 of path's bytes in hex, read from the index when
// the file hasn't changed since it was last hashed.
func Sum(root, path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	key := path
	if r, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(r, "..") {
		key = filepath.ToSlash(r)
	}
	indexMu.Lock()
	defer indexMu.Unlock()
	idx := map[string]indexEntry{}
	if b, err := os.ReadFile(indexPath(root)); err == nil { // #nosec G304 -- tacet's own cache
		_ = json.Unmarshal(b, &idx)
	}
	if e, ok := idx[key]; ok && e.Size == fi.Size() && e.MTime == fi.ModTime().UnixNano() && len(e.Sum) == 64 {
		return e.Sum, nil
	}
	sum, err := hashFile(path)
	if err != nil {
		return "", err
	}
	idx[key] = indexEntry{Size: fi.Size(), MTime: fi.ModTime().UnixNano(), Sum: sum}
	if b, err := json.Marshal(idx); err == nil && os.MkdirAll(Dir(root), 0o750) == nil {
		tmp := fmt.Sprintf("%s.%d.tmp", indexPath(root), os.Getpid())
		if os.WriteFile(tmp, b, 0o600) == nil {
			_ = os.Rename(tmp, indexPath(root))
		}
	}
	return sum, nil
}

// InlineBytes is how much unconverted text a session start converts
// itself; more than that goes to a background process so the session
// isn't held up.
const InlineBytes = 4 << 20

// Pending splits docs into those already converted and those still to
// convert, plus the total size of the latter.
func Pending(root string, docs []string) (done []*Result, todo []string, size int64) {
	for _, p := range docs {
		sum, err := Sum(root, p)
		if err != nil {
			continue
		}
		out := CachePath(root, p, sum, ".md")
		r, err := cached(p, out)
		switch {
		case r != nil:
			done = append(done, r)
		case err != nil:
			// converted before and found to be a scan: nothing to list
		default:
			todo = append(todo, p)
			if fi, err := os.Stat(p); err == nil {
				size += fi.Size()
			}
		}
	}
	return done, todo, size
}

// BusyFile marks a background conversion in progress.
func BusyFile(root string) string { return filepath.Join(Dir(root), ".converting") }

// Busy reports whether a background conversion started in the last ten
// minutes is still marked as running.
func Busy(root string) bool {
	fi, err := os.Stat(BusyFile(root))
	return err == nil && time.Since(fi.ModTime()) < 10*time.Minute
}

// Line is the one line a session start shows for a converted document:
// where it is and which file to read instead, relative to root.
func Line(root string, r *Result) string {
	rel := func(p string) string {
		if x, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(x, "..") {
			return filepath.ToSlash(x)
		}
		return p
	}
	if r.Outline != "" {
		return rel(r.Source) + " → read " + rel(r.Outline) + " (or the .md)"
	}
	return rel(r.Source) + " → read " + rel(r.Path)
}
