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
	"time"

	"github.com/RhythmShahriar/tacet/internal/config"
)

// budgetProject is a project with the budget feature on and a 1,000
// token session budget, plus an empty transcript.
func budgetProject(t *testing.T) (string, *config.Config, *hookInput) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Load(root)
	if err := cfg.Set("budget", true); err != nil {
		t.Fatal(err)
	}
	cfg.Budget.SessionTokens = 1000
	tp := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(tp, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, cfg, &hookInput{SessionID: "s1", TranscriptPath: tp, Cwd: root, Prompt: "go on"}
}

// spend appends one assistant response that used n tokens, written as
// two lines repeating the usage the way Claude Code writes it.
func spend(t *testing.T, tp, id string, n int) {
	t.Helper()
	f, err := os.OpenFile(tp, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	for _, block := range []string{"text", "tool_use"} {
		fmt.Fprintf(f, `{"type":"assistant","message":{"id":%q,"role":"assistant","content":[{"type":%q}],"usage":{"input_tokens":%d,"cache_read_input_tokens":0,"output_tokens":0}}}`+"\n", id, block, n)
	}
	fmt.Fprintln(f, `{"type":"user","message":{"role":"user","content":"ok"}}`)
}

func budgetMessage(t *testing.T, out any) string {
	t.Helper()
	if out == nil {
		return ""
	}
	m := out.(map[string]any)
	s, _ := m["systemMessage"].(string)
	return s
}

func TestBudgetThresholds(t *testing.T) {
	root, cfg, in := budgetProject(t)
	prompt := func() string {
		out, err := handle(root, cfg, "prompt", in)
		if err != nil {
			t.Fatal(err)
		}
		return budgetMessage(t, out)
	}

	spend(t, in.TranscriptPath, "m1", 500)
	if msg := prompt(); msg != "" {
		t.Fatalf("under the budget: %q", msg)
	}

	spend(t, in.TranscriptPath, "m2", 300) // 800 of 1,000
	msg := prompt()
	if !strings.Contains(msg, "this session has used 800 tokens (80% of your 1k budget)") || !strings.Contains(msg, "/compact") {
		t.Fatalf("at 80%%: %q", msg)
	}
	if msg := prompt(); msg != "" {
		t.Fatalf("80%% warned twice: %q", msg)
	}

	spend(t, in.TranscriptPath, "m3", 200) // 1,000 of 1,000
	out, _ := handle(root, cfg, "prompt", in)
	msg = budgetMessage(t, out)
	if !strings.Contains(msg, "1k tokens (100% of your 1k budget)") {
		t.Fatalf("at 100%%: %q", msg)
	}
	ctx := out.(map[string]any)["hookSpecificOutput"].(map[string]any)["additionalContext"].(string)
	if !strings.Contains(ctx, "100% used") {
		t.Errorf("model note: %q", ctx)
	}

	spend(t, in.TranscriptPath, "m4", 500)
	if msg := prompt(); msg != "" {
		t.Fatalf("100%% warned twice: %q", msg)
	}

	// Another session starts its own count and thresholds.
	in2 := *in
	in2.SessionID = "s2"
	in2.TranscriptPath = filepath.Join(t.TempDir(), "s2.jsonl")
	if err := os.WriteFile(in2.TranscriptPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	spend(t, in2.TranscriptPath, "n1", 900)
	out, _ = handle(root, cfg, "prompt", &in2)
	if msg := budgetMessage(t, out); !strings.Contains(msg, "900 tokens (90% of") {
		t.Fatalf("second session: %q", msg)
	}
}

func TestBudgetJumpWarnsOnce(t *testing.T) {
	root, cfg, in := budgetProject(t)
	spend(t, in.TranscriptPath, "m1", 1500)
	out, _ := handle(root, cfg, "prompt", in)
	msg := budgetMessage(t, out)
	if strings.Count(msg, "tacet:") != 1 || !strings.Contains(msg, "150%, over your 1k budget") {
		t.Fatalf("jump past both thresholds: %q", msg)
	}
}

func TestBudgetReadsOnlyTheTail(t *testing.T) {
	root, cfg, in := budgetProject(t)
	spend(t, in.TranscriptPath, "m1", 100)
	// A half-written line is left for the next prompt.
	f, _ := os.OpenFile(in.TranscriptPath, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(`{"type":"assistant","message":{"id":"m2","usage":{"input_tokens":700`)
	_ = f.Close()
	if _, err := handle(root, cfg, "prompt", in); err != nil {
		t.Fatal(err)
	}
	var st budgetState
	if err := readFileJSON(budgetPath(root), &st); err != nil {
		t.Fatal(err)
	}
	s := st.Sessions["s1"]
	if s.Tokens != 100 || s.Offset == 0 {
		t.Fatalf("state %+v", s)
	}
	f, _ = os.OpenFile(in.TranscriptPath, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString("}}}\n")
	_ = f.Close()
	out, _ := handle(root, cfg, "prompt", in)
	if msg := budgetMessage(t, out); !strings.Contains(msg, "800 tokens") {
		t.Fatalf("after the line finished: %q", msg)
	}
}

func TestBudgetOff(t *testing.T) {
	root, cfg, in := budgetProject(t)
	_ = cfg.Set("budget", false)
	spend(t, in.TranscriptPath, "m1", 5000)
	out, err := handle(root, cfg, "prompt", in)
	if err != nil || out != nil {
		t.Fatalf("feature off: %v %v", out, err)
	}
	if _, err := os.Stat(budgetPath(root)); err == nil {
		t.Error("feature off wrote state")
	}
	if config.Features["budget"] {
		t.Error("budget is on by default")
	}
}

func TestBudgetLimitsFile(t *testing.T) {
	root, cfg, in := budgetProject(t)
	cfg.Budget.SessionCostUSD = 10
	now := time.Now()
	if err := os.MkdirAll(config.Dir(root), 0o750); err != nil {
		t.Fatal(err)
	}
	write := func(used float64, resets int64, cost float64) {
		b, _ := json.Marshal(map[string]any{
			"session_id": "s1",
			"cost":       map[string]any{"total_cost_usd": cost},
			"rate_limits": map[string]any{
				"five_hour": map[string]any{"used_percentage": used, "resets_at": resets},
			},
		})
		if err := os.WriteFile(limitsPath(root), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(85, now.Add(2*time.Hour).Unix(), 8.5)
	shown, _ := promptBudget(root, cfg, in, now)
	all := strings.Join(shown, "\n")
	if len(shown) != 2 || !strings.Contains(all, "your 5-hour limit is 85% used, resetting at") ||
		!strings.Contains(all, "cost about $8.50 (85% of your $10.00 budget)") {
		t.Fatalf("limits: %q", shown)
	}
	if shown, _ := promptBudget(root, cfg, in, now); len(shown) != 0 {
		t.Fatalf("limits warned twice: %q", shown)
	}
	// A window that already reset is stale and says nothing.
	write(95, now.Add(-time.Minute).Unix(), 0)
	if shown, _ := promptBudget(root, cfg, in, now); len(shown) != 0 {
		t.Fatalf("stale window: %q", shown)
	}
	// A new window warns again.
	write(82, now.Add(5*time.Hour).Unix(), 0)
	if shown, _ := promptBudget(root, cfg, in, now); len(shown) != 1 {
		t.Fatalf("new window: %q", shown)
	}
}

func TestBudgetLimitsDefaults(t *testing.T) {
	b := config.Budget{}.Limits()
	if b.SessionTokens != config.DefaultSessionTokens || len(b.WarnAt) != 2 || b.WarnAt[0] != 0.8 || b.WarnAt[1] != 1 {
		t.Fatalf("defaults %+v", b)
	}
	b = config.Budget{SessionTokens: 5, WarnAt: []float64{1, 0.5, -1}}.Limits()
	if b.SessionTokens != 5 || len(b.WarnAt) != 2 || b.WarnAt[0] != 0.5 {
		t.Fatalf("custom %+v", b)
	}
}
