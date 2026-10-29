// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package audio

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPinnedHashes(t *testing.T) {
	hex64 := regexp.MustCompile(`^[0-9a-f]{64}$`)
	all := []Asset{ModelMulti, ModelEnglish}
	for _, e := range engines {
		all = append(all, e)
	}
	for _, a := range all {
		if !hex64.MatchString(a.SHA256) || a.Size <= 0 || !strings.HasPrefix(a.URL, "https://") || !strings.HasSuffix(a.URL, a.Name) {
			t.Errorf("bad pin %+v", a)
		}
	}
}

func TestArgs(t *testing.T) {
	got := Args(Options{Model: "m.bin", Threads: 3}, "in.wav", "/tmp/out")
	want := []string{"-m", "m.bin", "-f", "in.wav", "-oj", "-of", "/tmp/out", "-np", "-l", "auto", "-t", "3"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
	en := Args(Options{Model: "m.bin", English: true}, "in.wav", "o")
	if i := slices.Index(en, "-l"); en[i+1] != "en" {
		t.Fatalf("english model should pin the language: %v", en)
	}
	if i := slices.Index(en, "-t"); en[i+1] == "0" {
		t.Fatalf("threads default to the CPU count: %v", en)
	}
	ff := strings.Join(FFmpegArgs("a.m4a", "b.wav"), " ")
	if !strings.Contains(ff, "-i a.m4a") || !strings.Contains(ff, "-ar 16000 -ac 1 -c:a pcm_s16le b.wav") {
		t.Fatalf("ffmpeg args %s", ff)
	}
}

func TestMarkdownGroupsSegmentsIntoLines(t *testing.T) {
	segs := []Segment{{0, 4, "Hello there."}, {4, 12, "Second bit."}, {31, 35, "New line."}, {36, 40, "Same line."}}
	md := Markdown("talk.mp3", "en", segs)
	want := "[00:00] Hello there. Second bit.\n[00:31] New line. Same line.\n"
	if !strings.HasSuffix(md, want) || !strings.HasPrefix(md, "<!-- tacet transcribed talk.mp3 (language en)") {
		t.Fatalf("markdown:\n%s", md)
	}
	long := Markdown("x.wav", "", []Segment{{0, 2, "a"}, {3725, 3730, "b"}})
	if !strings.Contains(long, "[1:02:05] b") {
		t.Fatalf("hour stamps:\n%s", long)
	}
	if !strings.Contains(Markdown("x.wav", "", nil), "(no speech found)") {
		t.Fatal("empty transcript")
	}
}

func TestSegmentsDropBlanks(t *testing.T) {
	var w whisperJSON
	if err := json.Unmarshal([]byte(`{"result":{"language":"de"},"transcription":[{"offsets":{"from":0,"to":1500},"text":" Hallo"},{"offsets":{"from":1500,"to":2000},"text":" [BLANK_AUDIO]"}]}`), &w); err != nil {
		t.Fatal(err)
	}
	s := w.segments()
	if len(s) != 1 || s[0].Text != "Hallo" || s[0].End != 1.5 || w.Result.Language != "de" {
		t.Fatalf("segments %+v", s)
	}
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func serve(t *testing.T, files map[string][]byte) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadChecksTheHash(t *testing.T) {
	body := []byte("model bytes")
	srv := serve(t, map[string][]byte{"/m.bin": body})
	dir := t.TempDir()
	dst := filepath.Join(dir, "m.bin")
	good := Asset{"m.bin", srv.URL + "/m.bin", sum(body), int64(len(body))}
	if err := Download(context.Background(), srv.Client(), good, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); !bytes.Equal(b, body) {
		t.Fatal("wrong bytes")
	}
	os.Remove(dst)
	bad := good
	bad.SHA256 = sum([]byte("other"))
	err := Download(context.Background(), srv.Client(), bad, dst)
	if !errors.Is(err, ErrHash) {
		t.Fatalf("want ErrHash, got %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a bad download was left behind: %v", entries)
	}
	missing := good
	missing.URL = srv.URL + "/nope"
	if err := Download(context.Background(), srv.Client(), missing, dst); err == nil {
		t.Fatal("404 should fail")
	}
}

type entry struct {
	name, link string
	body       string
}

func tgz(t *testing.T, entries ...entry) []byte {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.link != "" {
			h = &tar.Header{Name: e.name, Linkname: e.link, Typeflag: tar.TypeSymlink}
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(e.body))
	}
	tw.Close()
	gz.Close()
	return b.Bytes()
}

func TestUnpackTarKeepsOnlyWhatRuns(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "e.tar.gz")
	os.WriteFile(arc, tgz(t,
		entry{name: "whisper-bin-ubuntu-x64/whisper-cli", body: "#!/bin/sh\n"},
		entry{name: "whisper-bin-ubuntu-x64/libwhisper.so.1.9.5", body: "lib"},
		entry{name: "whisper-bin-ubuntu-x64/libwhisper.so.1", link: "libwhisper.so.1.9.5"},
		entry{name: "whisper-bin-ubuntu-x64/bench", body: "skip"},
	), 0o600)
	out := filepath.Join(dir, "out")
	if err := Unpack(arc, out); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"whisper-cli", "libwhisper.so.1.9.5", "libwhisper.so.1"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "bench")); err == nil {
		t.Error("extracted a file it doesn't need")
	}
	os.WriteFile(arc, tgz(t, entry{name: "x/libevil.so", link: "../../../etc/passwd"}), 0o600)
	if err := Unpack(arc, filepath.Join(dir, "evil")); err == nil {
		t.Fatal("a link out of the folder was accepted")
	}
}

func TestUnpackZip(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, n := range []string{"Release/whisper-cli.exe", "Release/whisper.dll", "Release/bench.exe"} {
		w, _ := z.Create(n)
		w.Write([]byte(n))
	}
	z.Close()
	dir := t.TempDir()
	arc := filepath.Join(dir, "e.zip")
	os.WriteFile(arc, b.Bytes(), 0o600)
	if err := Unpack(arc, filepath.Join(dir, "out")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadDir(filepath.Join(dir, "out"))
	var names []string
	for _, e := range got {
		names = append(names, e.Name())
	}
	if strings.Join(names, " ") != "whisper-cli.exe whisper.dll" {
		t.Fatalf("unpacked %v", names)
	}
}

// isolate points tacet at an empty cache and a PATH without whisper or ffmpeg.
func isolate(t *testing.T) string {
	cache := t.TempDir()
	t.Setenv("TACET_CACHE_DIR", cache)
	t.Setenv("TACET_WHISPER_CLI", "")
	t.Setenv("PATH", t.TempDir())
	return cache
}

func TestSetupFromAFakeServer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake engine is a tar.gz")
	}
	cache := isolate(t)
	if _, _, _, err := Ready(); !errors.Is(err, ErrNotSetUp) {
		t.Fatalf("empty cache should not be ready: %v", err)
	}
	eng := tgz(t, entry{name: "w/whisper-cli", body: "#!/bin/sh\n"})
	model := []byte("tiny model")
	srv := serve(t, map[string][]byte{"/e.tar.gz": eng, "/" + ModelMulti.Name: model})
	need := []Asset{
		{"e.tar.gz", srv.URL + "/e.tar.gz", sum(eng), int64(len(eng))},
		{ModelMulti.Name, srv.URL + "/" + ModelMulti.Name, sum(model), int64(len(model))},
	}
	var log bytes.Buffer
	if err := Setup(context.Background(), srv.Client(), need, &log); err != nil {
		t.Fatal(err)
	}
	if FindWhisper() != filepath.Join(cache, "whisper-"+EngineVersion, "whisper-cli") {
		t.Fatalf("engine not found: %q", FindWhisper())
	}
	if _, err := os.Stat(filepath.Join(cache, "e.tar.gz")); err == nil {
		t.Fatal("archive kept after unpacking")
	}
	if !strings.Contains(log.String(), "downloading e.tar.gz") {
		t.Fatalf("progress: %s", log.String())
	}
	// The fake model has the wrong size for the real pin, so it doesn't count.
	if p, _ := FindModel(); p != "" {
		t.Fatalf("a model of the wrong size was accepted: %s", p)
	}
}

func TestPlanWithoutEngineBuild(t *testing.T) {
	isolate(t)
	need, err := Plan(false)
	if _, ok := Engine(); !ok {
		if err == nil || !strings.Contains(err.Error(), InstallHint()) {
			t.Fatalf("want install hint, got %v", err)
		}
		return
	}
	if err != nil || len(need) != 2 || need[1] != ModelMulti {
		t.Fatalf("plan %v %v", need, err)
	}
	if en, _ := Plan(true); en[1] != ModelEnglish {
		t.Fatalf("english plan %v", en)
	}
}

// fakeWhisper is a whisper-cli stand-in that writes a fixed transcript and
// counts its runs.
func fakeWhisper(t *testing.T) (bin, runs string) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	dir := t.TempDir()
	runs = filepath.Join(dir, "runs")
	bin = filepath.Join(dir, "whisper-cli")
	script := `#!/bin/sh
echo run >> "` + runs + `"
while [ $# -gt 0 ]; do [ "$1" = "-of" ] && out="$2"; shift; done
printf '%s' '{"result":{"language":"en"},"transcription":[{"offsets":{"from":0,"to":2500},"text":" Ship it on Friday."}]}' > "$out.json"
`
	os.WriteFile(bin, []byte(script), 0o755)
	return bin, runs
}

func TestTranscribeCachesByContent(t *testing.T) {
	isolate(t)
	bin, runs := fakeWhisper(t)
	root := t.TempDir()
	clip := filepath.Join(root, "standup.wav")
	os.WriteFile(clip, []byte("RIFF fake"), 0o644)
	o := Options{Bin: bin, Model: "m.bin"}
	r, err := Transcribe(context.Background(), root, clip, o)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(r.Path)
	if !strings.Contains(string(b), "[00:00] Ship it on Friday.") || r.Language != "en" || r.Seconds != 2.5 {
		t.Fatalf("transcript %+v:\n%s", r, b)
	}
	if !strings.HasSuffix(r.Path, ".transcript.md") || !strings.HasPrefix(r.Path, filepath.Join(root, ".tacet", "docs", "standup-")) {
		t.Fatalf("path %s", r.Path)
	}
	again, err := Transcribe(context.Background(), root, clip, o)
	if err != nil || !again.Cached || again.Path != r.Path {
		t.Fatalf("second run %+v %v", again, err)
	}
	if n, _ := os.ReadFile(runs); strings.Count(string(n), "run") != 1 {
		t.Fatalf("whisper ran %d times", strings.Count(string(n), "run"))
	}
	if Lookup(root, clip) == nil || Busy(root, clip) {
		t.Fatal("lookup or lock state wrong after a run")
	}
}

func TestTranscribeNeedsFFmpegForM4A(t *testing.T) {
	isolate(t)
	bin, _ := fakeWhisper(t)
	root := t.TempDir()
	clip := filepath.Join(root, "memo.m4a")
	os.WriteFile(clip, []byte("fake"), 0o644)
	_, err := Transcribe(context.Background(), root, clip, Options{Bin: bin, Model: "m"})
	if !errors.Is(err, ErrNoFFmpeg) {
		t.Fatalf("want ErrNoFFmpeg, got %v", err)
	}
}

func TestLockWaitsForTheOtherRun(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	unlock, err := lock(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := lock(ctx, p); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second lock should wait: %v", err)
	}
	unlock()
	u2, err := lock(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	u2()
}

func TestMentions(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "call.mp3"), []byte("x"), 0o644)
	got := Mentions(root, root, "summarize call.mp3 and missing.wav, then spec.docx")
	if len(got) != 1 || filepath.Base(got[0]) != "call.mp3" {
		t.Fatalf("mentions %v", got)
	}
}

// TestRealTranscription runs the real engine when TACET_WHISPER=1. The
// clip is TACET_WHISPER_CLIP, or one spoken by espeak-ng or say.
func TestRealTranscription(t *testing.T) {
	if os.Getenv("TACET_WHISPER") != "1" {
		t.Skip("set TACET_WHISPER=1 to run the real engine")
	}
	bin, model, en, err := Ready()
	if err != nil {
		t.Skip(err)
	}
	root := t.TempDir()
	clip := os.Getenv("TACET_WHISPER_CLIP")
	if clip == "" {
		clip = filepath.Join(root, "hello.wav")
		text := "The deployment is planned for Friday morning."
		var cmd *exec.Cmd
		if p, err := exec.LookPath("espeak-ng"); err == nil {
			cmd = exec.Command(p, "-w", clip, text)
		} else if p, err := exec.LookPath("say"); err == nil {
			clip = filepath.Join(root, "hello.aiff")
			cmd = exec.Command(p, "-o", clip, text)
		} else {
			t.Skip("no clip: set TACET_WHISPER_CLIP or install espeak-ng")
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("tts: %v %s", err, out)
		}
		if filepath.Ext(clip) == ".aiff" {
			wav := filepath.Join(root, "hello.wav")
			if out, err := exec.Command("ffmpeg", FFmpegArgs(clip, wav)...).CombinedOutput(); err != nil {
				t.Skipf("ffmpeg: %v %s", err, out)
			}
			clip = wav
		}
	}
	start := time.Now()
	r, err := Transcribe(context.Background(), root, clip, Options{Bin: bin, Model: model, English: en})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(r.Path)
	t.Logf("%.1fs of audio in %s:\n%s", r.Seconds, time.Since(start), b)
	if !strings.Contains(string(b), "[00:00]") {
		t.Fatal("no timestamps")
	}
}
