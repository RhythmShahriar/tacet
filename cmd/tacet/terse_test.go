// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/RhythmShahriar/tacet/internal/config"
)

func terseSettings(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

func terseRun(t *testing.T, root string, args ...string) {
	t.Helper()
	var out bytes.Buffer
	a := &app{out: &out, err: &out, dir: root}
	var err error
	switch args[0] {
	case "on":
		err = runOn(a, args[1:])
	case "off":
		err = runOff(a, args[1:])
	case "undo":
		err = runUndo(a, nil)
	}
	if err != nil {
		t.Fatalf("tacet %v: %v\n%s", args, err, out.String())
	}
}

func TestTerseOnOffRestoresPrevious(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	before := "{\n  \"model\": \"haiku\",\n  \"outputStyle\": \"Explanatory\"\n}\n"
	p := filepath.Join(root, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}

	terseRun(t, root, "on", "terse")
	if got, want := terseSettings(t, root), "{\n  \"model\": \"haiku\",\n  \"outputStyle\": \"tacet:terse\"\n}\n"; got != want {
		t.Fatalf("on terse:\n%s\nwant\n%s", got, want)
	}
	if !config.Load(root).On("terse") {
		t.Fatal("terse is not on in the config")
	}

	// Switching to the other style keeps the original previous value.
	terseRun(t, root, "on", "lesscode")
	c := config.Load(root)
	if !c.On("lesscode") || c.On("terse") {
		t.Fatalf("features %v", c.Features)
	}
	terseRun(t, root, "off", "lesscode")
	if got := terseSettings(t, root); got != before {
		t.Fatalf("off restored:\n%s\nwant\n%s", got, before)
	}
	if _, err := os.Stat(styleStatePath(root)); !os.IsNotExist(err) {
		t.Fatalf("state left behind: %v", err)
	}
	if config.Load(root).On("lesscode") {
		t.Fatal("lesscode still on")
	}
}

func TestTerseOffWithoutPrevious(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	terseRun(t, root, "on", "terse")
	if got := terseSettings(t, root); got != "{\n  \"outputStyle\": \"tacet:terse\"\n}\n" {
		t.Fatalf("on terse: %s", got)
	}
	terseRun(t, root, "off", "terse")
	if got := terseSettings(t, root); got != "{}\n" {
		t.Fatalf("off terse: %q", got)
	}
}

func TestTerseUndo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	terseRun(t, root, "on", "terse")
	terseRun(t, root, "undo")
	if got := terseSettings(t, root); got != "" {
		t.Fatalf("undo left settings %q", got)
	}
	if config.Load(root).On("terse") {
		t.Fatal("undo left terse on")
	}
	if _, err := os.Stat(styleStatePath(root)); !os.IsNotExist(err) {
		t.Fatalf("undo left state: %v", err)
	}
}

// A style the person picked after tacet's is theirs: off leaves it.
func TestTerseOffKeepsOtherStyle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	terseRun(t, root, "on", "terse")
	p := filepath.Join(root, ".claude", "settings.local.json")
	if err := os.WriteFile(p, []byte("{\"outputStyle\": \"Concise\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	terseRun(t, root, "off", "terse")
	if got := terseSettings(t, root); got != "{\"outputStyle\": \"Concise\"}\n" {
		t.Fatalf("off changed a style it did not set: %q", got)
	}
}
