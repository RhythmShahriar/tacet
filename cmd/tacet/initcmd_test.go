// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInNpxCache(t *testing.T) {
	cases := map[string]bool{
		"/home/me/.npm/_npx/0a1b2c3d/node_modules/@rhythmsh/tacet-linux-x64/bin/tacet": true,
		"/Users/me/.npm/_npx/9f8e/node_modules/@rhythmsh/tacet-darwin-arm64/bin/tacet": true,
		"/usr/local/lib/node_modules/@rhythmsh/tacet-linux-x64/bin/tacet":              false,
		"/home/me/.local/bin/tacet":   false,
		"/home/me/my_npx_tools/tacet": false,
		filepath.Join("C:", "Users", "me", "AppData", "Local", "npm-cache", "_npx", "ab", "tacet.exe"): true,
	}
	for bin, want := range cases {
		if got := inNpxCache(bin); got != want {
			t.Errorf("inNpxCache(%s) = %v, want %v", bin, got, want)
		}
	}
	if !strings.Contains(npxWarning, "npm i -g @rhythmsh/tacet") {
		t.Errorf("the npx warning doesn't give the global install: %q", npxWarning)
	}
}
