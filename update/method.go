// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package update

import (
	"os"
	"path/filepath"
	"strings"
)

// GoInstall is how a go install user updates.
const GoInstall = "go install -tags gotreesitter_no_copyleft github.com/" + Repo + "/cmd/tacet@latest"

// Method is how this tacet binary was installed.
type Method struct {
	Name    string // release, go, brew, scoop, winget, nix, package
	Command string // what to run instead of tacet update; empty for release
}

// SelfManaged reports whether tacet update may replace the binary: true
// for the install scripts and release archives.
func (m Method) SelfManaged() bool { return m.Command == "" }

// Detect works out how exe was installed from where it lives. env reads
// the environment (os.Getenv in tacet) and home is the user's home.
func Detect(exe string, env func(string) string, home string) Method {
	p := filepath.ToSlash(filepath.Clean(exe))
	lp := strings.ToLower(p)
	dir := filepath.Dir(filepath.Clean(exe))
	switch {
	case strings.Contains(lp, "/cellar/") || strings.Contains(lp, "/homebrew/") || strings.Contains(lp, "/linuxbrew/"):
		return Method{"brew", "brew upgrade tacet"}
	case strings.Contains(lp, "/scoop/"):
		return Method{"scoop", "scoop update tacet"}
	case strings.Contains(lp, "/winget/"):
		return Method{"winget", "winget upgrade tacet"}
	case strings.HasPrefix(lp, "/nix/store/"):
		return Method{"nix", "update tacet through your nix configuration"}
	}
	for _, d := range goBins(env, home) {
		if samePath(dir, d) {
			return Method{"go", GoInstall}
		}
	}
	for _, d := range []string{"/usr/bin", "/usr/sbin", "/bin", "/snap/bin"} {
		if p == d+"/"+filepath.Base(p) {
			return Method{"package", "update tacet with the package manager that installed it"}
		}
	}
	return Method{Name: "release"}
}

// goBins are the folders go install writes to.
func goBins(env func(string) string, home string) []string {
	var out []string
	if b := env("GOBIN"); b != "" {
		out = append(out, b)
	}
	gp := env("GOPATH")
	if gp == "" && home != "" {
		gp = filepath.Join(home, "go")
	}
	for _, g := range filepath.SplitList(gp) {
		if g != "" {
			out = append(out, filepath.Join(g, "bin"))
		}
	}
	return out
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}
