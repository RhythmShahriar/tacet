// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package clean

import (
	"io/fs"
	"path/filepath"
	"strconv"
	"time"
)

// Size is what a folder holds on disk.
type Size struct {
	Bytes  int64 // files with one link: freed when the folder goes
	Shared int64 // files with other hard links, which may stay
	Capped bool  // the walk ran out of time; the sizes are lower bounds
	Newest time.Time
}

// tickEvery is how often Measure reports a running total.
const tickEvery = time.Second

// Measure walks dir without following links, for at most limit. tick,
// when set, hears the running total about once a second, so a big tree
// shows progress.
func Measure(dir string, limit time.Duration, tick func(bytes int64)) Size {
	var s Size
	start := time.Now()
	last := start
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable: skip it, keep the rest
		}
		if n++; n%512 == 0 {
			now := time.Now()
			if now.Sub(start) > limit {
				s.Capped = true
				return filepath.SkipAll
			}
			if tick != nil && now.Sub(last) >= tickEvery {
				last = now
				tick(s.Bytes + s.Shared)
			}
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		b, shared := usage(fi)
		if shared {
			s.Shared += b
		} else {
			s.Bytes += b
		}
		if fi.ModTime().After(s.Newest) {
			s.Newest = fi.ModTime()
		}
		return nil
	})
	return s
}

// HumanBytes formats a size the way every screen shows it: 820 KB,
// 14.2 MB, 32.1 GB (powers of 1000).
func HumanBytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return strconv.FormatInt(n, 10) + " B"
	}
	return strconv.FormatFloat(f, 'f', 1, 64) + " " + units[i]
}
