// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RhythmShahriar/tacet/audio"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"1.1.0", "1.0.0", true},
		{"v1.0.1", "1.0.0", true},
		{"2.0.0", "1.9.9", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.1.0", false},
		{"1.1.0", "1.1.0-rc.1", true},
		{"1.1.0-rc.2", "1.1.0-rc.1", true},
		{"1.1.0-rc.10", "1.1.0-rc.9", true},
		{"1.1.0-rc.1", "1.0.0", true},
		{"1.1.0-rc.1", "1.1.0", false},
		{"1.1.0", "dev", false},
		{"garbage", "1.0.0", false},
		{"1.0", "0.9.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestSummaryDropsControlCharacters(t *testing.T) {
	notes := "## What's Changed\n\n* feat: self-update \x1b[31mred\x1b[0m\n\n**Full Changelog**: x\n" + strings.Repeat("é", 120)
	got := Summary(notes, 8)
	if len(got) != 3 || got[0] != "What's Changed" || strings.ContainsRune(got[1], 0x1b) {
		t.Fatalf("Summary = %q", got)
	}
	if n := len([]rune(got[2])); n != 100 {
		t.Fatalf("long line has %d runes, want 100", n)
	}
}

// fixture is a fake release served by httptest.
type fixture struct {
	srv      *httptest.Server
	archive  []byte
	sums     []byte
	lookups  atomic.Int32
	tag      string
	notFound bool
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func newFixture(t *testing.T, archiveName string, archive []byte) *fixture {
	t.Helper()
	f := &fixture{archive: archive, tag: "v1.1.0"}
	f.sums = []byte(sum(archive) + "  " + archiveName + "\n" + strings.Repeat("0", 64) + "  other.tar.gz\n")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		f.lookups.Add(1)
		if f.notFound {
			http.NotFound(w, nil)
			return
		}
		base := f.srv.URL + "/dl/"
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": f.tag, "body": "## Changes\n* self-update",
			"assets": []map[string]string{
				{"name": archiveName, "browser_download_url": base + archiveName},
				{"name": ChecksumsName, "browser_download_url": base + ChecksumsName},
				{"name": BundleName, "browser_download_url": base + BundleName},
			},
		})
	})
	mux.HandleFunc("/dl/"+archiveName, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(f.archive) })
	mux.HandleFunc("/dl/"+ChecksumsName, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(f.sums) })
	mux.HandleFunc("/dl/"+BundleName, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"fake":true}`)) })
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	old := API
	API = f.srv.URL + "/api"
	t.Cleanup(func() { API = old })
	return f
}

// fakeVerifier accepts or rejects without Sigstore and records the tag.
type fakeVerifier struct {
	err error
	tag string
}

func (v *fakeVerifier) Verify(_ context.Context, sums, bundle []byte, tag string) error {
	v.tag = tag
	if len(sums) == 0 || len(bundle) == 0 {
		return errors.New("empty input")
	}
	return v.err
}

// oldBinary writes a stand-in for the running tacet.
func oldBinary(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "tacet")
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func install(t *testing.T, f *fixture, v Verifier, exe, goos string, smoke func(string, string) error) error {
	t.Helper()
	r, err := Latest(context.Background(), f.srv.Client(), "")
	if err != nil {
		t.Fatal(err)
	}
	in := &Installer{Client: f.srv.Client(), Verifier: v, Exe: exe, GOOS: goos, GOARCH: "amd64", Smoke: smoke}
	return in.Install(context.Background(), r)
}

func TestInstallReplacesAndKeepsOld(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			name := ArchiveName(goos, "amd64")
			var archive []byte
			if goos == "windows" {
				archive = zipOf(t, map[string]string{"tacet.exe": "new-binary", "README.md": "x"})
			} else {
				archive = tarGz(t, map[string]string{"tacet": "new-binary", "README.md": "x"})
			}
			f := newFixture(t, name, archive)
			exe := oldBinary(t)
			v := &fakeVerifier{}
			smoked := ""
			err := install(t, f, v, exe, goos, func(p, want string) error { smoked = readFile(t, p) + " " + want; return nil })
			if err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, exe); got != "new-binary" {
				t.Fatalf("binary = %q", got)
			}
			if got := readFile(t, OldPath(exe)); got != "old-binary" {
				t.Fatalf("tacet.old = %q", got)
			}
			if v.tag != "v1.1.0" || smoked != "new-binary 1.1.0" {
				t.Fatalf("verified tag %q, smoke %q", v.tag, smoked)
			}
			if fi, _ := os.Stat(exe); goos != "windows" && fi.Mode().Perm()&0o100 == 0 {
				t.Fatalf("new binary is not executable: %v", fi.Mode())
			}
			left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".tacet-new-*"))
			if len(left) > 0 {
				t.Fatalf("staged files left behind: %v", left)
			}
		})
	}
}

// Each failure must leave the running binary exactly as it was.
func TestInstallFailsClosed(t *testing.T) {
	name := ArchiveName("linux", "amd64")
	good := tarGz(t, map[string]string{"tacet": "new-binary"})
	cases := map[string]struct {
		setup  func(f *fixture)
		verify error
		smoke  error
		want   error
	}{
		"bad signature":    {verify: ErrSignature, want: ErrSignature},
		"tampered archive": {setup: func(f *fixture) { f.archive = tarGz(t, map[string]string{"tacet": "evil"}) }, want: audio.ErrHash},
		"no checksum line": {setup: func(f *fixture) { f.sums = []byte(strings.Repeat("0", 64) + "  other.tar.gz\n") }, want: ErrNoChecksum},
		"no binary": {setup: func(f *fixture) {
			f.archive = tarGz(t, map[string]string{"README.md": "x"})
			f.sums = []byte(sum(f.archive) + "  " + name + "\n")
		}, want: ErrNoBinary},
		"smoke fails": {smoke: errors.New("exec format error")},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			f := newFixture(t, name, good)
			if c.setup != nil {
				c.setup(f)
			}
			exe := oldBinary(t)
			err := install(t, f, &fakeVerifier{err: c.verify}, exe, "linux", func(string, string) error { return c.smoke })
			if err == nil {
				t.Fatal("install succeeded, want an error")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if got := readFile(t, exe); got != "old-binary" {
				t.Fatalf("binary was replaced: %q", got)
			}
			if _, err := os.Stat(OldPath(exe)); err == nil {
				t.Fatal("tacet.old written although nothing was replaced")
			}
		})
	}
}

func TestInstallNeedsEveryAsset(t *testing.T) {
	exe := oldBinary(t)
	in := &Installer{Client: http.DefaultClient, Verifier: &fakeVerifier{}, Exe: exe, GOOS: "linux", GOARCH: "amd64"}
	err := in.Install(context.Background(), &Release{Tag: "v1.1.0", Version: "1.1.0", Assets: map[string]string{ChecksumsName: "x"}})
	if err == nil || !strings.Contains(err.Error(), "tacet_linux_amd64.tar.gz") {
		t.Fatalf("err = %v", err)
	}
}

func TestWindowsSwapRestoresOnFailure(t *testing.T) {
	exe := oldBinary(t)
	err := swapWindows(exe, filepath.Join(filepath.Dir(exe), "missing"), OldPath(exe))
	if err == nil {
		t.Fatal("swap with a missing new binary succeeded")
	}
	if got := readFile(t, exe); got != "old-binary" {
		t.Fatalf("binary not restored: %q", got)
	}
}

func TestReplaceUnwritableDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes anywhere")
	}
	exe := oldBinary(t)
	dir := filepath.Dir(exe)
	newBin := filepath.Join(t.TempDir(), "new")
	_ = os.WriteFile(newBin, []byte("new-binary"), 0o755)
	_ = os.Chmod(dir, 0o500)
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := Replace(exe, newBin, "linux"); err == nil {
		t.Fatal("replace in a read-only folder succeeded")
	}
	if got := readFile(t, exe); got != "old-binary" {
		t.Fatalf("binary changed: %q", got)
	}
}

func TestRollback(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			exe := oldBinary(t)
			if err := Rollback(exe, goos); !errors.Is(err, ErrNoOld) {
				t.Fatalf("rollback with no tacet.old: %v", err)
			}
			newBin := filepath.Join(t.TempDir(), "new")
			_ = os.WriteFile(newBin, []byte("new-binary"), 0o755)
			if err := Replace(exe, newBin, goos); err != nil {
				t.Fatal(err)
			}
			if err := Rollback(exe, goos); err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, exe); got != "old-binary" {
				t.Fatalf("after rollback: %q", got)
			}
		})
	}
}

func TestChecksum(t *testing.T) {
	h := strings.Repeat("a", 64)
	sums := []byte(h + "  tacet_linux_amd64.tar.gz\n" + strings.Repeat("b", 64) + " *tacet_windows_amd64.zip\nshort  x\n")
	if got, err := Checksum(sums, "tacet_linux_amd64.tar.gz"); err != nil || got != h {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := Checksum(sums, "tacet_windows_amd64.zip"); err != nil {
		t.Fatal(err)
	}
	if _, err := Checksum(sums, "x"); !errors.Is(err, ErrNoChecksum) {
		t.Fatalf("short hash accepted: %v", err)
	}
}

func TestRefreshOncePerDay(t *testing.T) {
	t.Setenv("TACET_CACHE_DIR", t.TempDir())
	f := newFixture(t, ArchiveName("linux", "amd64"), nil)
	now := time.Now()
	s, err := Refresh(context.Background(), f.srv.Client(), now)
	if err != nil || s.Latest != "1.1.0" || s.Available("1.0.0") != "1.1.0" || s.Available("1.1.0") != "" {
		t.Fatalf("state %+v, err %v", s, err)
	}
	_, _ = Refresh(context.Background(), f.srv.Client(), now.Add(time.Hour))
	if n := f.lookups.Load(); n != 1 {
		t.Fatalf("%d lookups within a day, want 1", n)
	}
	_, _ = Refresh(context.Background(), f.srv.Client(), now.Add(25*time.Hour))
	if n := f.lookups.Load(); n != 2 {
		t.Fatalf("%d lookups after a day, want 2", n)
	}
}

func TestRefreshFailureStillCounts(t *testing.T) {
	t.Setenv("TACET_CACHE_DIR", t.TempDir())
	f := newFixture(t, ArchiveName("linux", "amd64"), nil)
	f.notFound = true
	if _, err := Refresh(context.Background(), f.srv.Client(), time.Now()); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("err = %v", err)
	}
	if s := Load(); s.CheckedAt.IsZero() || s.Latest != "" {
		t.Fatalf("state %+v", s)
	}
}

func TestRefreshRespectsLock(t *testing.T) {
	t.Setenv("TACET_CACHE_DIR", t.TempDir())
	f := newFixture(t, ArchiveName("linux", "amd64"), nil)
	unlock, err := lock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Refresh(context.Background(), f.srv.Client(), time.Now()); !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v, want ErrLocked", err)
	}
	unlock()
	// A lock left by a crashed process is taken over after a minute.
	unlock, _ = lock()
	_ = unlock
	old := time.Now().Add(-2 * staleLock)
	_ = os.Chtimes(lockPath(), old, old)
	if _, err := Refresh(context.Background(), f.srv.Client(), time.Now()); err != nil {
		t.Fatalf("stale lock: %v", err)
	}
	if f.lookups.Load() != 1 {
		t.Fatal("no lookup after the stale lock")
	}
}

func TestLatestRejectsBadTags(t *testing.T) {
	f := newFixture(t, ArchiveName("linux", "amd64"), nil)
	for _, tag := range []string{"1.1.0", "vnext", ""} {
		f.tag = tag
		if _, err := Latest(context.Background(), f.srv.Client(), ""); !errors.Is(err, ErrNoRelease) {
			t.Errorf("tag %q: err = %v", tag, err)
		}
	}
}

func TestSetting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("TACET_NO_UPDATE_NOTIFIER", "")
	if Setting() != Prompt {
		t.Fatal("default is not prompt")
	}
	_ = os.MkdirAll(filepath.Join(dir, "tacet"), 0o750)
	for _, m := range []Mode{Notify, Off, Prompt} {
		_ = os.WriteFile(filepath.Join(dir, "tacet", "config.yaml"), []byte(fmt.Sprintf("update: %s\n", m)), 0o600)
		if got := Setting(); got != m {
			t.Errorf("update: %s gives %s", m, got)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "tacet", "config.yaml"), []byte("update: sometimes\n"), 0o600)
	if Setting() != Prompt {
		t.Error("an unknown mode is not prompt")
	}
	t.Setenv("TACET_NO_UPDATE_NOTIFIER", "1")
	if Setting() != Off {
		t.Error("TACET_NO_UPDATE_NOTIFIER=1 does not turn it off")
	}
}

func TestDetect(t *testing.T) {
	home := t.TempDir()
	gobin := filepath.Join(t.TempDir(), "gobin")
	env := func(k string) string {
		if k == "GOBIN" {
			return gobin
		}
		return ""
	}
	cases := map[string]string{
		filepath.Join(home, ".local", "bin", "tacet"):       "release",
		filepath.Join(home, "go", "bin", "tacet"):           "go",
		filepath.Join(gobin, "tacet"):                       "go",
		"/opt/homebrew/bin/tacet":                           "brew",
		"/home/linuxbrew/.linuxbrew/Cellar/tacet/1.0/tacet": "brew",
		"/nix/store/abc-tacet/bin/tacet":                    "nix",
		"/usr/bin/tacet":                                    "package",
		"/usr/local/bin/tacet":                              "release",
		`C:/Users/me/scoop/apps/tacet/current/tacet.exe`:    "scoop",
	}
	for exe, want := range cases {
		m := Detect(exe, env, home)
		if m.Name != want {
			t.Errorf("Detect(%s) = %s, want %s", exe, m.Name, want)
		}
		if m.SelfManaged() != (want == "release") {
			t.Errorf("Detect(%s).SelfManaged() = %v", exe, m.SelfManaged())
		}
	}
	if m := Detect(filepath.Join(home, "go", "bin", "tacet"), env, home); m.Command != GoInstall {
		t.Errorf("go install command = %q", m.Command)
	}
}
