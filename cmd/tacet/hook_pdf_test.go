// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/internal/config"
)

func TestPDFSeenIsPerSession(t *testing.T) {
	root := t.TempDir()
	if pdfSeen(root, "s1", "/p/a.pdf") {
		t.Fatal("nothing marked yet")
	}
	markPDF(root, "s1", "/p/a.pdf")
	if !pdfSeen(root, "s1", "/p/a.pdf") || pdfSeen(root, "s2", "/p/a.pdf") || pdfSeen(root, "s1", "/p/b.pdf") {
		t.Fatal("marks must be per session and per file")
	}
}

// A first page read of a long PDF opens on the outline; the next page
// read of the same file in that session passes through.
func TestFirstPageReadGetsTheOutline(t *testing.T) {
	src, _ := filepath.Glob(filepath.Join("..", "..", "testbed", "bench", "cache", "doc", "*", "oauth-guide.pdf"))
	if len(src) == 0 {
		t.Skip("the bench PDF isn't built here (testbed/bench/selftest.sh builds it)")
	}
	root := t.TempDir()
	pdf := filepath.Join(root, "guide.pdf")
	b, err := os.ReadFile(src[0])
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(pdf, b, 0o644)
	cfg := config.Load(root)
	raw := json.RawMessage(`{"file_path":"` + pdf + `","pages":"1-2"}`)

	out, err := preRead(root, cfg, raw, pdf, "", "s1")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(out)
	if !strings.Contains(string(got), ".outline.md") && !strings.Contains(string(got), ".focus-") {
		t.Fatalf("first page read should open on the outline: %s", got)
	}
	if strings.Contains(string(got), `"pages"`) {
		t.Fatalf("pages must be dropped from the redirected read: %s", got)
	}
	if out, _ := preRead(root, cfg, raw, pdf, "", "s1"); out != nil {
		t.Fatalf("second page read in the same session should pass through: %v", out)
	}
	if out, _ := preRead(root, cfg, raw, pdf, "", "s2"); out == nil {
		t.Fatal("another session gets the outline again")
	}
}
