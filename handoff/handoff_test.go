// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package handoff

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RhythmShahriar/tacet/claude"
)

func session(id string) *claude.Session {
	return &claude.Session{ID: id, Turns: []claude.Turn{
		{Role: "user", Human: true, Text: "add a retry to the fetcher"},
		{Role: "assistant", Text: "Added retry with backoff in fetch.go.\nTests pass."},
		{Role: "user", Human: true, Text: "now make the timeout configurable"},
	}}
}

func TestWriteAndInject(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	if err := Write(root, session("aaaaaaaa-1"), now); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path(root))
	note := string(b)
	for _, want := range []string{Header, "add a retry to the fetcher", "now make the timeout configurable", "> Tests pass."} {
		if !strings.Contains(note, want) {
			t.Fatalf("missing %q in\n%s", want, note)
		}
	}
	if ForSession(root, "aaaaaaaa-1", now) != "" {
		t.Fatal("the session that wrote it doesn't need it")
	}
	if ForSession(root, "bbbbbbbb-2", now) == "" {
		t.Fatal("a new session should get it")
	}
	if ForSession(root, "bbbbbbbb-2", now.Add(Fresh+time.Hour)) != "" {
		t.Fatal("stale notes aren't injected")
	}
}

func TestKeepsHandWrittenSessionMD(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(Path(root), []byte("# my own notes\n"), 0o644)
	Write(root, session("x"), time.Now())
	b, _ := os.ReadFile(Path(root))
	if string(b) != "# my own notes\n" {
		t.Fatal("overwrote a SESSION.md tacet didn't write")
	}
	if ForSession(root, "y", time.Now()) != "" {
		t.Fatal("injected a file tacet didn't write")
	}
}

func TestNothingToHandOn(t *testing.T) {
	if Build(t.TempDir(), &claude.Session{}, time.Now()) != "" {
		t.Fatal("empty session should write nothing")
	}
}
