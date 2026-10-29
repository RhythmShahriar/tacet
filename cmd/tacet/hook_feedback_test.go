// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/internal/config"
)

func TestPostEditAndTestGateOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake tools are shell scripts")
	}
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "ruff"), []byte("#!/bin/sh\necho 'app.py:1:8: F401 `os` imported but unused'\nexit 1\n"), 0o755)
	os.WriteFile(filepath.Join(bin, "pytest"), []byte("#!/bin/sh\necho 'FAILED test_app.py::test_x - assert 1 == 2'\nexit 1\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname = \"x\"\n"), 0o644)
	os.WriteFile(filepath.Join(root, "app.py"), []byte("import os\n"), 0o644)

	cfg := config.Load(root)
	if cfg.On("feedback") || cfg.On("testgate") {
		t.Fatal("feedback and testgate must default to off")
	}
	in := &hookInput{Event: "PostToolUse", SessionID: "s", Cwd: root, ToolName: "Edit", ToolInput: json.RawMessage(`{"file_path":"app.py"}`)}
	if out, _ := handle(root, cfg, "post-edit", in); out != nil {
		t.Fatalf("feature off produced %v", out)
	}
	if out, _ := handle(root, cfg, "test-gate", &hookInput{SessionID: "s"}); out != nil {
		t.Fatalf("testgate off produced %v", out)
	}

	cfg.Features["feedback"], cfg.Features["testgate"] = true, true
	out, _ := handle(root, cfg, "post-edit", in)
	b, _ := json.Marshal(out)
	var got struct{ Decision, Reason string }
	_ = json.Unmarshal(b, &got)
	if got.Decision != "block" || !strings.Contains(got.Reason, "F401") {
		t.Fatalf("post-edit output: %s", b)
	}
	md := &hookInput{SessionID: "s", Cwd: root, ToolInput: json.RawMessage(`{"file_path":"notes.md"}`)}
	if out, _ := handle(root, cfg, "post-edit", md); out != nil {
		t.Fatalf("non-source edit produced %v", out)
	}

	out, _ = handle(root, cfg, "test-gate", &hookInput{SessionID: "s"})
	b, _ = json.Marshal(out)
	_ = json.Unmarshal(b, &got)
	if got.Decision != "block" || !strings.Contains(got.Reason, "FAILED test_app.py::test_x") {
		t.Fatalf("test-gate output: %s", b)
	}
}
