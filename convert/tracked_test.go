// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package convert

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	git(t, root, "init", "-q")
	return root
}

func TestTrackedListsOnlyTrackedOfficeFiles(t *testing.T) {
	root := repo(t)
	for _, f := range []string{"docs/spec.docx", "deck.PPTX", "data.xlsx", "book.epub", "paper.pdf", "notes.md", "untracked.docx"} {
		os.MkdirAll(filepath.Dir(filepath.Join(root, f)), 0o755)
		os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644)
	}
	git(t, root, "add", "docs/spec.docx", "deck.PPTX", "data.xlsx", "book.epub", "paper.pdf", "notes.md")
	// A tracked file over MaxSize, made sparse and added without hashing.
	big := filepath.Join(root, "huge.docx")
	f, _ := os.Create(big)
	f.Truncate(MaxSize + 1)
	f.Close()
	git(t, root, "update-index", "--add", "--cacheinfo", "100644,e69de29bb2d1d6434b8b29ae775ad8c2e48c5391,huge.docx")

	var got []string
	for _, p := range Tracked(root) {
		rel, _ := filepath.Rel(root, p)
		got = append(got, filepath.ToSlash(rel))
	}
	want := "book.epub data.xlsx deck.PPTX docs/spec.docx"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

func TestTrackedCapsTheList(t *testing.T) {
	root := repo(t)
	for i := range MaxTracked + 3 {
		os.WriteFile(filepath.Join(root, fmt.Sprintf("d%02d.docx", i)), []byte("x"), 0o644)
	}
	git(t, root, "add", ".")
	if n := len(Tracked(root)); n != MaxTracked {
		t.Fatalf("listed %d, want %d", n, MaxTracked)
	}
}

func TestTrackedOutsideGit(t *testing.T) {
	if got := Tracked(t.TempDir()); got != nil {
		t.Fatalf("outside git: %v", got)
	}
}

func TestSumUsesTheIndexUntilTheFileChanges(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.docx")
	os.WriteFile(p, []byte("one"), 0o644)
	s1, err := Sum(root, p)
	if err != nil || len(s1) != 64 {
		t.Fatalf("sum %q %v", s1, err)
	}
	b, _ := os.ReadFile(indexPath(root))
	if !strings.Contains(string(b), `"a.docx"`) || !strings.Contains(string(b), s1) {
		t.Fatalf("index not written: %s", b)
	}
	os.WriteFile(p, []byte("two!"), 0o644)
	os.Chtimes(p, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	s2, _ := Sum(root, p)
	if s2 == s1 {
		t.Fatal("a changed file kept its old hash")
	}
}

func TestPendingAndLine(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("The retry policy backs off exponentially. ", 8)
	done := filepath.Join(root, "docs", "done.docx")
	todo := filepath.Join(root, "todo.docx")
	os.MkdirAll(filepath.Dir(done), 0o755)
	os.WriteFile(done, docx(long), 0o644)
	os.WriteFile(todo, docx(long, "other"), 0o644)
	if _, err := File(root, done); err != nil {
		t.Fatal(err)
	}
	d, td, size := Pending(root, []string{done, todo})
	if len(d) != 1 || len(td) != 1 || td[0] != todo || size == 0 {
		t.Fatalf("done %v todo %v size %d", d, td, size)
	}
	if r := Lookup(root, todo); r != nil {
		t.Fatal("Lookup converted a document")
	}
	line := Line(root, d[0])
	if !strings.HasPrefix(line, "docs/done.docx → read .tacet/docs/done-") || !strings.HasSuffix(line, ".md") {
		t.Fatalf("line %q", line)
	}
	r := *d[0]
	r.Outline = strings.TrimSuffix(r.Path, ".md") + ".outline.md"
	if l := Line(root, &r); !strings.HasSuffix(l, ".outline.md (or the .md)") {
		t.Fatalf("outline line %q", l)
	}
}

func TestBusy(t *testing.T) {
	root := t.TempDir()
	if Busy(root) {
		t.Fatal("busy with no marker")
	}
	os.MkdirAll(Dir(root), 0o755)
	os.WriteFile(BusyFile(root), nil, 0o600)
	if !Busy(root) {
		t.Fatal("fresh marker not busy")
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(BusyFile(root), old, old)
	if Busy(root) {
		t.Fatal("stale marker still busy")
	}
}
