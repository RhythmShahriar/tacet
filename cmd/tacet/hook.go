// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/claude"
	"github.com/RhythmShahriar/tacet/convert"
	"github.com/RhythmShahriar/tacet/guard"
	"github.com/RhythmShahriar/tacet/handoff"
	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/speak"
)

// hookInput is the JSON Claude Code sends a hook on stdin.
type hookInput struct {
	Event          string          `json:"hook_event_name"`
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	Source         string          `json:"source"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	LastMessage    string          `json:"last_assistant_message"`
	StopHookActive bool            `json:"stop_hook_active"`
}

type toolInput struct {
	FilePath string `json:"file_path"`
	Command  string `json:"command"`
}

// maxContext is Claude Code's cap on a hook's additionalContext.
const maxContext = 10_000

// runHook handles one hook event. It never fails Claude Code: errors go
// to .tacet/hook.log and the hook exits 0 with no output.
func runHook(a *app, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: tacet hook <event>", errUsage)
	}
	if args[0] == "background-audit" {
		backgroundAudit(a.root())
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(os.Stdin, 8<<20))
	if err != nil {
		return nil
	}
	var in hookInput
	if json.Unmarshal(b, &in) != nil {
		return nil
	}
	if in.Cwd == "" {
		in.Cwd, _ = os.Getwd()
	}
	root := config.Root(in.Cwd)
	if !config.Initialized(root) {
		return nil // tacet only works where tacet init ran
	}
	cfg := config.Load(root)
	out, err := handle(root, cfg, args[0], &in)
	if err != nil {
		logHook(root, args[0], err)
		return nil
	}
	if out != nil {
		_ = json.NewEncoder(a.out).Encode(out)
	}
	return nil
}

func handle(root string, cfg *config.Config, event string, in *hookInput) (any, error) {
	switch event {
	case "session-start":
		return sessionStart(root, cfg, in), nil
	case "pre-read":
		var ti toolInput
		_ = json.Unmarshal(in.ToolInput, &ti)
		if cfg.On("guard") && guard.IsSecretPath(ti.FilePath) {
			return deny(fmt.Sprintf("tacet guard: %s holds secrets, so it stays out of the conversation. Ask the person for the one value you need, or use a .example file.", filepath.Base(ti.FilePath))), nil
		}
		return preRead(root, cfg, in.ToolInput, ti.FilePath)
	case "pre-bash":
		var ti toolInput
		_ = json.Unmarshal(in.ToolInput, &ti)
		if f, ok := guard.BashReadsSecret(ti.Command); ok && cfg.On("guard") {
			return deny(fmt.Sprintf("tacet guard: this would print %s, which holds secrets. Ask the person for the one value you need instead.", f)), nil
		}
		return nil, nil
	case "pre-compact":
		return nil, writeHandoff(root, cfg, in)
	case "session-end":
		err := writeHandoff(root, cfg, in)
		countSession(root, cfg)
		return nil, err
	case "stop":
		if cfg.On("speak") && !in.StopHookActive && in.LastMessage != "" {
			return nil, speak.Say(in.LastMessage)
		}
		return nil, nil
	}
	return nil, nil
}

func deny(reason string) any {
	return map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason,
	}}
}

func sessionStart(root string, cfg *config.Config, in *hookInput) any {
	var parts []string
	if cfg.On("handoff") && in.Source != "resume" {
		if note := handoff.ForSession(root, in.SessionID, time.Now()); note != "" {
			parts = append(parts, "Notes from the previous session on this project (tacet SESSION.md):\n\n"+note)
		}
	}
	if n := takeNotice(root); n != "" {
		parts = append(parts, n)
	}
	if len(parts) == 0 {
		return nil
	}
	ctx := strings.Join(parts, "\n\n")
	if len(ctx) > maxContext {
		ctx = ctx[:maxContext]
	}
	return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "SessionStart", "additionalContext": ctx}}
}

func writeHandoff(root string, cfg *config.Config, in *hookInput) error {
	if !cfg.On("handoff") || in.TranscriptPath == "" {
		return nil
	}
	s, err := claude.ReadSession(in.TranscriptPath)
	if err != nil {
		return err
	}
	if s.ID == "" {
		s.ID = in.SessionID
	}
	_ = config.ExcludePath(root, handoff.File)
	return handoff.Write(root, s, time.Now())
}

// state is tacet's per-project counters.
type state struct {
	Sessions  int `json:"sessions"`
	LastAudit int `json:"last_audit"`
}

func statePath(root string) string  { return filepath.Join(config.Dir(root), "state.json") }
func noticePath(root string) string { return filepath.Join(config.Dir(root), "notice.txt") }

// countSession counts the session and, every AuditEvery sessions, starts
// a re-audit in the background: SessionEnd hooks get 1.5 seconds.
func countSession(root string, cfg *config.Config) {
	var st state
	_ = readFileJSON(statePath(root), &st)
	st.Sessions++
	due := st.Sessions-st.LastAudit >= cfg.AuditEvery
	if due {
		st.LastAudit = st.Sessions
	}
	_ = writeFileJSON(statePath(root), st)
	if !due {
		return
	}
	if self, err := os.Executable(); err == nil {
		cmd := exec.Command(self, "-C", root, "hook", "background-audit") // #nosec G204 -- tacet itself
		detach(cmd)
		_ = cmd.Start()
	}
}

// backgroundAudit rescans and leaves a one-line notice for the next
// session when suggestions are waiting.
func backgroundAudit(root string) {
	s, err := doScan(root, time.Time{})
	if err != nil {
		logHook(root, "background-audit", err)
		return
	}
	_, sug := s.fixes()
	if len(sug) == 0 {
		_ = os.Remove(noticePath(root))
		return
	}
	saved := 0
	for _, f := range sug {
		saved += saving(f)
	}
	msg := fmt.Sprintf("tacet has %d suggestion(s) for this project", len(sug))
	if saved > 0 {
		msg += fmt.Sprintf(" that would save about %d tokens per request", saved)
	}
	msg += ". Mention once, briefly, that the person can type /tacet to review them; don't apply anything yourself."
	_ = os.WriteFile(noticePath(root), []byte(msg), 0o600)
}

func takeNotice(root string) string {
	b, err := os.ReadFile(noticePath(root))
	if err != nil {
		return ""
	}
	_ = os.Remove(noticePath(root))
	return strings.TrimSpace(string(b))
}

func logHook(root, event string, err error) {
	f, ferr := os.OpenFile(filepath.Join(config.Dir(root), "hook.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if ferr != nil {
		return
	}
	defer func() { _ = f.Close() }()
	fmt.Fprintf(f, "%s %s: %v\n", time.Now().UTC().Format(time.RFC3339), event, err)
}

// preRead sends a read of a PDF or Office file to its Markdown version.
// Anything that goes wrong lets the original read through.
func preRead(root string, cfg *config.Config, raw json.RawMessage, path string) (any, error) {
	if !cfg.On("convert") || !convert.Supported(path) {
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	r, err := convert.File(root, path)
	if errors.Is(err, convert.ErrScanned) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	in := map[string]any{}
	_ = json.Unmarshal(raw, &in)
	delete(in, "pages")
	in["file_path"] = r.Path
	// A long document: return part 1 exactly, so the read never hits the
	// size cap and the header tells the agent where part 2 starts.
	if _, set := in["limit"]; !set && r.FirstPart > 0 {
		if _, off := in["offset"]; !off {
			in["limit"] = r.FirstPart
		}
	}
	return map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "allow", "updatedInput": in,
		"permissionDecisionReason": "tacet: reading " + filepath.Base(path) + " as Markdown",
	}}, nil
}
