// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/audio"
	"github.com/RhythmShahriar/tacet/internal/config"
)

func testDocx(text string) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	add := func(name, body string) { w, _ := z.Create(name); w.Write([]byte(body)) }
	add("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`)
	add("_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`)
	add("word/document.xml", `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>`+text+`</w:t></w:r></w:p></w:body></w:document>`)
	z.Close()
	return b.Bytes()
}

func TestSessionDocsListsTrackedOfficeFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	run := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	cfg := config.Load(root)
	if got := sessionDocs(root, cfg); got != "" {
		t.Fatalf("no documents should add nothing, got %q", got)
	}
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)
	os.WriteFile(filepath.Join(root, "docs", "spec.docx"), testDocx(strings.Repeat("The retry policy backs off exponentially. ", 8)), 0o644)
	run("add", "docs/spec.docx")

	got := sessionDocs(root, cfg)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "docs/spec.docx → read .tacet/docs/spec-") {
		t.Fatalf("session text:\n%s", got)
	}
	if again := sessionDocs(root, cfg); again != got {
		t.Fatalf("cached session text changed:\n%s", again)
	}
	cfg.Features["convert"] = false
	if off := sessionDocs(root, cfg); off != "" {
		t.Fatalf("convert off still listed: %q", off)
	}
}

func isolateAudio(t *testing.T) {
	t.Setenv("TACET_CACHE_DIR", t.TempDir())
	t.Setenv("TACET_WHISPER_CLI", "")
	t.Setenv("PATH", t.TempDir())
}

func TestAudioNoteAsksForSetup(t *testing.T) {
	isolateAudio(t)
	root := t.TempDir()
	clip := filepath.Join(root, "call.mp3")
	os.WriteFile(clip, []byte("x"), 0o644)
	note, shown := audioNote(root, clip, 0)
	if !strings.Contains(note, "tacet transcribe --setup") || !strings.Contains(shown, "--setup") {
		t.Fatalf("note %q shown %q", note, shown)
	}
	// A read that reaches the hook is denied with the same advice.
	out, _ := json.Marshal(preReadAudio(root, json.RawMessage(`{"file_path":"call.mp3"}`), "call.mp3"))
	if !strings.Contains(string(out), `"permissionDecision":"deny"`) || !strings.Contains(string(out), "--setup") {
		t.Fatalf("pre-read %s", out)
	}
}

func TestPromptPointsAtACachedTranscript(t *testing.T) {
	isolateAudio(t)
	root := t.TempDir()
	clip := filepath.Join(root, "standup.wav")
	os.WriteFile(clip, []byte("RIFF"), 0o644)
	p, err := audio.TranscriptPath(root, clip)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("[00:00] hello\n"), 0o600)

	cfg := config.Load(root)
	out, _ := json.Marshal(promptDocs(root, cfg, &hookInput{Cwd: root, Prompt: "what was decided in standup.wav?"}))
	if !strings.Contains(string(out), jsonText("Read "+p)) {
		t.Fatalf("prompt output %s", out)
	}
	pre, _ := json.Marshal(preReadAudio(root, json.RawMessage(`{"file_path":"`+clip+`"}`), clip))
	if !strings.Contains(string(pre), `"file_path":`+jsonQuote(p)) {
		t.Fatalf("pre-read %s", pre)
	}
	cfg.Features["audio"] = false
	if off := promptDocs(root, cfg, &hookInput{Cwd: root, Prompt: "standup.wav"}); off != nil {
		t.Fatalf("audio off still answered: %v", off)
	}
}

// jsonQuote is s as a JSON string, so Windows backslashes compare as
// they appear in hook output.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// jsonText is s escaped the way it appears inside a JSON string.
func jsonText(s string) string {
	q := jsonQuote(s)
	return q[1 : len(q)-1]
}
