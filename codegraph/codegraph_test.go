// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package codegraph

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RhythmShahriar/tacet/tokens"
)

func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		write(t, dir, name, body)
	}
	return dir
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func open(t *testing.T, dir string) *Index {
	t.Helper()
	ix, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	return ix
}

func update(t *testing.T, ix *Index) Stats {
	t.Helper()
	st, err := ix.Update(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

var ctx = context.Background()

// goProject has a core file most others use, a helper file and a test.
var goProject = map[string]string{
	"store/store.go": `package store

// Store keeps records.
type Store struct{ items map[string]string }

// OpenStore makes a Store.
func OpenStore() *Store { return &Store{items: map[string]string{}} }

// Lookup finds one record.
func (s *Store) Lookup(key string) (string, bool) {
	v, ok := s.items[key]
	return v, ok
}

func (s *Store) append(k string) { s.items[k] = k }
`,
	"api/api.go": `package api

import "example/store"

func Handle(s *store.Store) string {
	v, _ := s.Lookup("a")
	return v
}

func Other() {
	s := store.OpenStore()
	s.Lookup("b")
	x := []int{}
	x = append(x, 1)
	_ = x
}
`,
	"cli/cli.go": `package cli

import "example/store"

func Run() {
	st := store.OpenStore()
	st.Lookup("c")
	Lookup := 1
	_ = Lookup
}
`,
	"store/store_test.go": `package store

import "testing"

func TestLookup(t *testing.T) {
	s := OpenStore()
	s.Lookup("x")
}
`,
	"README.md": "# not code\n",
}

func TestEveryLanguageIndexes(t *testing.T) {
	for _, f := range langFixtures {
		t.Run(f.lang, func(t *testing.T) {
			dir := project(t, f.files)
			ix := open(t, dir)
			st := update(t, ix)
			if st.Files != len(f.files) || st.Parsed != len(f.files) {
				t.Fatalf("stats %+v, want %d files", st, len(f.files))
			}
			for _, want := range f.defs {
				name, kind, _ := strings.Cut(want, ":")
				syms, _, err := ix.Symbols(ctx, name, 10)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, s := range syms {
					if s.Name == name && s.Kind == kind && s.Line > 0 && s.End >= s.Line && s.Signature != "" {
						found = true
					}
				}
				if !found {
					t.Errorf("no %s %s in %+v", kind, name, syms)
				}
			}
			refs, _, err := ix.Callers(ctx, f.call, 10)
			if err != nil {
				t.Fatal(err)
			}
			hit := false
			for _, r := range refs {
				if r.Path == f.in && strings.Contains(r.Text, f.call) {
					hit = true
				}
			}
			if !hit {
				t.Errorf("no call of %s in %s: %+v", f.call, f.in, refs)
			}
		})
	}
}

func TestEveryQueryCompiles(t *testing.T) {
	for name, l := range languages {
		if _, err := newParser(l); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for ext, name := range extensions {
		if languages[name] == nil {
			t.Errorf("%s maps to unknown language %s", ext, name)
		}
	}
}

func TestSupported(t *testing.T) {
	for _, p := range []string{"a.go", "src/x.TS", "a.tsx", "a.jsx", "a.py", "a.php", "a.rs", "A.java", "a.rb", "a.c", "a.cpp", "a.cs", "a.kt"} {
		if !Supported(p) {
			t.Errorf("%s should be supported", p)
		}
	}
	for _, p := range []string{"README.md", "a.min.js", "types.d.ts", "Makefile", "a.json", "noext"} {
		if Supported(p) {
			t.Errorf("%s should not be supported", p)
		}
	}
}

func TestUpdateIsIncremental(t *testing.T) {
	dir := project(t, goProject)
	ix := open(t, dir)
	if st := update(t, ix); st.Files != 4 || st.Parsed != 4 {
		t.Fatalf("first update %+v", st)
	}
	if st := update(t, ix); st.Parsed != 0 || st.Removed != 0 || st.Files != 4 {
		t.Fatalf("unchanged update %+v", st)
	}

	write(t, dir, "cli/cli.go", "package cli\n\nfunc Fresh() {}\n")
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(filepath.Join(dir, "cli", "cli.go"), later, later)
	if st := update(t, ix); st.Parsed != 1 || st.Removed != 0 {
		t.Fatalf("one edit %+v", st)
	}
	if syms, _, _ := ix.Symbols(ctx, "Fresh", 5); len(syms) != 1 {
		t.Errorf("Fresh not indexed: %+v", syms)
	}
	if syms, _, _ := ix.Symbols(ctx, "Run", 5); len(syms) != 0 {
		t.Errorf("Run should be gone: %+v", syms)
	}

	if err := os.Remove(filepath.Join(dir, "api", "api.go")); err != nil {
		t.Fatal(err)
	}
	if st := update(t, ix); st.Removed != 1 || st.Files != 3 || st.Parsed != 0 {
		t.Fatalf("one removal %+v", st)
	}
	if syms, _, _ := ix.Symbols(ctx, "Handle", 5); len(syms) != 0 {
		t.Errorf("Handle should be gone: %+v", syms)
	}

	// A second Open reads the same index without parsing again.
	ix2 := open(t, dir)
	if st := update(t, ix2); st.Parsed != 0 {
		t.Errorf("reopened index parsed %d files", st.Parsed)
	}
}

func TestSchemaChangeRebuilds(t *testing.T) {
	dir := project(t, goProject)
	ix := open(t, dir)
	update(t, ix)
	if _, err := ix.db.Exec(`UPDATE meta SET value='old' WHERE key='schema'`); err != nil {
		t.Fatal(err)
	}
	_ = ix.Close()
	ix = open(t, dir)
	if st := update(t, ix); st.Parsed != 4 {
		t.Errorf("rebuild parsed %d, want 4", st.Parsed)
	}
}

func TestSkipsBigBinaryAndDependencyFiles(t *testing.T) {
	dir := project(t, map[string]string{
		"main.go":                 "package main\n\nfunc Kept() {}\n",
		"node_modules/x/index.js": "function Dep() {}\n",
		"vendor/v/v.go":           "package v\n\nfunc Vendored() {}\n",
		".hidden/h.go":            "package h\n\nfunc Hidden() {}\n",
		"bin.go":                  "package main\x00\nfunc Binary() {}\n",
		"dist/bundle.js":          "function Bundled() {}\n",
		"web/app.min.js":          "function Minified(){}",
	})
	write(t, dir, "big.go", "package main\n\nfunc Big() {}\n"+strings.Repeat("// pad\n", MaxFileSize/7+1))
	ix := open(t, dir)
	update(t, ix)
	if syms, _, _ := ix.Symbols(ctx, "Kept", 5); len(syms) != 1 {
		t.Errorf("Kept missing: %+v", syms)
	}
	for _, name := range []string{"Dep", "Vendored", "Hidden", "Binary", "Bundled", "Minified", "Big"} {
		if syms, _, _ := ix.Symbols(ctx, name, 5); len(syms) != 0 {
			t.Errorf("%s should be skipped: %+v", name, syms)
		}
	}
}

func TestGitIgnoreIsRespected(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := project(t, map[string]string{
		"main.go":      "package main\n\nfunc Tracked() {}\n",
		"untracked.go": "package main\n\nfunc Untracked() {}\n",
		"gen/gen.go":   "package gen\n\nfunc Generated() {}\n",
		".gitignore":   "gen/\n",
	})
	for _, args := range [][]string{{"init", "-q"}, {"add", "main.go", ".gitignore"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	ix := open(t, dir)
	update(t, ix)
	for name, want := range map[string]int{"Tracked": 1, "Untracked": 1, "Generated": 0} {
		syms, _, _ := ix.Symbols(ctx, name, 5)
		got := 0
		for _, s := range syms {
			if s.Name == name {
				got++
			}
		}
		if got != want {
			t.Errorf("%s: %d symbols, want %d", name, got, want)
		}
	}
}

func TestSymbols(t *testing.T) {
	ix := open(t, project(t, goProject))
	update(t, ix)

	syms, total, err := ix.Symbols(ctx, "Lookup", 10)
	// TestLookup matches too, after the exact name.
	if err != nil || total != 2 || syms[0].Path != "store/store.go" || syms[0].Line != 10 || syms[0].End != 13 {
		t.Fatalf("Lookup: %+v %d %v", syms, total, err)
	}
	if !strings.HasPrefix(syms[0].Signature, "func (s *Store) Lookup(key string)") {
		t.Errorf("signature %q", syms[0].Signature)
	}

	// Exact matches come first, then prefixes, then substrings.
	syms, _, _ = ix.Symbols(ctx, "store", 10)
	if len(syms) < 2 || syms[0].Name != "Store" || syms[1].Name != "OpenStore" {
		t.Errorf("ranking for store: %+v", syms)
	}

	// Type.method narrows by the receiver in the signature.
	if syms, _, _ = ix.Symbols(ctx, "Store.Lookup", 10); len(syms) != 1 {
		t.Errorf("Store.Lookup: %+v", syms)
	}
	if syms, _, _ = ix.Symbols(ctx, "Nope.Lookup", 10); len(syms) != 0 {
		t.Errorf("Nope.Lookup should miss: %+v", syms)
	}

	// The limit cuts the list but the total counts every match.
	syms, total, _ = ix.Symbols(ctx, "o", 1)
	if len(syms) != 1 || total < 3 {
		t.Errorf("limit: %d shown, %d total", len(syms), total)
	}

	for _, q := range []string{"Missing", "", "%", "_"} {
		if syms, _, _ := ix.Symbols(ctx, q, 10); len(syms) != 0 {
			t.Errorf("%q should match nothing: %+v", q, syms)
		}
	}
}

func TestCallers(t *testing.T) {
	ix := open(t, project(t, goProject))
	update(t, ix)

	refs, total, err := ix.Callers(ctx, "Lookup", 50)
	if err != nil {
		t.Fatal(err)
	}
	// Three calls in code, one in a test; the local variable named Lookup
	// in cli.go is a plain mention and is left out.
	if total != 4 {
		t.Fatalf("total %d: %+v", total, refs)
	}
	if refs[0].Path != "api/api.go" || refs[0].Line != 6 || refs[0].In != "Handle" || !strings.Contains(refs[0].Text, `s.Lookup("a")`) {
		t.Errorf("first ref %+v", refs[0])
	}
	if last := refs[len(refs)-1]; last.Path != "store/store_test.go" || last.In != "TestLookup" {
		t.Errorf("tests should come last: %+v", last)
	}
	for _, r := range refs {
		if r.Path == "cli/cli.go" && r.Line == 8 {
			t.Errorf("local variable counted as a call: %+v", r)
		}
	}

	refs, total, _ = ix.Callers(ctx, "Lookup", 2)
	if len(refs) != 2 || total != 4 {
		t.Errorf("limit: %d shown, %d total", len(refs), total)
	}

	// A name that is only ever mentioned, never called, still shows.
	dir := project(t, map[string]string{
		"a.go": "package a\n\nconst Limit = 3\n",
		"b.go": "package a\n\nvar x = Limit + 1\n",
	})
	ix2 := open(t, dir)
	update(t, ix2)
	if refs, _, _ := ix2.Callers(ctx, "Limit", 10); len(refs) != 1 || refs[0].Path != "b.go" {
		t.Errorf("mentions of Limit: %+v", refs)
	}

	if refs, total, _ := ix.Callers(ctx, "Nothing", 10); len(refs) != 0 || total != 0 {
		t.Errorf("unknown name: %+v", refs)
	}
}

func TestOutline(t *testing.T) {
	dir := project(t, langFixtures[1].files) // typescript
	ix := open(t, dir)
	update(t, ix)

	syms, err := ix.Outline(ctx, "shape.ts")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range syms {
		got = append(got, strings.Repeat(" ", s.Depth)+s.Name)
	}
	want := "Sized Unit Shape  constructor  area newShape double"
	if strings.Join(got, " ") != want {
		t.Errorf("outline %q, want %q", strings.Join(got, " "), want)
	}
	// Absolute and ./ paths name the same file.
	for _, p := range []string{filepath.Join(dir, "shape.ts"), "./shape.ts"} {
		if s, err := ix.Outline(ctx, p); err != nil || len(s) != len(syms) {
			t.Errorf("%s: %d symbols, %v", p, len(s), err)
		}
	}
	for _, p := range []string{"missing.ts", "README.md", "../shape.ts"} {
		if _, err := ix.Outline(ctx, p); !errors.Is(err, ErrNotIndexed) {
			t.Errorf("%s: %v, want ErrNotIndexed", p, err)
		}
	}
}

func TestMapRanksTheCoreFirst(t *testing.T) {
	ix := open(t, project(t, goProject))
	update(t, ix)
	files, err := ix.Map(ctx, 1000, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 || files[0].Path != "store/store.go" {
		t.Fatalf("first file %+v", files)
	}
	first := files[0].Symbols
	names := map[string]bool{}
	for _, s := range first {
		names[s.Name] = true
	}
	if !names["Lookup"] || !names["OpenStore"] {
		t.Errorf("store symbols %+v", first)
	}
	for i := 1; i < len(first); i++ {
		if first[i].Line < first[i-1].Line {
			t.Errorf("symbols not in line order: %+v", first)
		}
	}
	// The builtin append doesn't make the project's append a hot symbol.
	files, _ = ix.Map(ctx, 25, "")
	for _, f := range files {
		for _, s := range f.Symbols {
			if s.Name == "append" {
				t.Errorf("append ranked into a tiny map: %+v", files)
			}
		}
	}
}

func TestMapBudgetAndDir(t *testing.T) {
	ix := open(t, project(t, goProject))
	update(t, ix)
	for _, budget := range []int{20, 60, 200} {
		files, err := ix.Map(ctx, budget, "")
		if err != nil {
			t.Fatal(err)
		}
		if n := tokens.Estimate(Render(files)); n > budget {
			t.Errorf("budget %d: map costs %d tokens", budget, n)
		}
	}
	small, _ := ix.Map(ctx, 20, "")
	big, _ := ix.Map(ctx, 2000, "")
	if len(Render(small)) >= len(Render(big)) {
		t.Errorf("a bigger budget should show more")
	}

	files, _ := ix.Map(ctx, 2000, "api")
	if len(files) != 1 || files[0].Path != "api/api.go" {
		t.Errorf("dir api: %+v", files)
	}
	if files, _ := ix.Map(ctx, 2000, "nowhere"); len(files) != 0 {
		t.Errorf("dir nowhere: %+v", files)
	}
	// Zero means the default budget.
	if files, _ := ix.Map(ctx, 0, ""); len(files) == 0 {
		t.Error("default budget gave an empty map")
	}
}

func TestMapKeepsLanguagesApart(t *testing.T) {
	ix := open(t, project(t, map[string]string{
		"lib.php":  "<?php\nfunction render_page() { return 1; }\n",
		"main.go":  "package main\n\nfunc main() { render_page() }\n\nfunc render_page() {}\n",
		"other.go": "package main\n\nfunc other() { render_page() }\n",
	}))
	update(t, ix)
	files, _ := ix.Map(ctx, 1000, "")
	rank := map[string]float64{}
	for _, f := range files {
		rank[f.Path] = f.Rank
	}
	if rank["main.go"] <= rank["lib.php"] {
		t.Errorf("Go calls ranked the PHP file: %+v", files)
	}
}

func TestMapOfEmptyProject(t *testing.T) {
	ix := open(t, project(t, map[string]string{"README.md": "hi\n"}))
	if st := update(t, ix); st.Files != 0 {
		t.Errorf("files %d", st.Files)
	}
	files, err := ix.Map(ctx, 100, "")
	if err != nil || len(files) != 0 || Render(files) != "" {
		t.Errorf("empty map: %+v %v", files, err)
	}
}

func TestUpdateHonorsCancel(t *testing.T) {
	ix := open(t, project(t, goProject))
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ix.Update(c); err == nil {
		t.Error("a canceled update should fail")
	}
}

func TestShortNameAndSignature(t *testing.T) {
	for in, want := range map[string]string{"Foo::bar": "bar", "a.b": "b", "~Shape": "Shape", "plain": "plain", "x y": ""} {
		if got := shortName(in); got != want {
			t.Errorf("shortName(%q) = %q, want %q", in, got, want)
		}
	}
	lines := [][]byte{[]byte("  func   F(a int)  {"), []byte(strings.Repeat("x", 300))}
	if got := signature(lines, 1); got != "func F(a int)" {
		t.Errorf("signature %q", got)
	}
	if got := signature(lines, 2); len(got) != maxSig+3 {
		t.Errorf("long signature not capped: %d", len(got))
	}
	if signature(lines, 0) != "" || signature(lines, 3) != "" {
		t.Error("out of range lines should give no signature")
	}
}
