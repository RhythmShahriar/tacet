// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"time"

	"github.com/RhythmShahriar/tacet/handoff"
	"github.com/RhythmShahriar/tacet/internal/config"
)

// preCompact writes SESSION.md and then the compaction snapshot into it:
// goal, files edited, last error and open todos, at most 2 KB.
func preCompact(root string, cfg *config.Config, in *hookInput) error {
	err := writeHandoff(root, cfg, in)
	if !cfg.On("handoff") || in.TranscriptPath == "" {
		return err
	}
	snap, serr := handoff.ReadSnapshot(root, in.TranscriptPath)
	if serr != nil {
		return serr
	}
	if snap.SessionID == "" {
		snap.SessionID = in.SessionID
	}
	_ = config.ExcludePath(root, handoff.File)
	if werr := handoff.WriteSnapshot(root, snap.Render(time.Now())); werr != nil {
		return werr
	}
	return err
}

// compactContext is the snapshot to hand back right after a compaction,
// since the summary may have dropped those details.
func compactContext(root string, cfg *config.Config, in *hookInput) string {
	if in.Source != "compact" || !cfg.On("handoff") {
		return ""
	}
	snap := handoff.SnapshotFor(root, in.SessionID, time.Now())
	if snap == "" {
		return ""
	}
	return "tacet kept these details from before the compaction (SESSION.md):\n\n" + snap
}
