// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package skills

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSource(t *testing.T) {
	cases := map[string]Source{
		"anthropics/skills/skills/pdf":                          {Owner: "anthropics", Repo: "skills", Path: "skills/pdf"},
		"anthropics/skills/skills/pdf@v1.2":                     {Owner: "anthropics", Repo: "skills", Path: "skills/pdf", Ref: "v1.2"},
		"github.com/o/r/a/b/":                                   {Owner: "o", Repo: "r", Path: "a/b"},
		"o/r":                                                   {Owner: "o", Repo: "r"},
		"o/r/x/SKILL.md@main":                                   {Owner: "o", Repo: "r", Path: "x", Ref: "main", File: true},
		"https://github.com/o/r/tree/main/skills/pdf":           {Owner: "o", Repo: "r", Path: "skills/pdf", Ref: "main"},
		"https://github.com/o/r/blob/dev/s/SKILL.md":            {Owner: "o", Repo: "r", Path: "s", Ref: "dev"},
		"https://raw.githubusercontent.com/o/r/main/s/SKILL.md": {Owner: "o", Repo: "r", Path: "s", Ref: "main", File: true},
		"https://example.com/team/lint/SKILL.md":                {URL: "https://example.com/team/lint/SKILL.md", File: true},
	}
	for in, want := range cases {
		got, err := ParseSource(in)
		if err != nil || got != want {
			t.Errorf("%s: %+v %v, want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "pdf", "o/r/../etc", "http://example.com/SKILL.md", "https://example.com/readme.md", "o r/x", "https://raw.githubusercontent.com/o/r/main/x.md"} {
		if _, err := ParseSource(in); !errors.Is(err, ErrSource) {
			t.Errorf("%q: %v, want ErrSource", in, err)
		}
	}
}

func TestFetchPlainURLPinsByHash(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("---\ndescription: Use when linting the team's code.\n---\nRun the linter.\n"))
	}))
	defer srv.Close()
	src, err := ParseSource(srv.URL + "/team/team-lint/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	im, err := Fetch(context.Background(), srv.Client(), src)
	if err != nil {
		t.Fatal(err)
	}
	if im.Name != "team-lint" || !strings.HasPrefix(im.Commit, "sha256:") || len(im.Files) != 1 {
		t.Fatalf("import %+v", im)
	}
	root := t.TempDir()
	dir, err := im.Write(root, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if !strings.HasPrefix(string(b), "---\n# imported by tacet skills add from 127.0.0.1:") || strings.Contains(string(b), "https://") {
		t.Fatalf("SKILL.md:\n%s", b)
	}
}

func TestImportFileKinds(t *testing.T) {
	for _, c := range []struct{ path, mode, data, want string }{
		{"reference.md", "100644", "# notes", ""},
		{"scripts/run.py", "100644", "print(1)", "script"},
		{"tool", "100644", "#!/bin/sh\necho", "script"},
		{"helper", "100755", "x", "executable"},
		{"logo.png", "100644", "\x89PNG\x00\x00", "binary"},
	} {
		if got := importFile(c.path, c.mode, []byte(c.data)).Script; got != c.want {
			t.Errorf("%s: %q, want %q", c.path, got, c.want)
		}
	}
}

func TestFinishNeedsFrontmatter(t *testing.T) {
	im := &Import{Origin: "x", Files: []ImportFile{importFile("SKILL.md", "100644", []byte("# just a body\n"))}}
	if err := im.finish("x"); !errors.Is(err, ErrNoFrontmatter) {
		t.Fatalf("got %v", err)
	}
}
