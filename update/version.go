// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package update

import (
	"strconv"
	"strings"
)

// semver is a parsed major.minor.patch with an optional pre-release.
type semver struct {
	n   [3]int
	pre string
}

func parse(v string) (semver, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	var s semver
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, s.pre = v[:i], v[i+1:]
		if s.pre == "" {
			return s, false
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return s, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return s, false
		}
		s.n[i] = n
	}
	return s, true
}

// Valid reports whether v is a release version tacet can compare.
func Valid(v string) bool {
	_, ok := parse(v)
	return ok
}

// Newer reports whether latest is a later release than current. A
// version that doesn't parse, such as a dev build, is never newer and
// never older.
func Newer(latest, current string) bool {
	l, ok1 := parse(latest)
	c, ok2 := parse(current)
	if !ok1 || !ok2 {
		return false
	}
	return compare(l, c) > 0
}

func compare(a, b semver) int {
	for i := range a.n {
		if a.n[i] != b.n[i] {
			if a.n[i] > b.n[i] {
				return 1
			}
			return -1
		}
	}
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "":
		return 1 // a release is newer than its own pre-release
	case b.pre == "":
		return -1
	}
	return comparePre(a.pre, b.pre)
}

// comparePre orders pre-release identifiers as semver 2.0 does.
func comparePre(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, errx := strconv.Atoi(as[i])
		y, erry := strconv.Atoi(bs[i])
		switch {
		case errx == nil && erry == nil:
			if x != y {
				if x > y {
					return 1
				}
				return -1
			}
		case errx == nil:
			return -1 // numbers sort before words
		case erry == nil:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(as) > len(bs):
		return 1
	case len(as) < len(bs):
		return -1
	}
	return 0
}

// Clean drops a leading v: v1.1.0 gives 1.1.0.
func Clean(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }
