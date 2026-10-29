// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/audio"
	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/charmbracelet/huh"
)

// runTranscribe is tacet transcribe [--setup] [--english] <audio>...
func runTranscribe(a *app, args []string) error {
	setup, english := false, false
	var files []string
	for _, x := range args {
		switch x {
		case "--setup":
			setup = true
		case "--english":
			english = true
		default:
			if strings.HasPrefix(x, "-") {
				return fmt.Errorf("%w: tacet transcribe [--setup] [--english] <audio file> [more files]", errUsage)
			}
			files = append(files, x)
		}
	}
	if !setup && len(files) == 0 {
		return fmt.Errorf("%w: tacet transcribe [--setup] [--english] <audio file> [more files]", errUsage)
	}
	if _, _, _, err := audio.Ready(); setup || err != nil {
		if err := setupAudio(a, english, setup); err != nil {
			return err
		}
	}
	if len(files) == 0 {
		return nil
	}
	bin, model, en, err := audio.Ready()
	if err != nil {
		return err
	}
	root := a.root()
	opts := audio.Options{Bin: bin, Model: model, English: en}
	var results []map[string]any
	for _, f := range files {
		p, err := filepath.Abs(f)
		if err != nil {
			return err
		}
		if !audio.Supported(p) {
			return fmt.Errorf("%s: not an audio file tacet reads (.mp3 .wav .m4a .ogg .flac .webm)", f)
		}
		start := time.Now()
		r, err := audio.Transcribe(context.Background(), root, p, opts)
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		took := time.Since(start).Round(100 * time.Millisecond)
		results = append(results, map[string]any{"source": f, "transcript": r.Path, "cached": r.Cached, "language": r.Language, "seconds": r.Seconds, "took_ms": took.Milliseconds()})
		if a.json {
			continue
		}
		note := "cached"
		if !r.Cached {
			note = fmt.Sprintf("%s of audio in %s", (time.Duration(r.Seconds) * time.Second).String(), took)
		}
		fmt.Fprintf(a.out, "%s %s\n", r.Path, ui.Dim.Render(note))
	}
	if a.json {
		return a.writeJSON(results)
	}
	return nil
}

// setupAudio downloads the speech engine and model after a yes. asked
// is true for tacet transcribe --setup, so a ready setup says so.
func setupAudio(a *app, english, asked bool) error {
	need, err := audio.Plan(english)
	if err != nil {
		return err
	}
	if len(need) == 0 {
		if asked && !a.json {
			fmt.Fprintln(a.err, "tacet: the speech engine is ready")
		}
		return nil
	}
	var mb int64
	var names []string
	for _, n := range need {
		mb += n.Size
		names = append(names, n.Name)
	}
	mb = (mb + (1 << 20) - 1) >> 20
	title := fmt.Sprintf("Download the speech engine (%s, %d MB) into %s? Every file is checked against a pinned SHA-256.", strings.Join(names, ", "), mb, audio.CacheDir())
	ok := a.yes
	switch {
	case ok:
	case interactive():
		if err := huh.NewConfirm().Title(title).Affirmative("Download").Negative("Not now").Value(&ok).Run(); err != nil && !errors.Is(err, huh.ErrUserAborted) {
			return err
		}
	default:
		return fmt.Errorf("%s Run tacet transcribe --setup in a terminal, or add --yes", title)
	}
	if !ok {
		return audio.ErrNotSetUp
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 30 * time.Minute}
	if err := audio.Setup(ctx, client, need, a.err); err != nil {
		return err
	}
	if _, _, _, err := audio.Ready(); err != nil {
		return err
	}
	fmt.Fprintln(a.err, "tacet: the speech engine is ready")
	return nil
}

// tacetBin is how the agent should call tacet from Bash: the plugin runs
// it by absolute path, so it may not be on PATH.
func tacetBin() string {
	if p, err := os.Executable(); err == nil {
		return p
	}
	return "tacet"
}
