// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/handoff"
	"github.com/RhythmShahriar/tacet/internal/config"
)

func TestPreCompactSnapshotComesBackAfterCompaction(t *testing.T) {
	root := t.TempDir()
	cfg := config.Load(root)
	tp := filepath.Join(t.TempDir(), "s.jsonl")
	lines := `{"type":"user","sessionId":"sess12345678","message":{"role":"user","content":"Fix the flaky upload test"}}
{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"Looking."},{"type":"tool_use","name":"Edit","input":{"file_path":"` + filepath.ToSlash(filepath.Join(root, "up.go")) + `"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","is_error":true,"content":"exit status 1: FAIL TestUpload"}]}}
`
	if err := os.WriteFile(tp, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	in := &hookInput{SessionID: "sess12345678", TranscriptPath: tp, Cwd: root}
	if _, err := handle(root, cfg, "pre-compact", in); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(handoff.Path(root))
	if !strings.Contains(string(b), "## Before compaction") {
		t.Fatalf("SESSION.md has no snapshot:\n%s", b)
	}

	startup := sessionStart(root, cfg, &hookInput{SessionID: "sess12345678", Source: "startup"})
	if s, _ := json.Marshal(startup); strings.Contains(string(s), "Before compaction") {
		t.Errorf("snapshot injected on startup: %s", s)
	}
	out := sessionStart(root, cfg, &hookInput{SessionID: "sess12345678", Source: "compact"})
	ctx := out.(map[string]any)["hookSpecificOutput"].(map[string]any)["additionalContext"].(string)
	for _, want := range []string{"Fix the flaky upload test", "`up.go`", "FAIL TestUpload"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("context lacks %q:\n%s", want, ctx)
		}
	}
	if len(ctx) > handoff.MaxSnapshot+200 {
		t.Errorf("context is %d bytes", len(ctx))
	}
}
