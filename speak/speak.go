// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package speak reads text aloud with the speech tool the system already
// has: say on macOS, spd-say or espeak-ng on Linux, SAPI on Windows.
package speak

import (
	"errors"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// MaxChars caps what is read aloud: the end of a long reply is the part
// worth hearing.
const MaxChars = 600

// ErrNoVoice means no speech tool was found.
var ErrNoVoice = errors.New("no speech tool found (say, spd-say, espeak-ng or espeak)")

var (
	codeRe = regexp.MustCompile("(?s)```.*?```")
	markRe = regexp.MustCompile("[`*_#>|\\[\\]]")
	linkRe = regexp.MustCompile(`\]\([^)]*\)`)
)

// Plain turns Markdown into text worth saying: no code blocks, links or marks.
func Plain(md string) string {
	s := codeRe.ReplaceAllString(md, " (code) ")
	s = linkRe.ReplaceAllString(s, "]")
	s = markRe.ReplaceAllString(s, "")
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > MaxChars {
		s = string(r[len(r)-MaxChars:])
		if i := strings.Index(s, ". "); i >= 0 && i < 200 {
			s = s[i+2:]
		}
	}
	return s
}

// Say starts reading text aloud and returns without waiting.
func Say(md string) error {
	text := Plain(md)
	if text == "" {
		return nil
	}
	cmd, err := command(text)
	if err != nil {
		return err
	}
	return cmd.Start()
}

func command(text string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("say", text), nil // #nosec G204 -- text is an argument, never a shell
	case "windows":
		ps := "Add-Type -AssemblyName System.Speech; (New-Object System.Speech.Synthesis.SpeechSynthesizer).Speak($args[0])"
		return exec.Command("powershell", "-NoProfile", "-Command", ps, text), nil // #nosec G204
	}
	for _, c := range []string{"spd-say", "espeak-ng", "espeak"} {
		if p, err := exec.LookPath(c); err == nil {
			return exec.Command(p, text), nil // #nosec G204
		}
	}
	return nil, ErrNoVoice
}
