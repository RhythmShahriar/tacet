// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RhythmShahriar/tacet/feedback"
	"github.com/RhythmShahriar/tacet/internal/config"
)

// postEdit runs the project's checker on a file the agent just changed
// and hands back only the failures. Clean code adds nothing.
func postEdit(root string, cfg *config.Config, in *hookInput) any {
	if !cfg.On("feedback") && !cfg.On("testgate") {
		return nil
	}
	var ti toolInput
	_ = json.Unmarshal(in.ToolInput, &ti)
	if ti.FilePath == "" {
		return nil
	}
	path := ti.FilePath
	if !filepath.IsAbs(path) {
		path = filepath.Join(in.Cwd, path)
	}
	if !feedback.Source(root, path) {
		return nil
	}
	if cfg.On("testgate") {
		feedback.MarkEdit(root, in.SessionID)
	}
	if !cfg.On("feedback") {
		return nil
	}
	res := feedback.Run(context.Background(), path, feedback.Detect(root, path))
	if len(res.TimedOut) > 0 {
		logHook(root, "post-edit", fmt.Errorf("%s took over %s on %s; skipped", strings.Join(res.TimedOut, ", "), feedback.CheckTimeout, filepath.Base(path)))
	}
	if res.Text == "" {
		return nil
	}
	// decision "block" puts the reason next to the tool result; the edit
	// itself already happened.
	return map[string]any{"decision": "block", "reason": res.Text}
}

// testGate keeps the agent from finishing while the tests it broke still
// fail, at most feedback.MaxRetries times a session.
func testGate(root string, cfg *config.Config, in *hookInput) any {
	if !cfg.On("testgate") {
		return nil
	}
	reason, timedOut := feedback.Gate(context.Background(), root, in.SessionID, in.StopHookActive)
	if timedOut {
		logHook(root, "test-gate", fmt.Errorf("tests took over %s; skipped", feedback.TestTimeout))
	}
	if reason == "" {
		return nil
	}
	return map[string]any{"decision": "block", "reason": reason}
}
