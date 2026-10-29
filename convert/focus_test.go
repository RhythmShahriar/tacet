// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package convert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFocusPicksMatchingSection(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	ol := filepath.Join(dir, "doc.outline.md")
	body := "# Spec\nintro\n## Install\nrun the installer and pick a folder\n## Exit codes\nexit 130 means interrupted; E1 nothing installed\n## Release\ntag and push\n"
	os.WriteFile(md, []byte(body), 0o644)
	os.WriteFile(ol, []byte("outline\n"), 0o644)
	f := Focus(ol, md, "which exit code means interrupted?")
	if f == "" {
		t.Fatal("no focus file")
	}
	b, _ := os.ReadFile(f)
	got := string(b)
	if !strings.Contains(got, "exit 130 means interrupted") || strings.Contains(got, "run the installer") {
		t.Fatalf("wrong sections:\n%s", got)
	}
	if !strings.HasPrefix(got, "outline\n") {
		t.Fatal("outline must come first")
	}
	if Focus(ol, md, "the and for") != "" {
		t.Fatal("stop words alone shouldn't match")
	}
	if Focus(ol, md, "kubernetes helm") != "" {
		t.Fatal("no match should fall back to the outline")
	}
}
