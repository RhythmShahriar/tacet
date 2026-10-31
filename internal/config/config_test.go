// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The docs index's feature table lists exactly the features tacet has.
func TestReadmeFeatures(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile(`(?m)^\| \[([a-z]+)\]\(features/[a-z]+\.md\) \| (on|off)\b`)
	var got []string
	for _, m := range row.FindAllStringSubmatch(string(b), -1) {
		got = append(got, m[1])
		if want := map[bool]string{true: "on", false: "off"}[Features[m[1]]]; m[2] != want {
			t.Errorf("docs/README.md says %s is %s by default, config says %s", m[1], m[2], want)
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(FeatureNames(), ",") {
		t.Fatalf("docs/README.md features %v, config %v", got, FeatureNames())
	}
}

func TestLoadOldConfig(t *testing.T) {
	root := t.TempDir()
	old := "features:\n  speak: true\n  trim: true\n  outline: false\n  convert: false\naudit_every: 5\n"
	if err := os.MkdirAll(Dir(root), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path(root), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Load(root)
	for _, gone := range []string{"speak", "trim", "outline"} {
		if _, ok := c.Features[gone]; ok {
			t.Errorf("%s kept after load", gone)
		}
	}
	if c.On("convert") || !c.On("guard") || c.AuditEvery != 5 {
		t.Fatalf("features %v audit_every %d", c.Features, c.AuditEvery)
	}
	if err := c.Set("speak", true); err == nil {
		t.Error("speak is still a feature")
	}
	if len(c.Features) != len(Features) {
		t.Fatalf("features %v", c.Features)
	}
}

func TestLoadMissing(t *testing.T) {
	c := Load(filepath.Join(t.TempDir(), "none"))
	for k, v := range Features {
		if c.On(k) != v {
			t.Errorf("%s = %v, want %v", k, c.On(k), v)
		}
	}
}
