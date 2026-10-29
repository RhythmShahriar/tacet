// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

func (a *app) writeJSON(v any) error {
	enc := json.NewEncoder(a.out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func interactive() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
}

func width() int {
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 40 {
		if w > 100 {
			return 100
		}
		return w
	}
	return 80
}

func printFindings(w io.Writer, title string, fs []core.Finding) {
	fmt.Fprintln(w, ui.Section(title, len(fs), width()))
	if len(fs) == 0 {
		fmt.Fprintln(w, "  "+ui.Good.Render("nothing to fix"))
		return
	}
	for _, f := range fs {
		sev := lipgloss.NewStyle().Foreground(ui.SeverityColor(string(f.Severity))).Render(fmt.Sprintf("%-6s", f.Severity))
		where := f.Path
		if f.Line > 0 {
			where = fmt.Sprintf("%s:%d", f.Path, f.Line)
		}
		save := ""
		if t := saving(f); t > 0 {
			save = ui.Brand.Render(" -" + ui.Tokens(t))
		}
		fmt.Fprintf(w, "  %s %s%s\n", sev, f.Message, save)
		if where != "" {
			fmt.Fprintf(w, "         %s\n", ui.Dim.Render(where))
		}
		if f.Hint != "" {
			fmt.Fprintf(w, "         %s\n", ui.Dim.Render("→ "+f.Hint))
		}
	}
}

func saving(f core.Finding) int {
	if f.Tokens > 0 {
		return f.Tokens
	}
	if f.Fix != nil {
		return f.Fix.Tokens
	}
	return 0
}
