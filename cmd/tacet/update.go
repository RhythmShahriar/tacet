// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/RhythmShahriar/tacet/internal/ui"
	"github.com/RhythmShahriar/tacet/plugin"
	"github.com/RhythmShahriar/tacet/update"
)

// stdin is where the [Y/n] answer is read; tests swap it.
var stdin io.Reader = os.Stdin

// updateClient downloads releases. The lookup has its own 2-second
// timeout; this one bounds a whole archive.
var updateClient = &http.Client{Timeout: 5 * time.Minute}

// newVerifier checks release signatures and smokeTest runs the new
// binary before it goes in place; tests swap both.
var (
	newVerifier = func() update.Verifier { return update.Sigstore{} }
	smokeTest   = smoke
)

// currentVersion is the release this binary was built from: the stamped
// version, else the module version go install records, else dev.
func currentVersion() string {
	if update.Valid(version) {
		return update.Clean(version)
	}
	if bi, ok := debug.ReadBuildInfo(); ok && update.Valid(bi.Main.Version) {
		return update.Clean(bi.Main.Version)
	}
	return version
}

func selfPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func installMethod(exe string) update.Method {
	home, _ := os.UserHomeDir()
	return update.Detect(exe, os.Getenv, home)
}

// runUpdate is tacet update [--check] [--version v] [--rollback].
func runUpdate(a *app, args []string) error {
	var check, rollback bool
	var tag string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--check":
			check = true
		case "--rollback":
			rollback = true
		case "--version":
			if i+1 >= len(args) {
				return fmt.Errorf("%w: tacet update --version <version>", errUsage)
			}
			tag = args[i+1]
			i++
		case "--refresh": // the background daily check
			if updateChecks() {
				_, _ = update.Refresh(context.Background(), http.DefaultClient, time.Now())
			}
			return nil
		case "--plugin": // the new binary rewrites the plugin after an update
			return reinstallPlugin()
		default:
			return fmt.Errorf("%w: tacet update [--check] [--version <version>] [--rollback]", errUsage)
		}
	}
	exe, err := selfPath()
	if err != nil {
		return err
	}
	if rollback {
		if err := update.Rollback(exe, runtime.GOOS); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "restored the previous tacet at %s\n", exe)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), update.LookupTimeout)
	r, err := update.Latest(ctx, http.DefaultClient, tag)
	cancel()
	if err != nil {
		return fmt.Errorf("looking up the latest release: %w", err)
	}
	if tag == "" {
		s := update.Load()
		s.CheckedAt, s.Latest, s.Notes = time.Now(), r.Version, r.Notes
		_ = update.Save(s)
	}

	cur := currentVersion()
	if tag == "" && !update.Newer(r.Version, cur) {
		if a.json {
			return a.writeJSON(map[string]any{"current": cur, "latest": r.Version, "update": false})
		}
		fmt.Fprintf(a.out, "tacet %s is the latest release\n", cur)
		return nil
	}
	m := installMethod(exe)
	if a.json || check {
		if a.json {
			return a.writeJSON(map[string]any{"current": cur, "latest": r.Version, "update": true, "command": updateCommand(m)})
		}
		fmt.Fprintf(a.out, "tacet %s → %s: run %s\n", cur, r.Version, updateCommand(m))
		return nil
	}
	return offerUpdate(a, exe, m, cur, r, true)
}

// updateCommand is what the person runs to update this install.
func updateCommand(m update.Method) string {
	if m.SelfManaged() {
		return "tacet update"
	}
	return m.Command
}

// offerUpdate shows the versions and notes, asks [Y/n] (unless --yes)
// and installs. notes is false for the short prompt before a command.
func offerUpdate(a *app, exe string, m update.Method, cur string, r *update.Release, notes bool) error {
	w := a.out
	fmt.Fprintf(w, "%s %s → %s\n", ui.Brand.Render("tacet"), cur, ui.Good.Render(r.Version))
	if notes {
		for _, l := range update.Summary(r.Notes, 8) {
			fmt.Fprintln(w, "  "+ui.Dim.Render(l))
		}
	}
	if !m.SelfManaged() {
		fmt.Fprintf(w, "tacet was installed with %s. To update, run:\n  %s\n", m.Name, m.Command)
		return nil
	}
	if !a.yes && !askYes(w, "Update now? [Y/n] ") {
		s := update.Load()
		s.AskedAt = time.Now()
		_ = update.Save(s)
		fmt.Fprintln(w, "Not now. Run tacet update when you're ready.")
		return nil
	}
	return installRelease(a, exe, r)
}

// askYes reads one answer; empty, y and yes mean yes.
func askYes(w io.Writer, q string) bool {
	fmt.Fprint(w, q)
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(w)
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "y", "yes":
		return true
	}
	return false
}

func installRelease(a *app, exe string, r *update.Release) error {
	in := &update.Installer{
		Client: updateClient, Verifier: newVerifier(), Exe: exe,
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Smoke: smokeTest, Log: a.err,
	}
	if err := in.Install(context.Background(), r); err != nil {
		return fmt.Errorf("update stopped, nothing was replaced: %w", err)
	}
	fmt.Fprintf(a.out, "%s tacet %s installed at %s (the previous one is %s)\n",
		ui.Good.Render("✓"), r.Version, exe, filepath.Base(update.OldPath(exe)))
	if _, err := os.Stat(plugin.Dir()); err == nil {
		if err := refreshPlugin(exe, r.Version); err != nil {
			fmt.Fprintf(a.err, "the plugin wasn't rewritten (%v); run tacet init in a project to fix it\n", err)
		} else {
			fmt.Fprintln(a.out, "  plugin and /tacet updated; restart Claude Code to load them")
		}
	}
	return nil
}

// smoke runs the new binary's version command before it goes in place.
func smoke(path, want string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output() // #nosec G204 -- the verified release binary
	if err != nil {
		return err
	}
	if !strings.Contains(string(out), want) {
		return fmt.Errorf("it reports %q, expected %s", strings.TrimSpace(string(out)), want)
	}
	return nil
}

// refreshPlugin lets the new binary write its own plugin and skill, so
// the hooks match it. A binary from before tacet update existed can't,
// so this one writes them instead.
func refreshPlugin(exe, newVersion string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := exec.CommandContext(ctx, exe, "update", "--plugin").Run(); err == nil { // #nosec G204 -- tacet itself
		return nil
	}
	return plugin.Install(exe, newVersion)
}

func reinstallPlugin() error {
	exe, err := selfPath()
	if err != nil {
		return err
	}
	if err := plugin.Install(exe, currentVersion()); err != nil && !errors.Is(err, plugin.ErrNoClaude) {
		return err
	}
	return nil
}

// updateChecks reports whether tacet may look for releases at all.
func updateChecks() bool {
	return update.Setting() != update.Off && !update.InCI() && !testing.Testing()
}

// startRefresh runs the daily check in a detached process when it is
// due, so no command waits on the network.
func startRefresh() {
	if !updateChecks() || !update.Load().Due(time.Now()) {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "update", "--refresh") // #nosec G204 -- tacet itself
	detach(cmd)
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

// skipsUpdatePrompt lists commands that run before the prompt can help.
var skipsUpdatePrompt = map[string]bool{"update": true, "version": true, "uninstall": true}

// beforeCommand is the oh-my-zsh style prompt: in a terminal, outside
// CI, when the cached check found a newer release, ask before running
// the command. It never blocks on the network; the check itself runs
// in the background.
func beforeCommand(a *app, name string) {
	if a.json || skipsUpdatePrompt[name] || !interactive() || !updateChecks() {
		return
	}
	startRefresh()
	cur := currentVersion()
	s := update.Load()
	latest := s.Available(cur)
	if latest == "" {
		return
	}
	exe, err := selfPath()
	if err != nil {
		return
	}
	if time.Since(s.AskedAt) < update.CheckEvery {
		return // asked or reminded in the last day
	}
	s.AskedAt = time.Now()
	_ = update.Save(s)
	m := installMethod(exe)
	if update.Setting() == update.Notify || !m.SelfManaged() || a.yes {
		fmt.Fprintln(a.err, ui.Dim.Render(fmt.Sprintf("tacet %s is out: run %s", latest, updateCommand(m))))
		return
	}
	fmt.Fprintf(a.out, "tacet %s is out. ", latest)
	if !askYes(a.out, "Update now? [Y/n] ") {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), update.LookupTimeout)
	r, err := update.Latest(ctx, http.DefaultClient, "")
	cancel()
	switch {
	case err == nil && !update.Newer(r.Version, cur):
		fmt.Fprintf(a.out, "tacet %s is already the latest release\n", cur)
	case err == nil:
		err = installRelease(a, exe, r)
	}
	if err != nil {
		fmt.Fprintln(a.err, "tacet:", err)
	}
	fmt.Fprintln(a.out)
}

// updateNotice is the one-time session message for a new release, ""
// when there is none or it was already shown. Hooks never prompt.
func updateNotice() string {
	if update.Setting() == update.Off || update.InCI() {
		return ""
	}
	startRefresh()
	s := update.Load()
	latest := s.Available(currentVersion())
	if latest == "" || s.Notified == latest {
		return ""
	}
	s.Notified = latest
	if update.Save(s) != nil {
		return "" // can't remember it, so don't repeat it every session
	}
	cmd := "tacet update"
	if exe, err := selfPath(); err == nil {
		cmd = updateCommand(installMethod(exe))
	}
	return fmt.Sprintf("tacet %s is available: run %s", latest, cmd)
}

// updateInfo is the update line in tacet status and /tacet.
type updateInfo struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	Command string `json:"command"`
}

func updateStatus() *updateInfo {
	if update.Setting() == update.Off || update.InCI() {
		return nil
	}
	cur := currentVersion()
	latest := update.Load().Available(cur)
	if latest == "" {
		return nil
	}
	cmd := "tacet update"
	if exe, err := selfPath(); err == nil {
		cmd = updateCommand(installMethod(exe))
	}
	return &updateInfo{Current: cur, Latest: latest, Command: cmd}
}
