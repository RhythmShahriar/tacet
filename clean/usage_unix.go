// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

//go:build !windows

package clean

import (
	"errors"
	"io/fs"
	"syscall"
)

// usage is the disk space a file takes and whether other hard links
// share it, as pnpm's package store does: removing one link of such a
// file frees nothing.
func usage(fi fs.FileInfo) (bytes int64, shared bool) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512, !fi.IsDir() && st.Nlink > 1
	}
	return fi.Size(), false
}

// alive reports whether a process with this pid runs. A pid that is
// reused by another process reads as alive, which keeps a worktree.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
