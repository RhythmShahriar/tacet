// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/internal/config"
)

// bigLog is a build log of n lines with one failure in the middle.
func bigLog(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if i == n/2 {
			b.WriteString("pkg/x/x.go:12: undefined: Frobnicate\n")
			continue
		}
		fmt.Fprintf(&b, "compiling module %04d of the build with some padding text to make it long\n", i)
	}
	return b.String()
}

func runPostOutput(t *testing.T, root string, cfg *config.Config, tool string, resp any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(resp)
	out := postOutput(root, cfg, &hookInput{ToolName: tool, ToolResponse: raw})
	if out == nil {
		return nil
	}
	b, _ := json.Marshal(out)
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	return got
}

func TestBigOutputIsOffByDefault(t *testing.T) {
	root := t.TempDir()
	cfg := config.Load(root)
	if cfg.On("bigoutput") {
		t.Fatal("bigoutput must default to off")
	}
	resp := map[string]any{"stdout": bigLog(3000), "stderr": "", "interrupted": false, "isImage": false}
	if got := runPostOutput(t, root, cfg, "Bash", resp); got != nil {
		t.Errorf("off: replaced output %v", got)
	}
}

func TestBigOutputKeepsHeadTailAndErrors(t *testing.T) {
	root := t.TempDir()
	cfg := config.Load(root)
	cfg.Features["bigoutput"] = true

	small := map[string]any{"stdout": "ok\n", "stderr": "", "interrupted": false, "isImage": false}
	if got := runPostOutput(t, root, cfg, "Bash", small); got != nil {
		t.Errorf("small output was replaced: %v", got)
	}

	full := bigLog(3000)
	resp := map[string]any{"stdout": full, "stderr": "warn", "interrupted": false, "isImage": false}
	got := runPostOutput(t, root, cfg, "Bash", resp)
	if got == nil {
		t.Fatal("big output was not replaced")
	}
	h := got["hookSpecificOutput"].(map[string]any)
	if h["hookEventName"] != "PostToolUse" {
		t.Errorf("event %v", h["hookEventName"])
	}
	upd := h["updatedToolOutput"].(map[string]any)
	if upd["stderr"] != "warn" || upd["interrupted"] != false || upd["isImage"] != false {
		t.Errorf("the Bash output shape changed: %v", upd)
	}
	view := upd["stdout"].(string)
	for _, want := range []string{"compiling module 0001", "compiling module 0040", "1500: pkg/x/x.go:12: undefined: Frobnicate", "compiling module 3000", "full output: "} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	if strings.Contains(view, "compiling module 0041") || len(view) > len(full)/10 {
		t.Errorf("view kept too much: %d of %d bytes", len(view), len(full))
	}
	files, _ := filepath.Glob(filepath.Join(outDir(root), "*.md"))
	if len(files) != 1 {
		t.Fatalf("stored files %v", files)
	}
	if b, _ := os.ReadFile(files[0]); string(b) != full || !strings.Contains(view, files[0]) {
		t.Errorf("stored output differs or the view doesn't name %s", files[0])
	}

	// WebFetch: whatever string field is long gets the same treatment.
	page := map[string]any{"result": strings.Repeat("A long page line about nothing much at all.\n", 2000), "code": 200.0}
	got = runPostOutput(t, root, cfg, "WebFetch", page)
	if got == nil {
		t.Fatal("big page was not replaced")
	}
	upd = got["hookSpecificOutput"].(map[string]any)["updatedToolOutput"].(map[string]any)
	if upd["code"] != 200.0 || !strings.Contains(upd["result"].(string), "full output: ") {
		t.Errorf("page view: %v", upd)
	}
}

func TestBigOutputOneLongLine(t *testing.T) {
	root := t.TempDir()
	cfg := config.Load(root)
	cfg.Features["bigoutput"] = true
	got := runPostOutput(t, root, cfg, "Bash", map[string]any{"stdout": strings.Repeat("x", 100_000)})
	if got == nil {
		t.Fatal("a long single line was not replaced")
	}
	view := got["hookSpecificOutput"].(map[string]any)["updatedToolOutput"].(map[string]any)["stdout"].(string)
	if len(view) > 2000 {
		t.Errorf("view of one long line is %d bytes", len(view))
	}
}
