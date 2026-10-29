// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package update finds newer tacet releases and installs one after the
// person says yes. It never updates on its own: a release must carry a
// Sigstore signature from tacet's release workflow and every byte must
// match the signed checksums, or nothing on disk changes.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// Repo is where tacet is released.
const Repo = "RhythmShahriar/tacet"

// API is the GitHub API base for the repository.
var API = "https://api.github.com/repos/" + Repo

// LookupTimeout bounds the latest-release request.
const LookupTimeout = 2 * time.Second

// Release is one published tacet release.
type Release struct {
	Tag     string            // v1.1.0
	Version string            // 1.1.0
	Notes   string            // the release notes, as written
	Assets  map[string]string // file name to download URL
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	Draft   bool   `json:"draft"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// ErrNoRelease means GitHub had no release to offer.
var ErrNoRelease = errors.New("no release found")

// Latest asks GitHub for the newest release. tag picks one release
// instead, such as a release candidate; empty means the latest.
func Latest(ctx context.Context, client *http.Client, tag string) (*Release, error) {
	u := API + "/releases/latest"
	if tag != "" {
		u = API + "/releases/tags/" + url.PathEscape("v"+Clean(tag))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "tacet-update")
	resp, err := client.Do(req) // #nosec G107 -- the GitHub API for tacet's own repository
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNoRelease
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub: %s", resp.Status)
	}
	var gr ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&gr); err != nil {
		return nil, fmt.Errorf("GitHub release: %w", err)
	}
	if gr.Draft || !Valid(gr.TagName) || !strings.HasPrefix(gr.TagName, "v") {
		return nil, ErrNoRelease
	}
	r := &Release{Tag: gr.TagName, Version: Clean(gr.TagName), Notes: gr.Body, Assets: map[string]string{}}
	for _, a := range gr.Assets {
		r.Assets[a.Name] = a.URL
	}
	return r, nil
}

// Summary is the first lines of release notes, safe to print: markdown
// headings and terminal control characters are dropped.
func Summary(notes string, lines int) []string {
	var out []string
	for _, l := range strings.Split(notes, "\n") {
		l = strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, l))
		l = strings.TrimSpace(strings.TrimLeft(l, "#"))
		if l == "" || strings.HasPrefix(l, "**Full Changelog**") {
			continue
		}
		if r := []rune(l); len(r) > 100 {
			l = string(r[:97]) + "..."
		}
		out = append(out, l)
		if len(out) == lines {
			break
		}
	}
	return out
}
