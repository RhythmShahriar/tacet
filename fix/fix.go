// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package fix applies the edits a fix makes and keeps what it needs to
// undo them: every change is backed up under .tacet/backup first.
package fix

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/RhythmShahriar/tacet/core"
)

// Change is one file a run touched.
type Change struct {
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
	Backup  string `json:"backup,omitempty"`
}

// Run is one apply, written as .tacet/backup/<id>/run.json.
type Run struct {
	ID      string   `json:"id"`
	Time    string   `json:"time"`
	Fixes   []string `json:"fixes"`
	Changes []Change `json:"changes"`
}

// ErrChanged means a file changed since the fix was planned.
var ErrChanged = errors.New("file changed since tacet read it")

// Apply writes every fix's edits under one backup run. Nothing is
// written when any edit fails to plan.
func Apply(root string, fixes []*core.Fix) (run *Run, err error) {
	id := time.Now().UTC().Format("20060102T150405.000Z")
	dir := filepath.Join(root, ".tacet", "backup", id)
	run = &Run{ID: id, Time: time.Now().UTC().Format(time.RFC3339)}
	planned := false
	defer func() {
		if err != nil && !planned {
			_ = os.RemoveAll(dir)
		}
	}()
	// Each file's bytes as the fixes so far leave it.
	cur := map[string][]byte{}
	for _, f := range fixes {
		run.Fixes = append(run.Fixes, f.ID)
		for _, e := range f.Edits {
			base, seen := cur[e.Path]
			if !seen {
				b, err := os.ReadFile(e.Path) // #nosec G304 -- a file a fix planned to edit
				if err == nil {
					base = b
				}
				ch := Change{Path: e.Path, Existed: err == nil}
				if ch.Existed {
					ch.Backup = filepath.Join(dir, fmt.Sprintf("%d", len(run.Changes)))
					if err := write(ch.Backup, b); err != nil {
						return run, err
					}
				}
				run.Changes = append(run.Changes, ch)
			}
			var next []byte
			if e.Change != nil {
				n, err := e.Change(base)
				if err != nil {
					return run, fmt.Errorf("%s: %w", e.Path, err)
				}
				next = n
			} else {
				if !bytes.Equal(base, e.Old) {
					return run, fmt.Errorf("%s: %w", e.Path, ErrChanged)
				}
				next = e.New
			}
			cur[e.Path] = next
		}
	}
	if err := writeJSON(filepath.Join(dir, "run.json"), run); err != nil {
		return run, err
	}
	planned = true
	paths := make([]string, 0, len(cur))
	for p := range cur {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if cur[p] == nil {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return run, err
			}
			continue
		}
		if err := write(p, cur[p]); err != nil {
			return run, err
		}
	}
	return run, nil
}

// Undo restores the newest run and removes its backup.
func Undo(root string) (*Run, error) {
	base := filepath.Join(root, ".tacet", "backup")
	ents, err := os.ReadDir(base)
	if err != nil || len(ents) == 0 {
		return nil, errors.New("nothing to undo")
	}
	last := ents[len(ents)-1].Name()
	b, err := os.ReadFile(filepath.Join(base, last, "run.json")) // #nosec G304 -- tacet's own backup
	if err != nil {
		return nil, err
	}
	var run Run
	if err := json.Unmarshal(b, &run); err != nil {
		return nil, err
	}
	for _, c := range run.Changes {
		if !c.Existed {
			if err := os.Remove(c.Path); err != nil && !os.IsNotExist(err) {
				return &run, err
			}
			continue
		}
		old, err := os.ReadFile(c.Backup) // #nosec G304 -- tacet's own backup
		if err != nil {
			return &run, err
		}
		if err := write(c.Path, old); err != nil {
			return &run, err
		}
	}
	return &run, os.RemoveAll(filepath.Join(base, last))
}

func write(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".tacet-tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil { // #nosec G703 -- paths come from tacet's own fix plans
		return err
	}
	if fi, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmp, fi.Mode().Perm())
	}
	return os.Rename(tmp, path)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return write(path, append(b, '\n'))
}

// JSONEdit plans an edit of a JSON file through change, keeping the
// keys it doesn't touch. A missing file starts as {}.
func JSONEdit(path string, change func(m map[string]any)) core.Edit {
	return core.Edit{Path: path, Change: func(cur []byte) ([]byte, error) {
		m := map[string]any{}
		if len(bytes.TrimSpace(cur)) > 0 {
			if err := json.Unmarshal(cur, &m); err != nil {
				return nil, err
			}
		}
		change(m)
		b, err := json.MarshalIndent(m, "", "  ")
		return append(b, '\n'), err
	}}
}

// Preview runs an edit against the file as it is now, for diffs and
// token deltas before anything is written.
func Preview(e core.Edit) (old, next []byte, err error) {
	old, rerr := os.ReadFile(e.Path) // #nosec G304 -- a file a fix planned to edit
	if rerr != nil {
		old = nil
	}
	if e.Change == nil {
		return old, e.New, nil
	}
	next, err = e.Change(old)
	return old, next, err
}

// AddDeny returns a settings edit that appends rules to permissions.deny,
// skipping ones already there.
func AddDeny(path string, rules ...string) core.Edit {
	return JSONEdit(path, func(m map[string]any) {
		p, _ := m["permissions"].(map[string]any)
		if p == nil {
			p = map[string]any{}
			m["permissions"] = p
		}
		have := map[string]bool{}
		deny, _ := p["deny"].([]any)
		for _, r := range deny {
			if s, ok := r.(string); ok {
				have[s] = true
			}
		}
		for _, r := range rules {
			if !have[r] {
				deny = append(deny, r)
				have[r] = true
			}
		}
		p["deny"] = deny
	})
}
