// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/update"
)

// updateEnv isolates the update state and settings and pretends this
// is tacet 1.0.0.
func updateEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TACET_CACHE_DIR", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TACET_NO_UPDATE_NOTIFIER", "")
	t.Setenv("CI", "")
	old := version
	version = "1.0.0"
	t.Cleanup(func() { version = old })
}

func setStdin(t *testing.T, s string) {
	t.Helper()
	old := stdin
	stdin = strings.NewReader(s)
	t.Cleanup(func() { stdin = old })
}

func TestSessionStartAnnouncesEachVersionOnce(t *testing.T) {
	updateEnv(t)
	old := refreshMap
	refreshMap = func(string) {}
	t.Cleanup(func() { refreshMap = old })
	root := t.TempDir()
	cfg := config.Load(root)
	msg := func() string {
		out, _ := sessionStart(root, cfg, &hookInput{}).(map[string]any)
		s, _ := out["systemMessage"].(string)
		return s
	}
	if m := msg(); m != "" {
		t.Fatalf("no release cached, got %q", m)
	}
	_ = update.Save(update.State{CheckedAt: time.Now(), Latest: "1.1.0"})
	if m := msg(); !strings.Contains(m, "tacet 1.1.0 is available: run ") {
		t.Fatalf("first session: %q", m)
	}
	if m := msg(); m != "" {
		t.Fatalf("second session repeated it: %q", m)
	}
	s := update.Load()
	s.Latest = "1.2.0"
	_ = update.Save(s)
	if m := msg(); !strings.Contains(m, "1.2.0") {
		t.Fatalf("a newer release: %q", m)
	}
	s.Latest, s.Notified = "1.3.0", ""
	_ = update.Save(s)
	t.Setenv("TACET_NO_UPDATE_NOTIFIER", "1")
	if m := msg(); m != "" {
		t.Fatalf("notifier off: %q", m)
	}
	t.Setenv("TACET_NO_UPDATE_NOTIFIER", "")
	t.Setenv("CI", "true")
	if m := msg(); m != "" {
		t.Fatalf("in CI: %q", m)
	}
}

func TestUpdateStatus(t *testing.T) {
	updateEnv(t)
	if u := updateStatus(); u != nil {
		t.Fatalf("nothing cached: %+v", u)
	}
	_ = update.Save(update.State{CheckedAt: time.Now(), Latest: "1.1.0"})
	if u := updateStatus(); u == nil || u.Latest != "1.1.0" || u.Current != "1.0.0" || u.Command == "" {
		t.Fatalf("got %+v", u)
	}
	_ = os.MkdirAll(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "tacet"), 0o750)
	_ = os.WriteFile(update.UserConfig(), []byte("update: off\n"), 0o600)
	if u := updateStatus(); u != nil {
		t.Fatalf("update: off still reports %+v", u)
	}
}

func TestAskYes(t *testing.T) {
	for in, want := range map[string]bool{"\n": true, "y\n": true, "YES\n": true, "n\n": false, "no\n": false, "": false, "later\n": false} {
		setStdin(t, in)
		if got := askYes(&bytes.Buffer{}, "? "); got != want {
			t.Errorf("answer %q: %v, want %v", in, got, want)
		}
	}
}

// Hooks and non-terminals never see the prompt: run reads no stdin.
func TestNoPromptWithoutTerminal(t *testing.T) {
	updateEnv(t)
	_ = update.Save(update.State{CheckedAt: time.Now(), Latest: "1.1.0"})
	r := &countingReader{}
	old := stdin
	stdin = r
	t.Cleanup(func() { stdin = old })
	var out, errw bytes.Buffer
	run([]string{"version"}, &out, &errw)
	run([]string{"-C", t.TempDir(), "status"}, &out, &errw)
	if r.n > 0 || strings.Contains(out.String(), "Update now?") {
		t.Fatalf("prompted without a terminal: %q", out.String())
	}
}

type countingReader struct{ n int }

func (c *countingReader) Read([]byte) (int, error) { c.n++; return 0, errors.New("no input") }

func TestOfferUpdateOtherInstallPrintsCommand(t *testing.T) {
	updateEnv(t)
	setStdin(t, "y\n")
	var out bytes.Buffer
	a := &app{out: &out, err: &out}
	r := &update.Release{Tag: "v1.1.0", Version: "1.1.0", Notes: "* new"}
	m := update.Method{Name: "brew", Command: "brew upgrade tacet"}
	if err := offerUpdate(a, "/nonexistent/tacet", m, "1.0.0", r, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "brew upgrade tacet") || strings.Contains(out.String(), "Update now?") {
		t.Fatalf("output: %q", out.String())
	}
}

func TestOfferUpdateDeclined(t *testing.T) {
	updateEnv(t)
	setStdin(t, "n\n")
	var out bytes.Buffer
	a := &app{out: &out, err: &out}
	exe := filepath.Join(t.TempDir(), "tacet")
	_ = os.WriteFile(exe, []byte("old-binary"), 0o755)
	r := &update.Release{Tag: "v1.1.0", Version: "1.1.0"}
	if err := offerUpdate(a, exe, update.Method{Name: "release"}, "1.0.0", r, true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old-binary" || update.Load().AskedAt.IsZero() {
		t.Fatalf("declined update changed something: %q", out.String())
	}
}

type okVerifier struct{ err error }

func (v okVerifier) Verify(context.Context, []byte, []byte, string) error { return v.err }

// releaseServer serves a 1.1.0 release for this system.
func releaseServer(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the zip path is covered in the update package")
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "tacet", Mode: 0o755, Size: 10, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("new-binary"))
	_ = tw.Close()
	_ = gz.Close()
	archive := buf.Bytes()
	h := sha256.Sum256(archive)
	an := update.ArchiveName(runtime.GOOS, runtime.GOARCH)
	sums := hex.EncodeToString(h[:]) + "  " + an + "\n"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v1.1.0", "body": "## Changes\n* self-update", "assets": []map[string]string{
				{"name": an, "browser_download_url": srv.URL + "/a"},
				{"name": update.ChecksumsName, "browser_download_url": srv.URL + "/s"},
				{"name": update.BundleName, "browser_download_url": srv.URL + "/b"},
			}})
		case "/a":
			_, _ = w.Write(archive)
		case "/s":
			_, _ = w.Write([]byte(sums))
		case "/b":
			_, _ = w.Write([]byte("{}"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := update.API
	update.API = srv.URL + "/api"
	t.Cleanup(func() { update.API = old })
}

func TestRunUpdateCheck(t *testing.T) {
	updateEnv(t)
	releaseServer(t)
	var out, errw bytes.Buffer
	if c := run([]string{"update", "--check"}, &out, &errw); c != exitOK {
		t.Fatalf("exit %d: %s", c, errw.String())
	}
	if !strings.Contains(out.String(), "1.0.0 → 1.1.0") {
		t.Fatalf("output: %q", out.String())
	}
	if update.Load().Latest != "1.1.0" {
		t.Fatal("the lookup wasn't cached")
	}
	out.Reset()
	version = "1.1.0"
	run([]string{"update"}, &out, &errw)
	if !strings.Contains(out.String(), "latest release") {
		t.Fatalf("up to date: %q", out.String())
	}
}

func TestInstallRelease(t *testing.T) {
	updateEnv(t)
	releaseServer(t)
	oldV, oldS := newVerifier, smokeTest
	t.Cleanup(func() { newVerifier, smokeTest = oldV, oldS })
	smokeTest = func(string, string) error { return nil }
	r, err := update.Latest(context.Background(), http.DefaultClient, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		verr error
		want string
	}{{update.ErrSignature, "old-binary"}, {nil, "new-binary"}} {
		newVerifier = func() update.Verifier { return okVerifier{c.verr} }
		exe := filepath.Join(t.TempDir(), "tacet")
		_ = os.WriteFile(exe, []byte("old-binary"), 0o755)
		var out bytes.Buffer
		err := installRelease(&app{out: &out, err: &out}, exe, r)
		if (c.verr != nil) != (err != nil) {
			t.Fatalf("verifier %v: err %v", c.verr, err)
		}
		if b, _ := os.ReadFile(exe); string(b) != c.want {
			t.Fatalf("verifier %v: binary %q", c.verr, b)
		}
	}
}
