// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/audio"
	"github.com/RhythmShahriar/tacet/convert"
	"github.com/RhythmShahriar/tacet/internal/config"
)

// sessionDocs lists the Office files tracked in the repo, each with the
// Markdown file to read instead, because Claude Code refuses them before
// any hook can step in. Small batches are converted here; bigger ones in
// a background process, listed from the next session on.
func sessionDocs(root string, cfg *config.Config) string {
	if !cfg.On("convert") {
		return ""
	}
	docs := convert.Tracked(root)
	if len(docs) == 0 {
		return ""
	}
	done, todo, size := convert.Pending(root, docs)
	if len(todo) > 0 {
		if size <= convert.InlineBytes {
			for _, p := range todo {
				if r, err := convert.File(root, p); err == nil {
					done = append(done, r)
				}
			}
		} else if !convert.Busy(root) {
			spawn(root, "hook", "background-convert")
		}
	}
	return docLines(root, done)
}

// docLines is the session start text for converted documents: one
// sentence, then one line each.
func docLines(root string, done []*convert.Result) string {
	if len(done) == 0 {
		return ""
	}
	lines := []string{"These files can't be opened with Read; tacet keeps Markdown copies:"}
	for _, r := range done {
		lines = append(lines, convert.Line(root, r))
	}
	return strings.Join(lines, "\n")
}

// backgroundConvert converts the tracked documents a session start left.
func backgroundConvert(root string) {
	if err := os.MkdirAll(convert.Dir(root), 0o750); err != nil {
		return
	}
	if os.WriteFile(convert.BusyFile(root), nil, 0o600) != nil {
		return
	}
	defer func() { _ = os.Remove(convert.BusyFile(root)) }()
	_, todo, _ := convert.Pending(root, convert.Tracked(root))
	for _, p := range todo {
		if _, err := convert.File(root, p); err != nil {
			logHook(root, "background-convert", fmt.Errorf("%s: %w", filepath.Base(p), err))
		}
	}
}

// spawn starts tacet in the background on root, outliving the hook.
func spawn(root string, args ...string) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, append([]string{"-C", root}, args...)...) // #nosec G204 -- tacet itself
	detach(cmd)
	_ = cmd.Start()
}

// promptWait is how long the prompt hook waits for a fresh transcript
// before handing the agent a command that waits for it instead.
var promptWait = 8 * time.Second

// promptAudio handles recordings a prompt names. Claude Code's Read tool
// refuses audio before any hook runs, so the agent is pointed at the
// transcript, or told how to get one, before it tries.
func promptAudio(root string, in *hookInput) (notes, shown []string) {
	end := time.Now().Add(promptWait) // one wait shared by every recording
	for _, p := range audio.Mentions(root, in.Cwd, in.Prompt) {
		note, msg := audioNote(root, p, time.Until(end))
		notes = append(notes, note)
		if msg != "" {
			shown = append(shown, msg)
		}
	}
	return notes, shown
}

// audioNote says what the agent should read for a recording: the
// transcript when there is one (made now if it takes under wait), else
// the command that makes it or the one-time setup.
func audioNote(root, path string, wait time.Duration) (note, shown string) {
	name := filepath.Base(path)
	if r := audio.Lookup(root, path); r != nil {
		return fmt.Sprintf("%s is audio; tacet transcribed it: Read %s instead.", name, r.Path),
			"tacet: using the transcript of " + name
	}
	cmd := fmt.Sprintf("%q transcribe %q", tacetBin(), path)
	if _, _, _, err := audio.Ready(); err != nil {
		return fmt.Sprintf("%s is audio, which Read can't open. tacet can transcribe it once its speech engine is set up: ask the person to run `tacet transcribe --setup` once in a terminal (it asks before a download of about 70 MB), then run `%s` with Bash and Read the file it prints.", name, cmd),
			"tacet: run tacet transcribe --setup once so tacet can transcribe " + name
	}
	if !audio.Busy(root, path) {
		spawn(root, "transcribe", path)
	}
	for end := time.Now().Add(wait); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		if r := audio.Lookup(root, path); r != nil {
			return fmt.Sprintf("%s is audio; tacet transcribed it: Read %s instead.", name, r.Path),
				"tacet: transcribed " + name
		}
	}
	return fmt.Sprintf("%s is audio, which Read can't open. tacet is transcribing it now: run `%s` with Bash (it waits for that run and prints the transcript path), then Read that file.", name, cmd),
		"tacet: transcribing " + name + " in the background"
}

// preReadAudio covers a Read of a recording, should Claude Code ever let
// one reach the hook: the transcript replaces it, or the read is denied
// with the way to get one.
func preReadAudio(root string, raw json.RawMessage, path string) any {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	r := audio.Lookup(root, path)
	if r == nil {
		note, _ := audioNote(root, path, 0)
		return deny("tacet: " + note)
	}
	in := map[string]any{}
	_ = json.Unmarshal(raw, &in)
	in["file_path"] = r.Path
	msg := "tacet: reading the transcript of " + filepath.Base(path)
	return map[string]any{
		"systemMessage": msg,
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "allow", "updatedInput": in,
			"permissionDecisionReason": msg,
		}}
}
