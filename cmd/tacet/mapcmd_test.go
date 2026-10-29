// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/internal/config"
)

func mapProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"core/core.go": "package core\n\n// Engine runs things.\nfunc Engine() int { return 1 }\n",
		"a/a.go":       "package a\n\nfunc A() int { return core.Engine() }\n",
		"b/b.go":       "package b\n\nfunc B() int { return core.Engine() }\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestMapCommand(t *testing.T) {
	dir := mapProject(t)
	var out, errw bytes.Buffer
	if c := run([]string{"-C", dir, "map", "--budget", "200"}, &out, &errw); c != exitOK {
		t.Fatalf("exit %d: %s", c, errw.String())
	}
	if !strings.HasPrefix(out.String(), "core/core.go\n") || !strings.Contains(out.String(), "func Engine() int") {
		t.Errorf("map:\n%s", out.String())
	}

	out.Reset()
	if c := run([]string{"-C", dir, "--json", "map", "--dir", "a"}, &out, &errw); c != exitOK {
		t.Fatalf("exit %d: %s", c, errw.String())
	}
	var res struct {
		Index struct{ Files, Parsed int }
		Files []struct{ Path string }
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	if res.Index.Files != 3 || res.Index.Parsed != 0 || len(res.Files) != 1 || res.Files[0].Path != "a/a.go" {
		t.Errorf("json map: %+v", res)
	}

	out.Reset()
	if c := run([]string{"-C", dir, "map", "--update"}, &out, &errw); c != exitOK || out.Len() != 0 {
		t.Errorf("--update: exit %d, output %q", c, out.String())
	}
}

func TestMapCommandBadArgs(t *testing.T) {
	dir := mapProject(t)
	for _, args := range [][]string{{"--budget"}, {"--budget", "x"}, {"--budget", "-5"}, {"--dir"}, {"--what"}} {
		var out, errw bytes.Buffer
		if c := run(append([]string{"-C", dir, "map"}, args...), &out, &errw); c != exitUsage {
			t.Errorf("%v: exit %d, want %d", args, c, exitUsage)
		}
	}
}

func TestMapCommandWithoutCode(t *testing.T) {
	var out, errw bytes.Buffer
	if c := run([]string{"-C", t.TempDir(), "map"}, &out, &errw); c != exitOK || !strings.Contains(out.String(), "no code") {
		t.Errorf("exit %d: %q", c, out.String())
	}
}

func TestSessionStartRefreshesMapOnlyWhenOn(t *testing.T) {
	var got []string
	old := refreshMap
	refreshMap = func(root string) { got = append(got, root) }
	t.Cleanup(func() { refreshMap = old })

	root := t.TempDir()
	cfg := config.Load(root)
	sessionStart(root, cfg, &hookInput{})
	if len(got) != 1 || got[0] != root {
		t.Errorf("map on: refreshed %v", got)
	}
	got = nil
	_ = cfg.Set("map", false)
	sessionStart(root, cfg, &hookInput{})
	if len(got) != 0 {
		t.Errorf("map off: refreshed %v", got)
	}
}
