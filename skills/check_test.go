// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package skills

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/claude"
)

func TestShortenGolden(t *testing.T) {
	cases, _ := filepath.Glob(filepath.Join("testdata", "shorten", "*.before.md"))
	if len(cases) < 4 {
		t.Fatalf("fixtures missing: %v", cases)
	}
	for _, before := range cases {
		name := strings.TrimSuffix(filepath.Base(before), ".before.md")
		t.Run(name, func(t *testing.T) {
			in, err := os.ReadFile(before)
			if err != nil {
				t.Fatal(err)
			}
			out, ok := Shorten(in)
			if name == "short" {
				if ok || !bytes.Equal(out, in) {
					t.Fatalf("a one-sentence description changed:\n%s", out)
				}
				return
			}
			if !ok {
				t.Fatal("not shortened")
			}
			golden(t, filepath.Join("shorten", name+".after.md"), string(out))
			fm := claude.Frontmatter(bufio.NewReader(bytes.NewReader(out)))
			d := fm["description"]
			if len([]rune(d)) > MaxDescription || fm["when_to_use"] != "" || !strings.HasPrefix(d, "Use when") {
				t.Errorf("frontmatter %q", fm)
			}
			if again, ok := Shorten(out); ok || !bytes.Equal(again, out) {
				t.Error("shortening twice changes the file again")
			}
		})
	}
}

func TestFirstSentence(t *testing.T) {
	f, r := FirstSentence("Use when e.g. the build breaks. Then read the log.", 300)
	if f != "Use when e.g. the build breaks." || r != "Then read the log." {
		t.Errorf("%q / %q", f, r)
	}
	f, r = FirstSentence(strings.Repeat("word ", 100), 50)
	if len(f) > 50 || !strings.HasSuffix(f, "...") || r == "" {
		t.Errorf("%q / %q", f, r)
	}
}

func TestSimilarityAndLeading(t *testing.T) {
	a := "Use when asked to run the unit tests and fix every failure before a commit."
	b := "Use when asked to run the unit tests and fix every failure before you push."
	c := "Use when asked to write the release notes from the merged pull requests."
	if s := Similarity(a, b); s < 0.5 {
		t.Errorf("near copies scored %.2f", s)
	}
	if s := Similarity(a, c); s >= 0.5 {
		t.Errorf("different skills scored %.2f", s)
	}
	if Leading(a) != "run the unit" || Leading(a) != Leading(b) || Leading(a) == Leading(c) {
		t.Errorf("leading %q %q %q", Leading(a), Leading(b), Leading(c))
	}
	if Leading("Use when deploying.") != "" || Leading("Use when the a an") != "" {
		t.Error("too short or all filler has no trigger phrase")
	}
}
