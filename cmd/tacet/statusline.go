// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/claude"
	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/fix"
	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/charmbracelet/x/term"
)

// statusInput is the part of Claude Code's statusline JSON tacet shows.
// Every field may be absent or null, so all of them are pointers.
// Schema: https://code.claude.com/docs/en/statusline
type statusInput struct {
	Cwd       string `json:"cwd"`
	Workspace *struct {
		CurrentDir string `json:"current_dir"`
		ProjectDir string `json:"project_dir"`
	} `json:"workspace"`
	Cost *struct {
		TotalCostUSD *float64 `json:"total_cost_usd"`
	} `json:"cost"`
	ContextWindow *struct {
		UsedPercentage *float64 `json:"used_percentage"`
		CurrentUsage   *struct {
			Input      int `json:"input_tokens"`
			CacheWrite int `json:"cache_creation_input_tokens"`
			CacheRead  int `json:"cache_read_input_tokens"`
		} `json:"current_usage"`
	} `json:"context_window"`
	PromptCache *struct {
		CachingObserved *bool    `json:"caching_observed"`
		HitRatio        *float64 `json:"hit_ratio"`
	} `json:"prompt_cache"`
	RateLimits *struct {
		FiveHour *rateWindow `json:"five_hour"`
		SevenDay *rateWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

type rateWindow struct {
	UsedPercentage *float64 `json:"used_percentage"`
}

// ANSI styles. Claude Code captures the output, so lipgloss would see no
// terminal and drop every color; the codes are written directly.
const (
	ansiDot   = "\x1b[1;38;2;255;124;82m" // ui.Dot, bold
	ansiSaved = "\x1b[38;2;255;124;82m"
	ansiWarn  = "\x1b[38;2;249;115;22m" // ui.High
	ansiDim   = "\x1b[2m"
	ansiReset = "\x1b[0m"
)

// chainTimeout bounds the person's own statusline command under --after.
const chainTimeout = 3 * time.Second

// runStatusline prints one line for Claude Code's statusline from the
// JSON on stdin. It never fails: missing data only drops its part.
func runStatusline(a *app, args []string) error {
	install, chain, after := false, false, ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--install":
			install = true
		case "--chain":
			chain = true
		case "--after":
			if i+1 < len(args) {
				after = args[i+1]
				i++
			}
		}
	}
	if install {
		return installStatusline(a, chain)
	}
	var raw []byte
	if f, ok := stdin.(*os.File); !ok || !term.IsTerminal(f.Fd()) {
		raw, _ = io.ReadAll(io.LimitReader(stdin, 1<<20))
	}
	var in statusInput
	_ = json.Unmarshal(raw, &in)
	line := statusLine(&in, statusRoot(&in, a.dir), time.Now(), os.Getenv("NO_COLOR") == "")
	if after != "" {
		line = chainLine(runTheirs(after, raw), line)
	}
	fmt.Fprintln(a.out, line)
	return nil
}

// statusRoot is the nearest folder at or above the session's project
// directory that holds .tacet, or "" when there is none. It walks up
// instead of asking git, to stay well inside the time budget.
func statusRoot(in *statusInput, fallback string) string {
	dir := in.Cwd
	if in.Workspace != nil {
		if in.Workspace.ProjectDir != "" {
			dir = in.Workspace.ProjectDir
		} else if in.Workspace.CurrentDir != "" {
			dir = in.Workspace.CurrentDir
		}
	}
	if dir == "" {
		dir = fallback
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if fi, err := os.Stat(config.Dir(dir)); err == nil && fi.IsDir() {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			return ""
		}
		dir = up
	}
}

// statusLine renders the parts that have data, after the tacet mark.
func statusLine(in *statusInput, root string, now time.Time, color bool) string {
	paint := func(code, s string) string {
		if !color {
			return s
		}
		return code + s + ansiReset
	}
	var parts []string
	if root != "" {
		if n := savedOn(root, now); n > 0 {
			parts = append(parts, paint(ansiSaved, "▾"+ui.Tokens(n)+" saved"))
		}
	}
	if r, ok := cacheRatio(in); ok {
		parts = append(parts, "cache "+pct(r))
	}
	if c := in.ContextWindow; c != nil && c.UsedPercentage != nil {
		parts = append(parts, fmt.Sprintf("ctx %.0f%%", *c.UsedPercentage))
	}
	if rl := in.RateLimits; rl != nil {
		for _, w := range []struct {
			label string
			win   *rateWindow
		}{{"5h", rl.FiveHour}, {"7d", rl.SevenDay}} {
			if w.win == nil || w.win.UsedPercentage == nil {
				continue
			}
			p := *w.win.UsedPercentage
			s := fmt.Sprintf("%s %.0f%%", w.label, p)
			if p >= 80 {
				s = paint(ansiWarn, s)
			}
			parts = append(parts, s)
		}
	}
	if c := in.Cost; c != nil && c.TotalCostUSD != nil && *c.TotalCostUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f", *c.TotalCostUSD))
	}
	line := paint(ansiDot, "tacet")
	if len(parts) > 0 {
		line += " " + strings.Join(parts, paint(ansiDim, " · "))
	}
	return line
}

// cacheRatio is Claude Code's own hit ratio for the session when it
// sends one, else the last request's share of input read from the cache.
func cacheRatio(in *statusInput) (float64, bool) {
	if pc := in.PromptCache; pc != nil && pc.HitRatio != nil && (pc.CachingObserved == nil || *pc.CachingObserved) {
		return *pc.HitRatio, true
	}
	if c := in.ContextWindow; c != nil && c.CurrentUsage != nil {
		u := c.CurrentUsage
		if all := u.Input + u.CacheWrite + u.CacheRead; all > 0 {
			return float64(u.CacheRead) / float64(all), true
		}
	}
	return 0, false
}

// savedOn is the net tokens saved on now's local day, from
// .tacet/savings.jsonl, counted the way docSavings counts them.
func savedOn(root string, now time.Time) int {
	b, err := os.ReadFile(savingsPath(root)) // #nosec G304 -- tacet's own log
	if err != nil {
		return 0
	}
	y, m, d := now.Local().Date()
	saved := 0
	for _, l := range bytes.Split(b, []byte("\n")) {
		var s Saving
		if json.Unmarshal(l, &s) != nil || s.File == "" {
			continue
		}
		if sy, sm, sd := s.Time.Local().Date(); sy == y && sm == m && sd == d {
			saved += s.Saved
		}
	}
	return saved
}

// runTheirs runs the person's own statusline command with the same
// stdin and returns its output, or "" when it fails.
func runTheirs(command string, input []byte) string {
	ctx, cancel := context.WithTimeout(context.Background(), chainTimeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", command) // #nosec G204 G702 -- the person's own statusline command
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command) // #nosec G204 G702 -- the person's own statusline command
	}
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return ""
	}
	return strings.TrimRight(string(out), "\r\n")
}

// chainLine appends tacet's part to the last line of theirs.
func chainLine(theirs, ours string) string {
	if strings.TrimSpace(theirs) == "" {
		return ours
	}
	return theirs + "  " + ours
}

// settingsPath is the user settings file the statusline lives in.
func settingsPath() string { return filepath.Join(claude.Home(), "settings.json") }

// currentStatusLine is the statusLine command in the user settings, or "".
func currentStatusLine() (cmd string, set bool) {
	var m map[string]any
	if readFileJSON(settingsPath(), &m) != nil {
		return "", false
	}
	sl, ok := m["statusLine"]
	if !ok || sl == nil {
		return "", false
	}
	if o, ok := sl.(map[string]any); ok {
		c, _ := o["command"].(string)
		return c, true
	}
	return "", true
}

// isTacetStatusLine reports whether cmd already runs tacet's statusline.
func isTacetStatusLine(cmd string) bool {
	return strings.Contains(cmd, "tacet") && strings.Contains(cmd, "statusline")
}

// statuslineCommand is the command for the settings: the absolute tacet
// path, quoted when it needs it, then statusline and any extra words.
func statuslineCommand(extra string) string {
	bin, err := selfPath()
	if err != nil {
		bin = "tacet"
	}
	c := shellQuote(bin) + " statusline"
	if extra != "" {
		c += " " + extra
	}
	return c
}

// shellQuote leaves plain words alone and single-quotes the rest.
func shellQuote(s string) string {
	plain := func(r rune) bool {
		return strings.ContainsRune(`/\.-_:+@`, r) || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
	}
	if s != "" && strings.IndexFunc(s, func(r rune) bool { return !plain(r) }) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// statuslineFix sets statusLine to command, keeping the other keys of an
// existing statusLine object such as padding.
func statuslineFix(command string) *core.Fix {
	return &core.Fix{
		ID: "statusline", Title: "Show tacet's savings in the Claude Code statusline", Safe: false,
		Edits: []core.Edit{fix.JSONEdit(settingsPath(), func(m map[string]any) {
			sl, _ := m["statusLine"].(map[string]any)
			if sl == nil {
				sl = map[string]any{}
			}
			sl["type"] = "command"
			sl["command"] = command
			m["statusLine"] = sl
		})},
	}
}

// statuslineFinding is the init suggestion: only when no statusLine is
// set, so a person's own is never replaced without asking.
func statuslineFinding() []core.Finding {
	if _, set := currentStatusLine(); set {
		return nil
	}
	return []core.Finding{{
		Rule: "statusline/off", Severity: core.Info, Path: settingsPath(),
		Message: "the statusline can show tokens saved today, cache hits and rate limits (terminal only)",
		Fix:     statuslineFix(statuslineCommand("")),
	}}
}

func installStatusline(a *app, chain bool) error {
	cur, set := currentStatusLine()
	var f *core.Fix
	switch {
	case set && isTacetStatusLine(cur):
		fmt.Fprintln(a.out, "tacet's statusline is already on: "+cur)
		return nil
	case set && cur != "" && chain:
		f = statuslineFix(statuslineCommand("--after " + shellQuote(cur)))
	case set:
		fmt.Fprintln(a.out, "You already have a statusline, so tacet left it alone:")
		fmt.Fprintln(a.out, "  "+ui.Dim.Render(cur))
		fmt.Fprintln(a.out, "To keep it and add tacet's part after it, run:")
		fmt.Fprintln(a.out, "  tacet statusline --install --chain")
		fmt.Fprintln(a.out, "or set its command yourself to:")
		fmt.Fprintln(a.out, "  "+statuslineCommand("--after "+shellQuote(cur)))
		return nil
	default:
		f = statuslineFix(statuslineCommand(""))
	}
	root := a.root()
	_ = config.Exclude(root)
	if _, err := fix.Apply(root, []*core.Fix{f}); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%s Statusline set in %s (tacet undo reverts it). It shows in the terminal, not in VS Code.\n", ui.Good.Render("✓"), settingsPath())
	return nil
}
