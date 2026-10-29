// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package config reads and writes .tacet/config.yaml and finds the
// project tacet works on.
package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Features tacet can switch on and off, with their defaults.
var Features = map[string]bool{
	"convert":  true,  // PDF and Office files read as Markdown, long ones on an outline
	"audio":    true,  // audio read as a transcript (asks before the first download)
	"guard":    true,  // block reads of secrets
	"handoff":  true,  // SESSION.md written before compact and at session end
	"map":      true,  // code map for the MCP tools
	"skills":   true,  // suggest skills from repeated instructions
	"feedback": false, // lint and type errors shown right after an edit
	"testgate": false, // failing tests keep the agent from finishing

	"bigoutput": false, // experimental: keep long Bash and WebFetch output in .tacet/out
}

// FeatureNames lists the features in a stable order.
func FeatureNames() []string {
	out := make([]string, 0, len(Features))
	for k := range Features {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Config is .tacet/config.yaml.
type Config struct {
	Features map[string]bool `yaml:"features"`
	// Sessions between quiet re-audits run by the session-end hook.
	AuditEvery int `yaml:"audit_every,omitempty"`
}

// Dir is the project's .tacet folder.
func Dir(root string) string { return filepath.Join(root, ".tacet") }

func path(root string) string { return filepath.Join(Dir(root), "config.yaml") }

// Load reads the config, filling defaults for anything unset. Features
// tacet no longer has (speak, trim, outline) are dropped, so an old
// config still loads.
func Load(root string) *Config {
	c := &Config{Features: map[string]bool{}}
	if b, err := os.ReadFile(path(root)); err == nil {
		_ = yaml.Unmarshal(b, c)
	}
	if c.Features == nil {
		c.Features = map[string]bool{}
	}
	for k := range c.Features {
		if _, ok := Features[k]; !ok {
			delete(c.Features, k)
		}
	}
	for k, v := range Features {
		if _, ok := c.Features[k]; !ok {
			c.Features[k] = v
		}
	}
	if c.AuditEvery <= 0 {
		c.AuditEvery = 10
	}
	return c
}

// On reports whether a feature is on.
func (c *Config) On(f string) bool { return c.Features[f] }

// Save writes the config.
func (c *Config) Save(root string) error {
	if err := os.MkdirAll(Dir(root), 0o750); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	head := "# tacet settings for this project. tacet on|off <feature> edits this file.\n"
	return os.WriteFile(path(root), append([]byte(head), b...), 0o600)
}

// Initialized reports whether tacet init ran here.
func Initialized(root string) bool {
	_, err := os.Stat(path(root))
	return err == nil
}

// ErrUnknownFeature is returned for a feature name tacet doesn't have.
var ErrUnknownFeature = errors.New("unknown feature")

// Set turns a feature on or off.
func (c *Config) Set(f string, on bool) error {
	if _, ok := Features[f]; !ok {
		return ErrUnknownFeature
	}
	c.Features[f] = on
	return nil
}

// Root is the project tacet works on: the git top level above dir, else dir.
func Root(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	cmd := exec.Command("git", "-C", abs, "rev-parse", "--show-toplevel") // #nosec G204 -- fixed git subcommand
	if out, err := cmd.Output(); err == nil {
		if top := strings.TrimSpace(string(out)); top != "" {
			return filepath.Clean(top)
		}
	}
	return abs
}

// Exclude adds .tacet/ to .git/info/exclude so tacet's state never shows
// up in git status, without touching the project's .gitignore.
func Exclude(root string) error { return ExcludePath(root, ".tacet/") }

// ExcludePath adds one pattern to .git/info/exclude when it isn't there.
func ExcludePath(root, pattern string) error {
	gitDir := filepath.Join(root, ".git")
	fi, err := os.Stat(gitDir)
	if err != nil || !fi.IsDir() {
		return nil // not a git checkout, or a worktree: nothing to do
	}
	p := filepath.Join(gitDir, "info", "exclude")
	b, _ := os.ReadFile(p) // #nosec G304 -- the project's own exclude file
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == pattern {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	b = append(b, []byte("# added by tacet\n"+pattern+"\n")...)
	return os.WriteFile(p, b, 0o600) // #nosec G703 -- the project's .git/info/exclude
}
