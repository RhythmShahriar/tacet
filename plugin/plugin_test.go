// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package plugin

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteValidates(t *testing.T) {
	dir := t.TempDir()
	if out := os.Getenv("TACET_PLUGIN_OUT"); out != "" {
		dir = out
	}
	if err := Write(dir, "/opt/my tools/tacet", "1.2.3"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "plugins", Name, "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var h struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatal(err)
	}
	if got := h.Hooks["PreToolUse"][0].Hooks[0].Command; got != `"/opt/my tools/tacet" hook pre-read` {
		t.Fatalf("command %q", got)
	}
	if cl, err := exec.LookPath("claude"); err == nil && os.Getenv("TACET_VALIDATE") != "" {
		out, err := exec.Command(cl, "plugin", "validate", dir).CombinedOutput()
		if err != nil || strings.Contains(strings.ToLower(string(out)), "error") {
			t.Fatalf("claude plugin validate: %v\n%s", err, out)
		}
		t.Logf("%s", out)
	}
}

func TestSkillHiddenFromModel(t *testing.T) {
	if !strings.Contains(Skill("tacet"), "disable-model-invocation: true") {
		t.Fatal("the /tacet skill must cost no context")
	}
}

func TestSkillDraftsOnDemand(t *testing.T) {
	s := Skill("tacet")
	for _, want := range []string{"Bash(tacet skills new:*)", "make this a skill", "/tacet skill <what>", "tacet skills new --from-session --json", "--yes in place of --json"} {
		if !strings.Contains(s, want) {
			t.Errorf("the /tacet skill lacks %q", want)
		}
	}
}
