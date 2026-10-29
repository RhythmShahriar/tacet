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

	"github.com/RhythmShahriar/tacet/audio"
	"github.com/RhythmShahriar/tacet/claude"
	"github.com/RhythmShahriar/tacet/convert"
	"github.com/RhythmShahriar/tacet/guard"
	"github.com/RhythmShahriar/tacet/handoff"
	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/RhythmShahriar/tacet/speak"
	"github.com/RhythmShahriar/tacet/tokens"
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
	Prompt         string          `json:"prompt"`
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
	if args[0] == "background-convert" {
		backgroundConvert(a.root())
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
	if os.Getenv("TACET_DEBUG") != "" {
		logHook(root, args[0], fmt.Errorf("debug: tool=%s input=%s", in.ToolName, in.ToolInput))
	}
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
		return preRead(root, cfg, in.ToolInput, ti.FilePath, in.TranscriptPath)
	case "pre-bash":
		var ti toolInput
		_ = json.Unmarshal(in.ToolInput, &ti)
		if f, ok := guard.BashReadsSecret(ti.Command); ok && cfg.On("guard") {
			return deny(fmt.Sprintf("tacet guard: this would print %s, which holds secrets. Ask the person for the one value you need instead.", f)), nil
		}
		return nil, nil
	case "prompt":
		return promptDocs(root, cfg, in), nil
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
	if n := takeNotice(root); n != "" {
		parts = append(parts, n)
	}
	if d := sessionDocs(root, cfg); d != "" {
		parts = append(parts, d)
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
func preRead(root string, cfg *config.Config, raw json.RawMessage, path, transcript string) (any, error) {
	if strings.HasPrefix(path, convert.Dir(root)+string(filepath.Separator)) {
		recordFollowUp(root, path, raw)
		return nil, nil
	}
	if cfg.On("audio") && audio.Supported(path) {
		return preReadAudio(root, raw, path), nil
	}
	if !cfg.On("convert") || !convert.Supported(path) {
		return nil, nil
	}
	in := map[string]any{}
	_ = json.Unmarshal(raw, &in)
	isPDF := strings.EqualFold(filepath.Ext(path), ".pdf")
	if _, pages := in["pages"]; pages && isPDF {
		return nil, nil // a page range of a PDF: Claude reads it natively for less
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
	delete(in, "pages")
	in["file_path"] = r.Path
	_, hasLimit := in["limit"]
	_, hasOffset := in["offset"]
	switch {
	case hasLimit || hasOffset:
		// The agent asked for a range: give it that range of the text.
	case r.Outline != "" && cfg.On("outline"):
		// A long document: start with the outline, plus the sections that
		// match what the person asked, so only what the task needs is read.
		in["file_path"] = convert.ReadTarget(r, lastAsk(transcript), true)
	case r.FirstPart > 0:
		// Return part 1 exactly, so the read never hits the size cap.
		in["limit"] = r.FirstPart
	}
	msg := recordSaving(root, filepath.Base(path), r.Path, in["file_path"].(string), in["limit"])
	return map[string]any{
		"systemMessage": msg,
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "allow", "updatedInput": in,
			"permissionDecisionReason": msg,
		}}, nil
}

// Saving is one read tacet made cheaper, logged to .tacet/savings.jsonl.
type Saving struct {
	Time   time.Time `json:"time"`
	File   string    `json:"file"`
	Full   int       `json:"full_tokens"` // the whole document as text
	Read   int       `json:"read_tokens"` // what the agent was given
	Saved  int       `json:"saved_tokens"`
	Method string    `json:"method"` // focus, outline, part, markdown
}

func savingsPath(root string) string { return filepath.Join(config.Dir(root), "savings.jsonl") }

// recordSaving logs a redirect and returns the line shown to the person.
// The full document as text is the baseline; reading the PDF itself
// costs more than that (measured), so the saving is a floor.
func recordSaving(root, name, mdPath, target string, limit any) string {
	full := tokenCount(mdPath, 0)
	lim := 0
	if l, ok := limit.(int); ok {
		lim = l
	}
	read := tokenCount(target, lim)
	method := "markdown"
	switch {
	case strings.Contains(target, ".focus-"):
		method = "focus"
	case strings.HasSuffix(target, ".outline.md"):
		method = "outline"
	case lim > 0:
		method = "part"
	}
	s := Saving{Time: time.Now().UTC(), File: name, Full: full, Read: read, Saved: full - read, Method: method}
	if s.Saved < 0 {
		s.Saved = 0
	}
	if f, err := os.OpenFile(savingsPath(root), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		b, _ := json.Marshal(s)
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
	what := map[string]string{
		"focus":    "its outline and the sections matching your request",
		"outline":  "its outline first",
		"part":     "Markdown, part 1",
		"markdown": "Markdown",
	}[method]
	return fmt.Sprintf("tacet: reading %s as %s, about %s tokens instead of %s for the whole document", name, what, ui.Tokens(read), ui.Tokens(full))
}

// recordFollowUp logs a read of converted text after the outline, so the
// savings total is net of what the agent read afterwards.
func recordFollowUp(root, path string, raw json.RawMessage) {
	if strings.Contains(path, ".focus-") || strings.HasSuffix(path, ".outline.md") {
		return
	}
	var in struct {
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	_ = json.Unmarshal(raw, &in)
	b, err := os.ReadFile(path) // #nosec G304 -- tacet's own cache
	if err != nil {
		return
	}
	lines := strings.Split(string(b), "\n")
	from := in.Offset - 1
	if from < 0 {
		from = 0
	}
	to := len(lines)
	if in.Limit > 0 && from+in.Limit < to {
		to = from + in.Limit
	}
	if from >= to {
		return
	}
	read := tokens.Estimate(strings.Join(lines[from:to], "\n"))
	s := Saving{Time: time.Now().UTC(), File: filepath.Base(path), Read: read, Saved: -read, Method: "follow-up"}
	if f, err := os.OpenFile(savingsPath(root), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		b, _ := json.Marshal(s)
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
}

func tokenCount(path string, limitLines int) int {
	b, err := os.ReadFile(path) // #nosec G304 -- tacet's own cache
	if err != nil {
		return 0
	}
	if limitLines > 0 {
		lines := strings.SplitN(string(b), "\n", limitLines+1)
		if len(lines) > limitLines {
			lines = lines[:limitLines]
		}
		return tokens.Estimate(strings.Join(lines, "\n"))
	}
	return tokens.EstimateBytes(b)
}

// lastAsk is the newest prompt the person typed in the session.
func lastAsk(transcript string) string {
	if transcript == "" {
		return ""
	}
	s, err := claude.ReadSession(transcript)
	if err != nil {
		return ""
	}
	for i := len(s.Turns) - 1; i >= 0; i-- {
		if s.Turns[i].Human {
			return s.Turns[i].Text
		}
	}
	return ""
}

// promptDocs handles documents a prompt names that Claude Code can't
// read itself (Word, PowerPoint, Excel): it converts them now and says
// which Markdown file to read, focused on the request.
func promptDocs(root string, cfg *config.Config, in *hookInput) any {
	if in.Prompt == "" {
		return nil
	}
	var notes, shown []string
	// The handoff note costs tokens, so it is only added when the person
	// asks to carry on from before.
	if cfg.On("handoff") && handoff.WantsContinue(in.Prompt) {
		if note := handoff.ForSession(root, in.SessionID, time.Now()); note != "" {
			notes = append(notes, "Notes from the previous session on this project (tacet SESSION.md):\n\n"+note)
			shown = append(shown, "tacet: added the notes from your last session")
		}
	}
	if cfg.On("audio") {
		n, s := promptAudio(root, in)
		notes, shown = append(notes, n...), append(shown, s...)
	}
	if !cfg.On("convert") {
		return finishPrompt(notes, shown)
	}
	for _, p := range convert.Mentions(root, in.Cwd, in.Prompt) {
		if !convert.Binary(p) {
			continue // PDFs are redirected when they're read
		}
		r, err := convert.File(root, p)
		if err != nil {
			continue
		}
		target := convert.ReadTarget(r, in.Prompt, cfg.On("outline"))
		notes = append(notes, fmt.Sprintf("%s can't be read directly; tacet converted it: Read %s instead.", filepath.Base(p), target))
		shown = append(shown, recordSaving(root, filepath.Base(p), r.Path, target, nil))
	}
	return finishPrompt(notes, shown)
}

func finishPrompt(notes, shown []string) any {
	if len(notes) == 0 {
		return nil
	}
	ctx := strings.Join(notes, "\n\n")
	if len(ctx) > maxContext {
		ctx = ctx[:maxContext]
	}
	return map[string]any{
		"systemMessage": strings.Join(shown, "\n"),
		"hookSpecificOutput": map[string]any{
			"hookEventName": "UserPromptSubmit", "additionalContext": ctx,
		}}
}
