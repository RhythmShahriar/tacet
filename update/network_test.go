// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package update

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// These tests check the real v1.0.0 release on GitHub and Sigstore's
// public-good instance. They run only with TACET_NETWORK=1.
func needNetwork(t *testing.T) {
	t.Helper()
	if os.Getenv("TACET_NETWORK") != "1" {
		t.Skip("set TACET_NETWORK=1 to check the real release")
	}
}

func realRelease(t *testing.T) (*Release, []byte, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Minute}
	r, err := Latest(ctx, client, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	sums, err := fetch(ctx, client, r.Assets[ChecksumsName], 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := fetch(ctx, client, r.Assets[BundleName], 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return r, sums, bundle
}

func TestRealReleaseSignature(t *testing.T) {
	needNetwork(t)
	r, sums, bundle := realRelease(t)
	v := Sigstore{CacheDir: t.TempDir()}
	if err := v.Verify(context.Background(), sums, bundle, r.Tag); err != nil {
		t.Fatalf("the real v1.0.0 release does not verify: %v", err)
	}
	tampered := bytes.Replace(sums, []byte("tacet_linux_amd64"), []byte("tacet_linux_amd65"), 1)
	if err := v.Verify(context.Background(), tampered, bundle, r.Tag); !errors.Is(err, ErrSignature) {
		t.Fatalf("a tampered checksums.txt verified: %v", err)
	}
	if err := v.Verify(context.Background(), sums, bundle, "v9.9.9"); !errors.Is(err, ErrSignature) {
		t.Fatalf("the bundle verified for another tag: %v", err)
	}
}

// TestRealInstall installs the real release over a copy in a temp folder.
func TestRealInstall(t *testing.T) {
	needNetwork(t)
	r, _, _ := realRelease(t)
	exe := filepath.Join(t.TempDir(), "tacet")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	in := &Installer{
		Client: &http.Client{Timeout: 5 * time.Minute}, Verifier: Sigstore{CacheDir: t.TempDir()},
		Exe: exe, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Smoke: func(p, want string) error {
			out, err := exec.Command(p, "version").Output() // #nosec G204 -- the verified test download
			if err == nil && !strings.Contains(string(out), want) {
				t.Errorf("version prints %q, want %s", out, want)
			}
			return err
		},
	}
	if err := in.Install(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(exe, "version").Output() // #nosec G204 -- the verified test download
	if err != nil || !strings.Contains(string(out), "1.0.0") {
		t.Fatalf("installed binary: %q %v", out, err)
	}
	if b, _ := os.ReadFile(OldPath(exe)); string(b) != "old-binary" {
		t.Fatalf("tacet.old = %q", b)
	}
}
