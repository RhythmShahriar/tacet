// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package slug

import "testing"

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Hello, World!":       "hello-world",
		"  Go 1.22 is out  ":  "go-1-22-is-out",
		"Crème brûlée recipe": "crème-brûlée-recipe",
		"":                    "",
	} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
