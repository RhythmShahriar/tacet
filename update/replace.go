// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// OldPath is where the replaced binary is kept: tacet.old next to tacet
// (tacet.old next to tacet.exe on Windows).
func OldPath(exe string) string {
	return strings.TrimSuffix(exe, filepath.Ext(exe)) + ".old"
}

// Replace puts the binary at newBin in place of exe and keeps the binary
// it replaces as OldPath(exe). The new file is staged in exe's folder
// first, so the final step is one rename. goos picks the strategy:
//
//   - Unix renames over the running file, which is atomic; the old one
//     is kept through a hard link (a copy when links aren't supported).
//   - Windows can't rename over a running .exe but can rename it away,
//     so the running binary moves to .old and the new one moves in. If
//     that second rename fails, the first is undone.
func Replace(exe, newBin, goos string) error {
	fi, err := os.Stat(exe)
	if err != nil {
		return err
	}
	staged, err := stage(exe, newBin, fi.Mode().Perm()|0o100)
	if err != nil {
		return err
	}
	old := OldPath(exe)
	if goos == "windows" {
		err = swapWindows(exe, staged, old)
	} else {
		err = swapUnix(exe, staged, old)
	}
	if err != nil {
		_ = os.Remove(staged)
	}
	return err
}

func swapUnix(exe, staged, old string) error {
	_ = os.Remove(old)
	if err := os.Link(exe, old); err != nil {
		if err := copyFile(exe, old, 0o755); err != nil {
			return fmt.Errorf("keeping %s: %w", filepath.Base(old), err)
		}
	}
	return os.Rename(staged, exe)
}

func swapWindows(exe, staged, old string) error {
	if err := os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing the previous %s: %w", filepath.Base(old), err)
	}
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(staged, exe); err != nil {
		if rerr := os.Rename(old, exe); rerr != nil {
			return fmt.Errorf("%w; restoring the old binary also failed: %w (it is at %s)", err, rerr, old)
		}
		return err
	}
	return nil
}

// ErrNoOld means there is no tacet.old to roll back to.
var ErrNoOld = errors.New("no previous binary to roll back to")

// Rollback puts OldPath(exe) back in place of exe.
func Rollback(exe, goos string) error {
	old := OldPath(exe)
	if _, err := os.Stat(old); err != nil {
		return ErrNoOld
	}
	cur := exe + ".rollback"
	if goos == "windows" {
		_ = os.Remove(cur)
		if err := os.Rename(exe, cur); err != nil {
			return err
		}
		if err := os.Rename(old, exe); err != nil {
			_ = os.Rename(cur, exe)
			return err
		}
		return nil
	}
	return os.Rename(old, exe)
}

// stage copies src into a new hidden file in dst's folder with mode.
func stage(dst, src string, mode os.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(dst), ".tacet-new-*"+filepath.Ext(dst))
	if err != nil {
		return "", fmt.Errorf("can't write to %s: %w", filepath.Dir(dst), err)
	}
	name := f.Name()
	err = copyInto(f, src)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, mode)
	}
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func copyInto(f *os.File, src string) error {
	in, err := os.Open(src) // #nosec G304 -- a verified download
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if _, err := io.Copy(f, in); err != nil {
		return err
	}
	return f.Sync()
}

func copyFile(src, dst string, mode os.FileMode) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode) // #nosec G304 -- next to tacet's own binary
	if err != nil {
		return err
	}
	err = copyInto(f, src)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
