// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package handoff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// snapTranscript has two asks, edits, a failing test run, a fixed one
// and a todo list that changed once.
func snapTranscript(root string) string {
	lines := []string{
		`{"type":"user","sessionId":"abcdef1234","message":{"role":"user","content":"Add retries to the uploader"}}`,
		`{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"tool_use","name":"TodoWrite","input":{"todos":[{"content":"write backoff","status":"pending"}]}}]}}`,
		`{"type":"assistant","message":{"id":"m2","role":"assistant","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"` + filepath.ToSlash(filepath.Join(root, "up", "upload.go")) + `"}}]}}`,
		`{"type":"assistant","message":{"id":"m3","role":"assistant","content":[{"type":"tool_use","name":"Write","input":{"file_path":"/elsewhere/notes.txt"}},{"type":"tool_use","name":"Read","input":{"file_path":"/x/read-only.go"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","is_error":true,"content":"--- FAIL: TestRetry\n    upload_test.go:40: want 3 tries, got 1"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"type":"assistant","message":{"id":"m4","role":"assistant","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"` + filepath.ToSlash(filepath.Join(root, "up", "upload.go")) + `"}}]}}`,
		`{"type":"assistant","message":{"id":"m5","role":"assistant","content":[{"type":"tool_use","name":"TodoWrite","input":{"todos":[{"content":"write backoff","status":"completed"},{"content":"cap the delay","status":"in_progress"},{"content":"document it","status":"pending"}]}}]}}`,
		`{"type":"user","message":{"role":"user","content":"also cap the delay at 30s"}}`,
	}
	return strings.Join(lines, "\n") + "\n"
}

func TestSnapshotFromTranscript(t *testing.T) {
	root := t.TempDir()
	tp := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(tp, []byte(snapTranscript(root)), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := ReadSnapshot(root, tp)
	if err != nil {
		t.Fatal(err)
	}
	if s.Goal != "Add retries to the uploader" || s.Latest != "also cap the delay at 30s" {
		t.Errorf("goal %q latest %q", s.Goal, s.Latest)
	}
	if strings.Join(s.Edited, ",") != "up/upload.go,/elsewhere/notes.txt" {
		t.Errorf("edited %v", s.Edited)
	}
	if !strings.HasPrefix(s.LastError, "--- FAIL: TestRetry") {
		t.Errorf("last error %q", s.LastError)
	}
	if strings.Join(s.Todos, "|") != "[~] cap the delay|[ ] document it" {
		t.Errorf("todos %v", s.Todos)
	}
	if s.SessionID != "abcdef1234" {
		t.Errorf("session %q", s.SessionID)
	}
	out := s.Render(time.Now())
	for _, want := range []string{snapStart, snapEnd, "Session `abcdef12`", "Goal: Add retries", "`up/upload.go`", "> --- FAIL: TestRetry", "- [~] cap the delay"} {
		if !strings.Contains(out, want) {
			t.Errorf("snapshot lacks %q:\n%s", want, out)
		}
	}
}

func TestSnapshotEmptyAndCapped(t *testing.T) {
	s, err := ParseSnapshot("", strings.NewReader("not json\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Render(time.Now()) != "" || s.Todos != nil {
		t.Errorf("an empty transcript gives no snapshot: %+v", s)
	}
	big := &Snapshot{SessionID: "x", Goal: strings.Repeat("goal ", 500), LastError: strings.Repeat(strings.Repeat("e", 300)+"\n", 50)}
	for i := 0; i < 200; i++ {
		big.Edited = append(big.Edited, strings.Repeat("d/", 20)+"file.go")
		big.Todos = append(big.Todos, "[ ] "+strings.Repeat("t", 200))
	}
	out := big.Render(time.Now())
	if len(out) > MaxSnapshot {
		t.Errorf("snapshot is %d bytes, over %d", len(out), MaxSnapshot)
	}
	if !strings.HasSuffix(out, snapEnd+"\n") {
		t.Errorf("snapshot lost its end marker:\n%s", out)
	}
}

func TestWriteSnapshotAndSnapshotFor(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	s := &Snapshot{SessionID: "abcdef1234", Goal: "ship it", Edited: []string{"a.go"}}
	if err := WriteSnapshot(root, s.Render(now)); err != nil {
		t.Fatal(err)
	}
	s.Goal = "ship it again"
	if err := WriteSnapshot(root, s.Render(now)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path(root))
	if strings.Count(string(b), snapStart) != 1 || !strings.HasPrefix(string(b), Header) {
		t.Fatalf("the newer snapshot replaces the older one:\n%s", b)
	}
	got := SnapshotFor(root, "abcdef1234", now)
	if !strings.Contains(got, "Goal: ship it again") {
		t.Errorf("snapshot for the session: %q", got)
	}
	if SnapshotFor(root, "zzzzzzzz99", now) != "" {
		t.Error("another session's snapshot was returned")
	}
	if SnapshotFor(root, "abcdef1234", now.Add(SnapshotFresh+time.Minute)) != "" {
		t.Error("a stale snapshot was returned")
	}

	// A SESSION.md tacet didn't write is left alone.
	own := t.TempDir()
	if err := os.WriteFile(Path(own), []byte("# my notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteSnapshot(own, s.Render(now)); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(Path(own)); string(b) != "# my notes\n" {
		t.Errorf("rewrote the person's SESSION.md:\n%s", b)
	}
}
