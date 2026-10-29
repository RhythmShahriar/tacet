// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/RhythmShahriar/tacet/audio"
)

// Release asset names, a contract with .goreleaser.yaml.
const (
	ChecksumsName = "checksums.txt"
	BundleName    = "checksums.txt.sigstore.json"
)

// ArchiveName is the release archive for a system: tacet_linux_amd64.tar.gz,
// tacet_windows_arm64.zip.
func ArchiveName(goos, goarch string) string {
	if goos == "windows" {
		return "tacet_" + goos + "_" + goarch + ".zip"
	}
	return "tacet_" + goos + "_" + goarch + ".tar.gz"
}

// maxBinary caps the tacet binary read out of an archive.
const maxBinary = 512 << 20

// Installer downloads, verifies and installs one release.
type Installer struct {
	Client   *http.Client
	Verifier Verifier
	Exe      string // the binary to replace
	GOOS     string
	GOARCH   string
	// Smoke runs the new binary before it goes in place; an error stops
	// the update. Nil skips the check.
	Smoke func(path, version string) error
	Log   io.Writer
}

func (in *Installer) logf(format string, args ...any) {
	if in.Log != nil {
		fmt.Fprintf(in.Log, format, args...)
	}
}

// Install fails closed: the binary is only replaced after the signature,
// the signer, the checksum and the smoke test all pass.
func (in *Installer) Install(ctx context.Context, r *Release) error {
	name := ArchiveName(in.GOOS, in.GOARCH)
	for _, n := range []string{name, ChecksumsName, BundleName} {
		if r.Assets[n] == "" {
			return fmt.Errorf("release %s has no %s", r.Tag, n)
		}
	}
	tmp, err := os.MkdirTemp("", "tacet-update-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	in.logf("downloading %s\n", ChecksumsName)
	sums, err := fetch(ctx, in.Client, r.Assets[ChecksumsName], 1<<20)
	if err != nil {
		return err
	}
	sig, err := fetch(ctx, in.Client, r.Assets[BundleName], 1<<20)
	if err != nil {
		return err
	}
	in.logf("verifying the signature of %s\n", r.Tag)
	if err := in.Verifier.Verify(ctx, sums, sig, r.Tag); err != nil {
		return err
	}
	want, err := Checksum(sums, name)
	if err != nil {
		return err
	}
	in.logf("downloading %s\n", name)
	archive := filepath.Join(tmp, name)
	if err := audio.Download(ctx, in.Client, audio.Asset{Name: name, URL: r.Assets[name], SHA256: want}, archive); err != nil {
		return err
	}
	bin := filepath.Join(tmp, "tacet-"+r.Version)
	if err := Extract(archive, binaryName(in.GOOS), bin); err != nil {
		return fmt.Errorf("unpack %s: %w", name, err)
	}
	if in.Smoke != nil {
		if err := in.Smoke(bin, r.Version); err != nil {
			return fmt.Errorf("the new binary did not run: %w", err)
		}
	}
	return Replace(in.Exe, bin, in.GOOS)
}

func binaryName(goos string) string {
	if goos == "windows" {
		return "tacet.exe"
	}
	return "tacet"
}

// fetch downloads a small file into memory.
func fetch(ctx context.Context, client *http.Client, url string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req) // #nosec G107 -- a release asset, verified before use
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s: larger than %d bytes", url, max)
	}
	return b, nil
}

// ErrNoChecksum means checksums.txt has no line for the archive.
var ErrNoChecksum = errors.New("no checksum for the archive")

// Checksum finds name's SHA-256 in a checksums.txt.
func Checksum(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("%w %s", ErrNoChecksum, name)
}

// ErrNoBinary means the archive holds no tacet binary at its top level.
var ErrNoBinary = errors.New("no tacet binary in the archive")

// Extract copies the file called want at the archive's top level to dst.
func Extract(archive, want, dst string) error {
	if strings.HasSuffix(archive, ".zip") {
		z, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer func() { _ = z.Close() }()
		for _, f := range z.File {
			if f.Name != want || f.FileInfo().IsDir() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer func() { _ = rc.Close() }()
			return writeBinary(dst, rc)
		}
		return ErrNoBinary
	}
	f, err := os.Open(archive) // #nosec G304 -- a checked download
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return ErrNoBinary
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeReg && strings.TrimPrefix(h.Name, "./") == want {
			return writeBinary(dst, tr)
		}
	}
}

func writeBinary(dst string, r io.Reader) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700) // #nosec G302 G304 -- an executable in our temp folder
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, maxBinary+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxBinary {
		err = errors.New("binary is too large")
	}
	return err
}
