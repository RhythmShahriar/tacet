// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/RhythmShahriar/tacet/skills"
	"github.com/RhythmShahriar/tacet/tokens"
)

const skillsAddUsage = "tacet skills add <owner/repo/path[@ref] | SKILL.md URL> [--allow-scripts] | --list"

var skillsClient = &http.Client{Timeout: time.Minute}

// errScripts stops an import that carries code the person didn't allow.
var errScripts = errors.New("skill has scripts or binaries")

// runSkillsAdd imports a public skill pinned to a commit, after a yes.
// --json shows what would be written and writes only with --yes.
func runSkillsAdd(a *app, args []string) error {
	if hasFlag(&args, "--list") {
		return listSkillExamples(a)
	}
	allow := hasFlag(&args, "--allow-scripts")
	if len(args) != 1 {
		return fmt.Errorf("%w: %s", errUsage, skillsAddUsage)
	}
	src, err := skills.ParseSource(args[0])
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	im, err := skills.Fetch(ctx, skillsClient, src)
	if err != nil {
		return err
	}
	root := a.root()
	dest := filepath.Join(".claude", "skills", im.Name)
	scripts := im.Scripts()
	if !a.json {
		showImport(a, im, dest)
	}
	if len(scripts) > 0 && !allow {
		names := make([]string, 0, len(scripts))
		for _, f := range scripts {
			names = append(names, f.Path)
		}
		return fmt.Errorf("%w: %s; read them, then rerun with --allow-scripts", errScripts, strings.Join(names, ", "))
	}
	cost := descriptionTokens(im)
	if a.json && !a.yes {
		return a.writeJSON(map[string]any{"skill": im, "path": dest, "description_tokens": cost, "written": false})
	}
	ok, err := confirm(a, "Write "+dest+"?")
	if err != nil || !ok {
		return err
	}
	dir, err := im.Write(root, allow)
	if err != nil {
		return err
	}
	fs := importChecks(root, filepath.Join(dir, "SKILL.md"))
	if a.json {
		msgs := []string{}
		for _, f := range fs {
			msgs = append(msgs, f.Message)
		}
		return a.writeJSON(map[string]any{"skill": im, "path": dest, "description_tokens": cost, "written": true, "findings": msgs})
	}
	fmt.Fprintf(a.out, "%s wrote %s at %s: its description costs about %s tokens every session; type /%s to use it\n",
		ui.Good.Render("✓"), dest, shortCommit(im.Commit), ui.Tokens(cost), im.Name)
	for _, f := range fs {
		fmt.Fprintf(a.out, "  %s  %s\n", ui.Brand.Render(fmt.Sprintf("%-18s", f.Rule)), f.Message)
		if f.Hint != "" {
			fmt.Fprintf(a.out, "    %s\n", ui.Dim.Render("→ "+f.Hint))
		}
	}
	return nil
}

func showImport(a *app, im *skills.Import, dest string) {
	w := a.out
	fmt.Fprintln(w, ui.Section("import skill", 1, width()))
	fmt.Fprintf(w, "  %s  %s\n", im.Origin, ui.Dim.Render("at "+shortCommit(im.Commit)))
	fmt.Fprintf(w, "  %s\n\n", ui.Dim.Render("→ "+dest))
	for _, k := range []string{"name", "description", "when_to_use", "license", "allowed-tools"} {
		if v := im.Frontmatter[k]; v != "" {
			fmt.Fprintf(w, "    %s %s\n", ui.Dim.Render(k+":"), short100(v))
		}
	}
	fmt.Fprintln(w)
	for _, f := range im.Files {
		mark := ""
		if f.Script != "" {
			mark = "  " + ui.Bad.Render(f.Script)
		}
		fmt.Fprintf(w, "    %-44s %8s%s\n", f.Path, byteSize(f.Size), mark)
	}
	fmt.Fprintf(w, "    %s\n\n", ui.Dim.Render(fmt.Sprintf("%d file(s), %s", len(im.Files), byteSize(im.Size()))))
}

func listSkillExamples(a *app) error {
	if a.json {
		return a.writeJSON(map[string]any{"note": "examples, not endorsements", "examples": skills.Examples})
	}
	w := a.out
	fmt.Fprintln(w, ui.Section("public skills", len(skills.Examples), width()))
	fmt.Fprintf(w, "  %s\n\n", ui.Dim.Render("examples, not endorsements: read a skill before you add it"))
	for _, e := range skills.Examples {
		fmt.Fprintf(w, "  %s\n    %s\n", ui.Brand.Render(e.Source), ui.Dim.Render(e.About))
	}
	fmt.Fprintf(w, "\n  %s\n", ui.Dim.Render("tacet skills add <source> shows a skill before writing it"))
	return nil
}

// descriptionTokens is what the skill's listing entry costs each session.
func descriptionTokens(im *skills.Import) int {
	d := im.Description()
	if r := []rune(d); len(r) > 1536 {
		d = string(r[:1536])
	}
	return tokens.Estimate(im.Name + ": " + d)
}

// importChecks runs tacet skills check and keeps what it says about the
// new skill. A skill added a moment ago can't have been used yet, so the
// unused rule is left out.
func importChecks(root, skillPath string) []core.Finding {
	fs, err := skillChecks(root)
	if err != nil {
		return nil
	}
	want := relPath(root, skillPath)
	var out []core.Finding
	for _, f := range fs {
		if f.Path == want && f.Rule != "skills/unused" {
			out = append(out, f)
		}
	}
	return out
}

func shortCommit(c string) string {
	if strings.HasPrefix(c, "sha256:") && len(c) > 19 {
		return c[:19]
	}
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

func byteSize(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1f kB", float64(n)/1000)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/1_000_000)
}
