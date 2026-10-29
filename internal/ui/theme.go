// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package ui holds the terminal theme: ink, paper and one dot, the same
// palette as the owner's other tools.
package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Brand tokens.
const (
	Ink       = "#100F0D"
	Paper     = "#FFFFFF"
	Dot       = "#FF7C52"
	DotStrong = "#E8603A" // the same hue, deeper, for light backgrounds
)

// Semantic colors.
const (
	Danger = "#EF4444"
	High   = "#F97316"
	Warn   = "#EAB308"
	Low    = "#60A5FA"
	Ok     = "#10B981"
	Muted  = "#6B7280"
)

// Signal is the brand dot, adjusted for the terminal's background.
var Signal = lipgloss.AdaptiveColor{Light: DotStrong, Dark: Dot}

// Shared text styles.
var (
	Brand = lipgloss.NewStyle().Foreground(Signal).Bold(true)
	Label = lipgloss.NewStyle().Bold(true)
	Dim   = lipgloss.NewStyle().Foreground(lipgloss.Color(Muted))
	Good  = lipgloss.NewStyle().Foreground(lipgloss.Color(Ok))
	Bad   = lipgloss.NewStyle().Foreground(lipgloss.Color(Danger))
)

// SeverityColor maps a finding severity to its theme color.
func SeverityColor(sev string) lipgloss.Color {
	switch sev {
	case "high":
		return lipgloss.Color(High)
	case "medium":
		return lipgloss.Color(Warn)
	case "low":
		return lipgloss.Color(Low)
	}
	return lipgloss.Color(Muted)
}

// Banner is the first line of every interactive screen.
func Banner(version string) string {
	return Brand.Render("tacet") + "  " + Dim.Render(version)
}

// Section renders "LABEL  n ───" as a heading width cells wide.
func Section(label string, n, width int) string {
	head := Label.Render(strings.ToUpper(label))
	if n >= 0 {
		head += "  " + Dim.Render(fmt.Sprint(n))
	}
	rule := width - lipgloss.Width(head) - 1
	if rule < 3 {
		rule = 3
	}
	return head + " " + Dim.Render(strings.Repeat("─", rule))
}

// Tokens formats a token count the way every screen shows it: 950, 12.4k, 1.2M.
func Tokens(n int) string {
	switch {
	case n < 0:
		return "-" + Tokens(-n)
	case n < 1000:
		return fmt.Sprint(n)
	case n < 1_000_000:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1000)) + "k"
	case n < 1_000_000_000:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1_000_000)) + "M"
	}
	return trimZero(fmt.Sprintf("%.1f", float64(n)/1_000_000_000)) + "B"
}

func trimZero(s string) string { return strings.TrimSuffix(s, ".0") }
