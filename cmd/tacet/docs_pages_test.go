// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/RhythmShahriar/tacet/internal/config"
)

// The docs live in docs/ at the repo root. These tests fail when a
// command, flag or feature is added, renamed or removed without its page.

const docsDir = "../../docs"

func readDoc(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(docsDir, filepath.FromSlash(rel))) // #nosec G304 -- the repo's own docs
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func pageNames(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(docsDir, dir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range m {
		out = append(out, strings.TrimSuffix(filepath.Base(p), ".md"))
	}
	sort.Strings(out)
	return out
}

func TestEveryCommandHasAPage(t *testing.T) {
	var want []string
	for _, c := range commands {
		if c.hidden {
			continue
		}
		want = append(want, c.name)
		page := readDoc(t, "commands/"+c.name+".md")
		if !strings.HasPrefix(page, "# tacet "+c.name+"\n") {
			t.Errorf("docs/commands/%s.md should start with \"# tacet %s\"", c.name, c.name)
		}
	}
	sort.Strings(want)
	if got := pageNames(t, "commands"); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("docs/commands has pages %v, the commands are %v", got, want)
	}
}

func TestEveryFeatureHasAPage(t *testing.T) {
	for _, f := range config.FeatureNames() {
		page := readDoc(t, "features/"+f+".md")
		if !strings.HasPrefix(page, "# "+f+"\n") {
			t.Errorf("docs/features/%s.md should start with \"# %s\"", f, f)
		}
		def := "**Default: off.**"
		if config.Features[f] {
			def = "**Default: on.**"
		}
		if !strings.Contains(page, def) {
			t.Errorf("docs/features/%s.md should say %s", f, def)
		}
	}
	if got, want := pageNames(t, "features"), config.FeatureNames(); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("docs/features has pages %v, the features are %v", got, want)
	}
}

func TestIndexLinksEveryPage(t *testing.T) {
	index := readDoc(t, "README.md")
	for _, dir := range []string{"commands", "features"} {
		for _, name := range pageNames(t, dir) {
			if !strings.Contains(index, "("+dir+"/"+name+".md)") {
				t.Errorf("docs/README.md doesn't link %s/%s.md", dir, name)
			}
		}
	}
}

// commandSources are the files that parse each command's flags.
var commandSources = map[string][]string{
	"init":       {"initcmd.go"},
	"map":        {"mapcmd.go"},
	"docs":       {"docs.go"},
	"skills":     {"skillscmd.go", "skills_add.go"},
	"clean":      {"clean.go"},
	"check":      {"commands.go"},
	"transcribe": {"transcribe.go"},
	"report":     {"report.go"},
	"statusline": {"statusline.go"},
	"update":     {"update.go"},
}

// internalFlags are passed by tacet to itself, not by people.
var internalFlags = map[string]bool{
	"--summary": true, // clean: the session-start disk summary
	"--update":  true, // map: the background index refresh
	"--refresh": true, // update: the background release check
	"--plugin":  true, // update: the new binary rewrites the plugin
	"--after":   true, // statusline: written by --install --chain
	"--json":    true, // global, on every page through the index
}

var flagRe = regexp.MustCompile(`"(--[a-z][a-z-]*)`)

func TestEveryFlagIsDocumented(t *testing.T) {
	for name, files := range commandSources {
		page := readDoc(t, "commands/"+name+".md")
		for _, f := range files {
			src, err := os.ReadFile(f) // #nosec G304 -- the package's own source
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range flagRe.FindAllStringSubmatch(string(src), -1) {
				if flag := m[1]; !internalFlags[flag] && !strings.Contains(page, "`"+flag) {
					t.Errorf("docs/commands/%s.md doesn't document %s (from %s)", name, flag, f)
				}
			}
		}
	}
}

var linkRe = regexp.MustCompile(`\]\(([^)#\s]+)(#[^)]*)?\)`)

func TestDocsLinksResolve(t *testing.T) {
	err := filepath.WalkDir(docsDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		b, err := os.ReadFile(p) // #nosec G304 -- the repo's own docs
		if err != nil {
			return err
		}
		for _, m := range linkRe.FindAllStringSubmatch(string(b), -1) {
			target := m[1]
			if strings.Contains(target, "://") {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(p), filepath.FromSlash(target))); err != nil {
				t.Errorf("%s links %s, which doesn't exist", p, target)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
