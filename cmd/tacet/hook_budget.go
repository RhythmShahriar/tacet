// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/claude"
	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/internal/ui"
)

// Hooks get no rate limits or cost on stdin; only the status line does.
// The budget guard counts this session's tokens from the transcript,
// reading only what was added since the last prompt. It also uses
// .tacet/limits.json when a status line command leaves Claude Code's
// own figures there.

const budgetTip = "/compact or a new session with SESSION.md would cut what each turn re-reads."

// budgetKeep is how long an idle session's counters are kept.
const budgetKeep = 7 * 24 * time.Hour

// budgetSession is one session's running count.
type budgetSession struct {
	Transcript string    `json:"transcript"`
	Offset     int64     `json:"offset"`
	Tokens     int       `json:"tokens"`
	Recent     []string  `json:"recent,omitempty"` // message ids near the offset, counted once
	Fired      []string  `json:"fired,omitempty"`  // thresholds already shown
	Seen       time.Time `json:"seen"`
}

type budgetState struct {
	Sessions map[string]*budgetSession `json:"sessions"`
}

func budgetPath(root string) string { return filepath.Join(config.Dir(root), "budget.json") }
func limitsPath(root string) string { return filepath.Join(config.Dir(root), "limits.json") }

// limitWindow is one rate-limit window from the status line JSON.
type limitWindow struct {
	Used     *float64 `json:"used_percentage"`
	ResetsAt int64    `json:"resets_at"`
}

// statusLimits is the part of Claude Code's status line JSON tacet
// reads, as a status line command may save it to .tacet/limits.json.
type statusLimits struct {
	SessionID string `json:"session_id"`
	Cost      *struct {
		TotalUSD *float64 `json:"total_cost_usd"`
	} `json:"cost"`
	RateLimits map[string]limitWindow `json:"rate_limits"`
}

var windowNames = map[string]string{"five_hour": "5-hour", "seven_day": "7-day"}

// withBudget adds the budget warnings to what the prompt hook returns.
func withBudget(out any, root string, cfg *config.Config, in *hookInput) any {
	shown, note := promptBudget(root, cfg, in, time.Now())
	if len(shown) == 0 {
		return out
	}
	m, _ := out.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	msg := strings.Join(shown, "\n")
	if s, _ := m["systemMessage"].(string); s != "" {
		msg = s + "\n" + msg
	}
	m["systemMessage"] = msg
	if note != "" {
		h, _ := m["hookSpecificOutput"].(map[string]any)
		if h == nil {
			h = map[string]any{"hookEventName": "UserPromptSubmit"}
		}
		if c, _ := h["additionalContext"].(string); c != "" {
			note = c + "\n\n" + note
		}
		h["additionalContext"] = note
		m["hookSpecificOutput"] = h
	}
	return m
}

// promptBudget returns the lines to show the person, one per threshold
// newly crossed, plus one short line for the model. It never blocks.
func promptBudget(root string, cfg *config.Config, in *hookInput, now time.Time) (shown []string, note string) {
	if !cfg.On("budget") || in.SessionID == "" {
		return nil, ""
	}
	b := cfg.Budget.Limits()
	var st budgetState
	_ = readFileJSON(budgetPath(root), &st)
	if st.Sessions == nil {
		st.Sessions = map[string]*budgetSession{}
	}
	for id, s := range st.Sessions {
		if now.Sub(s.Seen) > budgetKeep {
			delete(st.Sessions, id)
		}
	}
	s := st.Sessions[in.SessionID]
	if s == nil || s.Transcript != in.TranscriptPath {
		s = &budgetSession{Transcript: in.TranscriptPath}
		st.Sessions[in.SessionID] = s
	}
	s.Seen = now
	if in.TranscriptPath != "" {
		if err := s.count(); err != nil {
			logHook(root, "prompt", fmt.Errorf("budget: %w", err))
		}
	}
	worst := 0.0

	// This session's tokens against the budget.
	frac := float64(s.Tokens) / float64(b.SessionTokens)
	if s.cross("tokens", frac, b.WarnAt) {
		worst = max(worst, frac)
		shown = append(shown, fmt.Sprintf("tacet: this session has used %s tokens (%s). %s",
			ui.Tokens(s.Tokens), budgetShare(frac, ui.Tokens(b.SessionTokens)), budgetTip))
	}

	// Claude Code's own cost and rate limits, when the status line saved them.
	var lim statusLimits
	if readFileJSON(limitsPath(root), &lim) == nil {
		if b.SessionCostUSD > 0 && lim.Cost != nil && lim.Cost.TotalUSD != nil && lim.SessionID == in.SessionID {
			f := *lim.Cost.TotalUSD / b.SessionCostUSD
			if s.cross("cost", f, b.WarnAt) {
				worst = max(worst, f)
				shown = append(shown, fmt.Sprintf("tacet: this session has cost about $%.2f (%s). %s",
					*lim.Cost.TotalUSD, budgetShare(f, fmt.Sprintf("$%.2f", b.SessionCostUSD)), budgetTip))
			}
		}
		for _, key := range []string{"five_hour", "seven_day"} {
			w, ok := lim.RateLimits[key]
			if !ok || w.Used == nil || (w.ResetsAt > 0 && now.Unix() >= w.ResetsAt) {
				continue // absent, or the window already reset
			}
			f := *w.Used / 100
			// A window that resets starts its thresholds over.
			if s.cross(fmt.Sprintf("%s@%d", key, w.ResetsAt), f, b.WarnAt) {
				worst = max(worst, f)
				line := fmt.Sprintf("tacet: your %s limit is %.0f%% used", windowNames[key], *w.Used)
				if w.ResetsAt > 0 {
					line += ", resetting " + resetTime(time.Unix(w.ResetsAt, 0), now)
				}
				shown = append(shown, line+". "+budgetTip)
			}
		}
	}
	if err := writeFileJSON(budgetPath(root), st); err != nil {
		logHook(root, "prompt", fmt.Errorf("budget: %w", err))
	}
	if len(shown) > 0 {
		note = fmt.Sprintf("tacet: the person's session budget is %.0f%% used; prefer targeted reads and short replies.", worst*100)
	}
	return shown, note
}

// cross marks every threshold frac has reached and reports whether one
// of them is new, so a jump past two thresholds warns once.
func (s *budgetSession) cross(metric string, frac float64, at []float64) bool {
	fresh := false
	for _, a := range at {
		if frac < a {
			break
		}
		key := fmt.Sprintf("%s:%g", metric, a)
		if slices.Contains(s.Fired, key) {
			continue
		}
		s.Fired = append(s.Fired, key)
		fresh = true
	}
	return fresh
}

// budgetShare words frac of a budget: "80% of your 2M budget", or "105%, over
// your 2M budget" once it is spent.
func budgetShare(frac float64, budget string) string {
	if frac > 1 {
		return fmt.Sprintf("%.0f%%, over your %s budget", frac*100, budget)
	}
	return fmt.Sprintf("%.0f%% of your %s budget", frac*100, budget)
}

func resetTime(t, now time.Time) string {
	if t.Sub(now) < 20*time.Hour {
		return "at " + t.Local().Format("15:04")
	}
	return t.Local().Format("Mon 15:04")
}

// usageLine is the part of a transcript line the count needs.
type usageLine struct {
	Type    string `json:"type"`
	Message *struct {
		ID    string        `json:"id"`
		Usage *claude.Usage `json:"usage"`
	} `json:"message"`
}

// count adds the usage written to the transcript since the last call.
// A partly written last line is left for next time; a transcript that
// shrank is counted again from the start.
func (s *budgetSession) count() error {
	f, err := os.Open(s.Transcript) // #nosec G304 -- the transcript Claude Code named
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.Size() < s.Offset {
		s.Offset, s.Tokens, s.Recent = 0, 0, nil
	}
	if _, err := f.Seek(s.Offset, io.SeekStart); err != nil {
		return err
	}
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil // an unfinished line waits for the next prompt
			}
			return err
		}
		s.Offset += int64(len(line))
		if !bytes.Contains(line, []byte(`"usage"`)) {
			continue
		}
		var l usageLine
		if json.Unmarshal(line, &l) != nil || l.Type != "assistant" || l.Message == nil || l.Message.Usage == nil {
			continue
		}
		// One response is written as several lines, each repeating its usage.
		if id := l.Message.ID; id != "" {
			if slices.Contains(s.Recent, id) {
				continue
			}
			s.Recent = append(s.Recent, id)
			if len(s.Recent) > 16 {
				s.Recent = s.Recent[len(s.Recent)-16:]
			}
		}
		s.Tokens += l.Message.Usage.Total()
	}
}
