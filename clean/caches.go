// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package clean

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OutKeep is how long a kept big tool output stays useful.
const OutKeep = 7 * 24 * time.Hour

// Cache is one file of tacet's own that can go: tacet makes it again
// when it is needed.
type Cache struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Why   string `json:"why"`
}

// Caches lists tacet's cache files under root that are safe to delete:
// big tool outputs older than a week, conversions and transcripts of
// files that no longer exist, and the per-session read records.
func Caches(root string, now time.Time) []Cache {
	dir := filepath.Join(root, ".tacet")
	var out []Cache
	add := func(p, why string) {
		if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() {
			out = append(out, Cache{Path: p, Bytes: fi.Size(), Why: why})
		}
	}
	for _, p := range files(filepath.Join(dir, "out")) {
		if fi, err := os.Stat(p); err == nil && now.Sub(fi.ModTime()) > OutKeep {
			add(p, "tool output older than 7 days")
		}
	}
	docs := files(filepath.Join(dir, "docs"))
	gone := map[string]bool{}
	for _, p := range docs {
		if strings.HasSuffix(p, ".outline.md") {
			continue
		}
		if src := source(root, p); src != "" {
			if _, err := os.Stat(src); os.IsNotExist(err) {
				gone[p] = true
				add(p, "made from a file that no longer exists")
			}
		}
	}
	for _, p := range docs {
		if md, ok := strings.CutSuffix(p, ".outline.md"); ok {
			if _, err := os.Stat(md + ".md"); gone[md+".md"] || os.IsNotExist(err) {
				add(p, "outline of a removed conversion")
			}
		}
	}
	for _, p := range files(filepath.Join(dir, "reads")) {
		add(p, "session read record")
	}
	return out
}

// RemoveCaches deletes the cache files and returns the bytes freed.
func RemoveCaches(cs []Cache) (int64, error) {
	var freed int64
	var first error
	for _, c := range cs {
		if err := os.Remove(c.Path); err != nil && !os.IsNotExist(err) {
			if first == nil {
				first = err
			}
			continue
		}
		freed += c.Bytes
	}
	return freed, first
}

// files lists the regular files directly in dir.
func files(dir string) []string {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range es {
		if e.Type().IsRegular() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// source reads the file a conversion or transcript was made from out of
// its header; "" when the header names none.
func source(root, p string) string {
	f, err := os.Open(p) // #nosec G304 -- tacet's own cache file
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	line, _ := bufio.NewReader(f).ReadString('\n')
	var rel string
	switch {
	case strings.HasPrefix(line, "<!-- tacet converted "):
		rel, _, _ = strings.Cut(strings.TrimPrefix(line, "<!-- tacet converted "), " to Markdown to save tokens.")
	case strings.HasPrefix(line, "<!-- tacet transcribed "):
		rel = strings.TrimPrefix(line, "<!-- tacet transcribed ")
		if i := strings.Index(rel, " (language "); i >= 0 {
			rel = rel[:i]
		} else {
			rel, _, _ = strings.Cut(rel, " with whisper.cpp.")
		}
	default:
		return ""
	}
	rel = strings.TrimSpace(rel)
	if rel == "" || strings.Contains(rel, "-->") {
		return ""
	}
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(root, filepath.FromSlash(rel))
}
