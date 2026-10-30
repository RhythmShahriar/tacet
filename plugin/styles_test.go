// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package plugin

import (
	"os"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/tokens"
)

func TestWriteStyles(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, "/usr/bin/tacet", "1.2.3"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"terse", "lesscode"} {
		b, err := os.ReadFile(StylePath(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, want := range []string{"---\nname: " + name + "\n", "keep-coding-instructions: true\n", "path and line", "No preamble"} {
			if !strings.Contains(s, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
		// Every token of a style is paid on every request.
		body := s[strings.Index(s[3:], "---")+6:]
		if n := tokens.Estimate(body); n > 130 {
			t.Errorf("%s style body is %d tokens", name, n)
		}
	}
	if TerseStyle != "tacet:terse" || LessCodeStyle != "tacet:lesscode" {
		t.Fatalf("style names %s %s", TerseStyle, LessCodeStyle)
	}
}
