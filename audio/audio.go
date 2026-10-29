// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package audio turns recordings into Markdown transcripts with
// timestamps, using the whisper.cpp command line tool. The agent reads a
// few hundred tokens of text instead of a file it can't open at all.
// Transcripts are cached by content hash next to converted documents.
package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/convert"
)

// Exts are the recordings tacet transcribes.
var Exts = map[string]bool{
	".mp3": true, ".wav": true, ".m4a": true, ".ogg": true, ".flac": true, ".webm": true,
}

// native are the formats whisper-cli decodes itself, for when ffmpeg is
// missing.
var native = map[string]bool{".wav": true, ".mp3": true, ".flac": true, ".ogg": true}

// MaxSize skips recordings too big to be worth transcribing here.
const MaxSize = 1 << 30

// Supported reports whether path is a recording tacet transcribes.
func Supported(path string) bool { return Exts[strings.ToLower(filepath.Ext(path))] }

// Mentions finds recordings named in a prompt that exist under root.
func Mentions(root, cwd, prompt string) []string {
	return convert.MentionsOf(root, cwd, prompt, Supported)
}

// Result is one transcript.
type Result struct {
	Source   string
	Path     string // the Markdown transcript
	Cached   bool
	Language string
	Seconds  float64 // length of the recording
}

const suffix = ".transcript.md"

// TranscriptPath is where the transcript of path's current bytes goes.
func TranscriptPath(root, path string) (string, error) {
	sum, err := convert.Sum(root, path)
	if err != nil {
		return "", err
	}
	return convert.CachePath(root, path, sum, suffix), nil
}

// Lookup returns the transcript made earlier from path's bytes, or nil.
func Lookup(root, path string) *Result {
	out, err := TranscriptPath(root, path)
	if err != nil {
		return nil
	}
	if fi, err := os.Stat(out); err != nil || fi.Size() == 0 { // #nosec G703 -- tacet's cache
		return nil
	}
	return &Result{Source: path, Path: out, Cached: true}
}

// Options pick the engine and model; Ready fills them.
type Options struct {
	Bin, Model string
	English    bool
	Threads    int
}

// ErrNoFFmpeg means the recording needs ffmpeg to decode and it is missing.
var ErrNoFFmpeg = errors.New("ffmpeg is needed to read this format and isn't on PATH: install it (brew install ffmpeg, sudo apt install ffmpeg or winget install ffmpeg) and try again")

// lockAge is how long a transcription in progress keeps others waiting.
const lockAge = 30 * time.Minute

// Transcribe writes the transcript of path under root's cache, reusing
// an earlier one of the same bytes. When another tacet is transcribing
// the same file, it waits for that result.
func Transcribe(ctx context.Context, root, path string, o Options) (*Result, error) {
	fi, err := os.Stat(path) // #nosec G703 -- a recording the person named
	if err != nil {
		return nil, err
	}
	if fi.Size() > MaxSize {
		return nil, fmt.Errorf("%s is over %d MB", filepath.Base(path), MaxSize>>20)
	}
	out, err := TranscriptPath(root, path)
	if err != nil {
		return nil, err
	}
	if r := Lookup(root, path); r != nil {
		return r, nil
	}
	if err := os.MkdirAll(convert.Dir(root), 0o750); err != nil {
		return nil, err
	}
	unlock, err := lock(ctx, out+".lock")
	if err != nil {
		return nil, err
	}
	defer unlock()
	if r := Lookup(root, path); r != nil {
		return r, nil // another run finished it while this one waited
	}
	tmp, err := os.MkdirTemp("", "tacet-audio-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	input, err := toWAV(ctx, path, filepath.Join(tmp, "in.wav"))
	if err != nil {
		return nil, err
	}
	base := filepath.Join(tmp, "out")
	cmd := exec.CommandContext(ctx, o.Bin, Args(o, input, base)...) // #nosec G204 -- whisper-cli with tacet's own arguments
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("whisper-cli: %w: %s", err, lastLines(stderr.String(), 3))
	}
	b, err := os.ReadFile(base + ".json") // #nosec G304 -- our temp folder
	if err != nil {
		return nil, fmt.Errorf("whisper-cli wrote no transcript: %s", lastLines(stderr.String(), 3))
	}
	var w whisperJSON
	if err := json.Unmarshal(b, &w); err != nil {
		return nil, err
	}
	rel := path
	if r, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(r, "..") {
		rel = filepath.ToSlash(r)
	}
	segs := w.segments()
	md := Markdown(rel, w.Result.Language, segs)
	part := out + ".tacet-tmp"
	if err := os.WriteFile(part, []byte(md), 0o600); err != nil { // #nosec G703 -- tacet's cache
		return nil, err
	}
	if err := os.Rename(part, out); err != nil { // #nosec G703 -- tacet's cache
		return nil, err
	}
	r := &Result{Source: path, Path: out, Language: w.Result.Language}
	if n := len(segs); n > 0 {
		r.Seconds = segs[n-1].End
	}
	return r, nil
}

// lock takes path, waiting while a fresh lock is held elsewhere.
func lock(ctx context.Context, path string) (func(), error) {
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 G703 -- tacet's cache
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil // #nosec G703 -- tacet's cache
		}
		fi, serr := os.Stat(path) // #nosec G703 -- tacet's cache
		if serr == nil && time.Since(fi.ModTime()) > lockAge {
			_ = os.Remove(path) // #nosec G703 -- left behind by a run that died
			continue
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// Busy reports whether a transcription of path is in progress.
func Busy(root, path string) bool {
	out, err := TranscriptPath(root, path)
	if err != nil {
		return false
	}
	fi, err := os.Stat(out + ".lock")
	return err == nil && time.Since(fi.ModTime()) < lockAge
}

// toWAV converts path to 16 kHz mono WAV with ffmpeg. Without ffmpeg,
// formats whisper-cli reads itself go through as they are.
func toWAV(ctx context.Context, path, wav string) (string, error) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		if native[strings.ToLower(filepath.Ext(path))] {
			return path, nil
		}
		return "", ErrNoFFmpeg
	}
	cmd := exec.CommandContext(ctx, ff, FFmpegArgs(path, wav)...) // #nosec G204 -- ffmpeg with tacet's own arguments
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg could not read %s: %s", filepath.Base(path), lastLines(stderr.String(), 2))
	}
	return wav, nil
}

// FFmpegArgs converts in to 16 kHz mono 16-bit WAV, the input whisper wants.
func FFmpegArgs(in, wav string) []string {
	return []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", in, "-vn", "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", wav}
}

// Args are whisper-cli's arguments: JSON output to base.json, no
// console text, the language detected unless the model is English only.
func Args(o Options, wav, base string) []string {
	lang := "auto"
	if o.English {
		lang = "en"
	}
	t := o.Threads
	if t <= 0 {
		t = min(runtime.NumCPU(), 8)
	}
	return []string{"-m", o.Model, "-f", wav, "-oj", "-of", base, "-np", "-l", lang, "-t", strconv.Itoa(t)}
}

type whisperJSON struct {
	Result struct {
		Language string `json:"language"`
	} `json:"result"`
	Transcription []struct {
		Offsets struct {
			From int `json:"from"`
			To   int `json:"to"`
		} `json:"offsets"`
		Text string `json:"text"`
	} `json:"transcription"`
}

// Segment is a stretch of speech, in seconds from the start.
type Segment struct {
	Start, End float64
	Text       string
}

func (w *whisperJSON) segments() []Segment {
	var out []Segment
	for _, t := range w.Transcription {
		text := strings.TrimSpace(t.Text)
		if text == "" || text == "[BLANK_AUDIO]" {
			continue
		}
		out = append(out, Segment{float64(t.Offsets.From) / 1000, float64(t.Offsets.To) / 1000, text})
	}
	return out
}

// LineSpan is how much speech one transcript line holds: whisper's own
// segments are a few seconds each and a timestamp on each costs tokens.
const LineSpan = 30.0

// Markdown lays segments out as lines of about LineSpan seconds, each
// starting with its timestamp.
func Markdown(rel, lang string, segs []Segment) string {
	var b strings.Builder
	long := len(segs) > 0 && segs[len(segs)-1].End >= 3600
	head := "<!-- tacet transcribed " + rel
	if lang != "" {
		head += " (language " + lang + ")"
	}
	head += " with whisper.cpp. Each line starts at its [timestamp]; ask to listen to the original if tone matters. -->\n\n"
	b.WriteString(head)
	if len(segs) == 0 {
		b.WriteString("(no speech found)\n")
		return b.String()
	}
	var line []string
	start := segs[0].Start
	flush := func() {
		if len(line) > 0 {
			fmt.Fprintf(&b, "[%s] %s\n", Stamp(start, long), strings.Join(line, " "))
			line = line[:0]
		}
	}
	for _, s := range segs {
		if len(line) > 0 && s.Start-start >= LineSpan {
			flush()
		}
		if len(line) == 0 {
			start = s.Start
		}
		line = append(line, s.Text)
	}
	flush()
	return b.String()
}

// Stamp formats seconds as mm:ss, or h:mm:ss for long recordings.
func Stamp(sec float64, long bool) string {
	s := int(sec)
	if long {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " / ")
}
