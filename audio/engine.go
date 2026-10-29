// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package audio

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Asset is one file tacet downloads, pinned to the hash it must have.
type Asset struct {
	Name   string
	URL    string
	SHA256 string
	Size   int64
}

// EngineVersion is the whisper.cpp release tacet downloads.
const EngineVersion = "b5454"

const releases = "https://github.com/ggml-org/whisper.cpp/releases/download/" + EngineVersion + "/"

// engines are the official whisper.cpp builds with a command line tool,
// by GOOS/GOARCH. macOS has none: Homebrew builds it.
var engines = map[string]Asset{
	"linux/amd64":   {"whisper-bin-ubuntu-x64.tar.gz", releases + "whisper-bin-ubuntu-x64.tar.gz", "a72becf15d7917f990f6313867a52638b82b7f9ef237fb0c980dac56a135781c", 10364195},
	"linux/arm64":   {"whisper-bin-ubuntu-arm64.tar.gz", releases + "whisper-bin-ubuntu-arm64.tar.gz", "6b95ebfc60447df48e70ef00a73bdc3f41679ed2d01ef465827206a2ff325149", 4608377},
	"windows/amd64": {"whisper-bin-x64.zip", releases + "whisper-bin-x64.zip", "6ba69e3482d7826214f90a6a9c84ca07782aec1e1d0c6a7c30c994fd5d816ccb", 8928640},
	"windows/arm64": {"whisper-bin-win-cpu-arm64.zip", releases + "whisper-bin-win-cpu-arm64.zip", "28c37e7b598c3d9bbfef94f3bd67f2ee6f12b7c86f3da5edcf5e4308d615ff6d", 4371193},
}

// models is pinned to one revision of the model repository, so the URL
// always serves the bytes the hash describes.
const models = "https://huggingface.co/ggerganov/whisper.cpp/resolve/5359861c739e955e79d9a303bcbc70fb988958b1/"

// Model choices: multilingual, or English only (a little more accurate
// on English speech).
var (
	ModelMulti   = Asset{"ggml-base-q5_1.bin", models + "ggml-base-q5_1.bin", "422f1ae452ade6f30a004d7e5c6a43195e4433bc370bf23fac9cc591f01a8898", 59707625}
	ModelEnglish = Asset{"ggml-base.en-q5_1.bin", models + "ggml-base.en-q5_1.bin", "4baf70dd0d7c4247ba2b81fafd9c01005ac77c2f9ef064e00dcf195d0e2fdd2f", 59721011}
)

// Engine is the official build for this system, false when there is none.
func Engine() (Asset, bool) {
	a, ok := engines[runtime.GOOS+"/"+runtime.GOARCH]
	return a, ok
}

// CacheDir is where the engine and models live: the user cache folder,
// shared by every project. TACET_CACHE_DIR overrides it.
func CacheDir() string {
	if d := os.Getenv("TACET_CACHE_DIR"); d != "" {
		return d
	}
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "tacet")
}

func engineDir() string { return filepath.Join(CacheDir(), "whisper-"+EngineVersion) }

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// FindWhisper finds whisper-cli: TACET_WHISPER_CLI, then PATH, then the
// copy tacet downloaded.
func FindWhisper() string {
	if p := os.Getenv("TACET_WHISPER_CLI"); p != "" {
		return p
	}
	for _, n := range []string{"whisper-cli", "whisper-cpp"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	p := filepath.Join(engineDir(), exe("whisper-cli"))
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		return p
	}
	return ""
}

// FindModel returns the downloaded model to use and whether it is the
// English-only one; the multilingual model wins when both are there.
func FindModel() (path string, english bool) {
	for _, m := range []Asset{ModelMulti, ModelEnglish} {
		p := filepath.Join(CacheDir(), m.Name)
		if fi, err := os.Stat(p); err == nil && fi.Size() == m.Size {
			return p, m == ModelEnglish
		}
	}
	return "", false
}

// ErrNotSetUp means the engine or the model isn't there yet.
var ErrNotSetUp = errors.New("speech engine not set up: run tacet transcribe --setup once")

// Ready reports the engine and model to use, or ErrNotSetUp.
func Ready() (bin, model string, english bool, err error) {
	bin = FindWhisper()
	model, english = FindModel()
	if bin == "" || model == "" {
		return "", "", false, ErrNotSetUp
	}
	return bin, model, english, nil
}

// InstallHint is what to do when no official build exists for this system.
func InstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "brew install whisper-cpp"
	case "linux":
		return "build whisper.cpp (git clone https://github.com/ggml-org/whisper.cpp && cd whisper.cpp && cmake -B build && cmake --build build -j --config Release) and put build/bin/whisper-cli on your PATH"
	}
	return "install whisper.cpp from https://github.com/ggml-org/whisper.cpp and put whisper-cli on your PATH"
}

// Plan lists what Setup would download, empty when nothing is missing.
// It fails when no engine is installed and none is published for this
// system.
func Plan(english bool) ([]Asset, error) {
	var need []Asset
	if FindWhisper() == "" {
		e, ok := Engine()
		if !ok {
			return nil, fmt.Errorf("no whisper.cpp build is published for %s/%s: %s", runtime.GOOS, runtime.GOARCH, InstallHint())
		}
		need = append(need, e)
	}
	m := ModelMulti
	if english {
		m = ModelEnglish
	}
	if fi, err := os.Stat(filepath.Join(CacheDir(), m.Name)); err != nil || fi.Size() != m.Size {
		need = append(need, m)
	}
	return need, nil
}

// Setup downloads and checks each asset of a plan, then unpacks the engine.
func Setup(ctx context.Context, client *http.Client, need []Asset, progress io.Writer) error {
	if err := os.MkdirAll(CacheDir(), 0o750); err != nil {
		return err
	}
	for _, a := range need {
		fmt.Fprintf(progress, "downloading %s (%d MB)\n", a.Name, (a.Size+(1<<20)-1)>>20)
		dst := filepath.Join(CacheDir(), a.Name)
		if err := Download(ctx, client, a, dst); err != nil {
			return err
		}
		if strings.HasSuffix(a.Name, ".bin") {
			continue
		}
		err := Unpack(dst, engineDir())
		_ = os.Remove(dst)
		if err != nil {
			return fmt.Errorf("unpack %s: %w", a.Name, err)
		}
	}
	return nil
}

// ErrHash means a download didn't match its pinned hash; it is deleted.
var ErrHash = errors.New("download does not match its pinned SHA-256")

// Download fetches a to dst and keeps it only when its SHA-256 matches.
func Download(ctx context.Context, client *http.Client, a Asset, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req) // #nosec G107 -- pinned URL, checked by hash
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", a.URL, resp.Status)
	}
	tmp := dst + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) // #nosec G304 -- tacet's cache
	if err != nil {
		return err
	}
	h := sha256.New()
	limit := a.Size + 1
	if a.Size <= 0 {
		limit = 1 << 30
	}
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, limit))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, a.SHA256) {
		_ = os.Remove(tmp)
		return fmt.Errorf("%s: %w (got %s)", a.Name, ErrHash, got)
	}
	return os.Rename(tmp, dst)
}

// keep reports whether an archive entry is needed to run whisper-cli:
// the tool, its shared libraries and the license.
func keep(name string) bool {
	switch {
	case name == "whisper-cli", name == "whisper-cli.exe", name == "LICENSE":
		return true
	case strings.HasPrefix(name, "lib") && strings.Contains(name, ".so"),
		strings.HasPrefix(name, "lib") && strings.HasSuffix(name, ".dylib"),
		strings.HasSuffix(strings.ToLower(name), ".dll"):
		return true
	}
	return false
}

// Unpack extracts the files whisper-cli needs from a release archive
// into dir, flat. Entries that would leave dir are refused.
func Unpack(archive, dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if strings.HasSuffix(archive, ".zip") {
		return unzip(archive, dir)
	}
	return untar(archive, dir)
}

func untar(archive, dir string) error {
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
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Base(h.Name)
		if !keep(name) {
			continue
		}
		dst := filepath.Join(dir, name)
		switch h.Typeflag {
		case tar.TypeReg:
			if err := writeFile(dst, tr, h.Size); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Libraries link to their versioned file in the same folder.
			if strings.ContainsAny(h.Linkname, `/\`) || h.Linkname == ".." {
				return fmt.Errorf("refusing link %s -> %s", h.Name, h.Linkname)
			}
			_ = os.Remove(dst)
			if err := os.Symlink(h.Linkname, dst); err != nil {
				return err
			}
		}
	}
}

func unzip(archive, dir string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer func() { _ = z.Close() }()
	for _, zf := range z.File {
		name := filepath.Base(zf.Name)
		if zf.FileInfo().IsDir() || !keep(name) {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = writeFile(filepath.Join(dir, name), rc, int64(zf.UncompressedSize64)) // #nosec G115 -- size of one file
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeFile(dst string, r io.Reader, size int64) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o750) // #nosec G302 G304 -- an executable in tacet's cache
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(r, size)); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
