// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package clean

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ReasonIgnored keeps a worktree holding git-ignored files the main
// checkout lacks or has with other content: test logins, local env files,
// scripts an agent left in an ignored folder. git status calls such a
// worktree clean, so it needs its own check.
const ReasonIgnored = "ignored files not in the main checkout"

// rebuildable are ignored paths a build or install recreates: they never
// keep a worktree. Matched against each path segment or the file name.
var rebuildable = regexp.MustCompile(`(^|/)(node_modules|\.next[^/]*|dist|build|out|\.turbo|coverage|__pycache__|target|\.venv|venv|\.cache|\.vercel|\.expo|\.pytest_cache|\.mypy_cache|\.ruff_cache|\.svelte-kit|\.nuxt|storybook-static|playwright-report|test-results|blob-report|\.gradle|\.parcel-cache|\.tacet|\.husky/_)(/|$)|(\.tsbuildinfo|next-env\.d\.ts|\.log|\.DS_Store|\.lock|\.pyc)$`)

// maxIgnoredFiles caps how many files of one ignored folder are compared.
const maxIgnoredFiles = 5000

// ignoredUnique lists the worktree's ignored files that aren't
// rebuildable and that the main checkout at root lacks or has with
// different bytes. ok is false when git couldn't list them.
func ignoredUnique(wt, root string) (files []string, ok bool) {
	out, err := git(wt, "status", "--porcelain", "--ignored", "--untracked-files=all")
	if err != nil {
		return nil, false
	}
	for _, l := range strings.Split(out, "\n") {
		rel, found := strings.CutPrefix(l, "!! ")
		if !found {
			continue
		}
		rel = strings.Trim(rel, `"`)
		if rebuildable.MatchString(strings.TrimSuffix(rel, "/")) {
			continue
		}
		p := filepath.Join(wt, filepath.FromSlash(rel))
		if !strings.HasSuffix(rel, "/") {
			if differs(p, filepath.Join(root, filepath.FromSlash(rel))) {
				files = append(files, rel)
			}
			continue
		}
		n := 0
		_ = filepath.WalkDir(p, func(f string, d fs.DirEntry, err error) error {
			if err != nil || n >= maxIgnoredFiles {
				return filepath.SkipDir
			}
			r, _ := filepath.Rel(wt, f)
			r = filepath.ToSlash(r)
			if d.IsDir() {
				if rebuildable.MatchString(r) {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || rebuildable.MatchString(r) {
				return nil
			}
			n++
			if differs(f, filepath.Join(root, filepath.FromSlash(r))) {
				files = append(files, r)
			}
			return nil
		})
	}
	return files, true
}

// differs reports whether a holds content b doesn't: b missing or with
// other bytes. Files over 64 MB compare by size only.
func differs(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil || !fa.Mode().IsRegular() {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil || fa.Size() != fb.Size() {
		return true
	}
	if fa.Size() > 64<<20 {
		return false
	}
	x, err1 := os.ReadFile(a) // #nosec G304 -- a file in a worktree tacet inspects
	y, err2 := os.ReadFile(b) // #nosec G304 -- the same path in the main checkout
	return err1 != nil || err2 != nil || !bytes.Equal(x, y)
}

// RescueDir is where --rescue copies a worktree's ignored files before
// removing it: inside the main checkout's .tacet folder, which tacet
// keeps out of git.
func RescueDir(root string, w *Worktree) string {
	return filepath.Join(root, ".tacet", "rescued", filepath.Base(w.Path))
}

// rescue copies the worktree's unique ignored files to RescueDir,
// keeping modes. Any failure stops the removal.
func (r *Repo) rescue(w *Worktree) error {
	dst := RescueDir(r.Root, w)
	for _, rel := range w.Ignored {
		src := filepath.Join(w.Path, filepath.FromSlash(rel))
		b, err := os.ReadFile(src) // #nosec G304 -- a file being rescued from a worktree
		if err != nil {
			return err
		}
		fi, err := os.Stat(src)
		if err != nil {
			return err
		}
		to := filepath.Join(dst, filepath.FromSlash(rel))
		if !within(to, dst) {
			return fmt.Errorf("%s would land outside %s", rel, dst)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(to, b, fi.Mode().Perm()); err != nil { // #nosec G703 -- checked to stay inside the rescue folder
			return err
		}
	}
	excludeTacet(r.Root)
	return nil
}

// excludeTacet adds .tacet/ to the repository's .git/info/exclude so the
// rescued files never show in git status.
func excludeTacet(root string) {
	gd, err := git(root, "rev-parse", "--git-common-dir")
	if err != nil {
		return
	}
	gd = strings.TrimSpace(gd)
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(root, gd)
	}
	p := filepath.Join(gd, "info", "exclude")
	b, _ := os.ReadFile(p) // #nosec G304 -- the repository's own exclude file
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == ".tacet/" {
			return
		}
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	b = append(b, []byte("# added by tacet\n.tacet/\n")...)
	_ = os.MkdirAll(filepath.Dir(p), 0o750)
	_ = os.WriteFile(p, b, 0o600) // #nosec G703 -- the repository's own exclude file
}
