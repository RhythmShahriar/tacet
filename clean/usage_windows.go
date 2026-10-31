// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

//go:build windows

package clean

import "io/fs"

// usage is the file's size; Windows reports no link count here.
func usage(fi fs.FileInfo) (bytes int64, shared bool) { return fi.Size(), false }

// alive is always true here: without a check, a lock stays a lock.
func alive(int) bool { return true }
