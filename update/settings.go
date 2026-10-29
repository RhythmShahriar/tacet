// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package update

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Mode is how tacet tells the person about a new release.
type Mode string

// Modes, set with update: in the user config.
const (
	Prompt Mode = "prompt" // ask [Y/n] before an interactive command (the default)
	Notify Mode = "notify" // print one line, never ask
	Off    Mode = "off"    // never check
)

// UserConfig is ~/.config/tacet/config.yaml ($XDG_CONFIG_HOME when set),
// the settings that belong to the person rather than to one project.
func UserConfig() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "tacet", "config.yaml")
}

// Setting is the update mode: off when TACET_NO_UPDATE_NOTIFIER is set,
// else update: from the user config, else prompt.
func Setting() Mode {
	if v := os.Getenv("TACET_NO_UPDATE_NOTIFIER"); v != "" && v != "0" {
		return Off
	}
	var c struct {
		Update string `yaml:"update"`
	}
	if p := UserConfig(); p != "" {
		if b, err := os.ReadFile(p); err == nil { // #nosec G304 -- the person's own config
			_ = yaml.Unmarshal(b, &c)
		}
	}
	switch m := Mode(strings.ToLower(strings.TrimSpace(c.Update))); m {
	case Notify, Off:
		return m
	}
	return Prompt
}

// InCI reports whether tacet runs in a CI job, where it never checks.
func InCI() bool { return os.Getenv("CI") != "" }
