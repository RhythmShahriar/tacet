// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package convert turns documents into Markdown the agent can read for a
// fraction of the tokens: a PDF page read natively costs its text plus an
// image of the page. Results are cached by content hash.
package convert

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RhythmShahriar/tacet/tokens"
	markitdown "github.com/conductor-oss/markitdown"
)

// Exts are the document types tacet converts. Claude Code reads
// notebooks itself.
var Exts = map[string]bool{
	".pdf": true, ".docx": true, ".pptx": true, ".xlsx": true, ".xls": true, ".epub": true,
}

// Binary reports whether Claude Code refuses to read path at all: its
// Read tool rejects these before any hook runs, so tacet has to point
// the agent at the Markdown before it tries.
func Binary(path string) bool {
	e := strings.ToLower(filepath.Ext(path))
	return Exts[e] && e != ".pdf"
}

// MinText is how little text a conversion may yield before tacet treats
// the file as a scan and leaves it to Claude Code's own reader, which
// sees the page images.
const MinText = 200

// MaxSize skips files too big to convert inside a hook.
const MaxSize = 100 << 20

// ErrScanned means the document has too little text to be worth it.
var ErrScanned = errors.New("almost no text: a scan or images; read it natively")

// Supported reports whether path is a document tacet converts.
func Supported(path string) bool { return Exts[strings.ToLower(filepath.Ext(path))] }

// Dir is where converted files are cached.
func Dir(root string) string { return filepath.Join(root, ".tacet", "docs") }

// Result is one conversion.
type Result struct {
	Source    string
	Path      string // the Markdown file
	Cached    bool
	Chars     int
	FirstPart int    // lines in part 1 when the document is split; 0 when it fits one read
	Outline   string // outline file for a long document with headings; "" otherwise
}

// OutlineMin is the size from which a document gets an outline, so the
// agent can read only the sections a task needs.
const OutlineMin = 6_000

// PartTokens is the most one part holds, under Claude Code's 25k-token
// cap on a single Read so no read fails and has to be retried.
const PartTokens = 20_000

// Compose writes the header and, for a long document, the line ranges
// of its parts so the agent reads each in one call.
func Compose(rel, text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	note := "<!-- tacet converted " + rel + " to Markdown to save tokens. Images and layout aren't included; ask to read the original if they matter. -->"
	// Cut after a blank line once a part is full.
	var cuts []int // index of the first line of each part after the first
	n := 0
	for i, l := range lines {
		n += tokens.Estimate(l) + 1
		if n >= PartTokens*9/10 && strings.TrimSpace(l) == "" && i+1 < len(lines) {
			cuts = append(cuts, i+1)
			n = 0
		} else if n >= PartTokens && i+1 < len(lines) {
			cuts = append(cuts, i+1)
			n = 0
		}
	}
	if len(cuts) == 0 {
		return note + "\n\n" + text + "\n"
	}
	// The header is two lines plus a blank, so body line i is file line i+4.
	const off = 4
	var parts []string
	start := 0
	for k, c := range append(cuts, len(lines)) {
		parts = append(parts, fmt.Sprintf("part %d: offset %d limit %d", k+1, start+off, c-start))
		start = c
	}
	index := "<!-- Long document: read it in parts with offset and limit, " + strings.Join(parts, "; ") + ". -->"
	return note + "\n" + index + "\n\n" + text + "\n"
}

// FirstPartLimit returns the limit of part 1 from a composed file's
// header, or 0 when the file is one part.
func FirstPartLimit(doc []byte) int {
	head := string(doc)
	if len(head) > 4096 {
		head = head[:4096]
	}
	var off, limit int
	if i := strings.Index(head, "part 1: offset "); i >= 0 {
		if _, err := fmt.Sscanf(head[i:], "part 1: offset %d limit %d", &off, &limit); err == nil {
			return off - 1 + limit
		}
	}
	return 0
}

var (
	mdOnce sync.Once
	md     *markitdown.MarkItDown
)

// File converts path into the cache under root and returns the Markdown
// file, reusing an earlier conversion of the same bytes.
func File(root, path string) (*Result, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() > MaxSize {
		return nil, fmt.Errorf("%s is over %d MB", filepath.Base(path), MaxSize>>20)
	}
	sum, err := hashFile(path)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	out := filepath.Join(Dir(root), safeName(base)+"-"+sum[:12]+".md")
	if b, err := os.ReadFile(out); err == nil { // #nosec G304 -- tacet's own cache
		if len(b) == 0 {
			return nil, ErrScanned
		}
		return &Result{Source: path, Path: out, Cached: true, Chars: len(b), FirstPart: FirstPartLimit(b), Outline: existing(outlinePath(out))}, nil
	}
	mdOnce.Do(func() { md = markitdown.New() })
	r, err := md.ConvertFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(Tighten(r.Markdown))
	if err := os.MkdirAll(Dir(root), 0o750); err != nil {
		return nil, err
	}
	if len(text) < MinText {
		_ = os.WriteFile(out, nil, 0o600) // remember, so the next read is instant
		return nil, ErrScanned
	}
	rel := path
	if r, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(r, "..") {
		rel = filepath.ToSlash(r)
	}
	doc := Compose(rel, text)
	tmp := out + ".tacet-tmp"
	if err := os.WriteFile(tmp, []byte(doc), 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, out); err != nil {
		return nil, err
	}
	res := &Result{Source: path, Path: out, Chars: len(text), FirstPart: FirstPartLimit([]byte(doc))}
	if o := Outline(rel, out, doc); o != "" {
		if err := os.WriteFile(outlinePath(out), []byte(o), 0o600); err == nil {
			res.Outline = outlinePath(out)
		}
	}
	return res, nil
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p) // #nosec G304 -- a document the agent asked to read
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}

func outlinePath(md string) string { return strings.TrimSuffix(md, ".md") + ".outline.md" }

func existing(p string) string {
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

type heading struct {
	level int
	title string
	line  int // 1-based file line
}

// Outline lists a long document's headings with the line range and size
// of each section, or returns "" when the document is short or has too
// few headings to navigate by.
func Outline(rel, mdPath, doc string) string {
	lines := strings.Split(doc, "\n")
	total := tokens.Estimate(doc)
	if total < OutlineMin {
		return ""
	}
	var hs []heading
	inFence := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(l, "#") {
			continue
		}
		lvl := len(l) - len(strings.TrimLeft(l, "#"))
		title := strings.TrimSpace(l[lvl:])
		if lvl > 4 || title == "" || len(title) > 120 {
			continue
		}
		hs = append(hs, heading{lvl, title, i + 1})
	}
	if len(hs) < 3 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- tacet outline of %s, about %s tokens in full. -->\n", rel, approx(total))
	fmt.Fprintf(&b, "Full text: %s\n", mdPath)
	b.WriteString("For a specific question: Read only the matching sections of the full text with the offset and limit below (several Reads in one turn if needed), or Grep it.\n")
	b.WriteString("For the whole document: ")
	if ps := partRanges(doc); len(ps) > 1 {
		b.WriteString("Read these parts of the full text all in one turn, as parallel Reads: " + strings.Join(ps, "; "))
	} else {
		b.WriteString("Read the full text once, with no offset or limit")
	}
	b.WriteString(".\n\noffset limit tokens section\n")
	for i, h := range hs {
		end := len(lines)
		for _, n := range hs[i+1:] {
			if n.level <= h.level {
				end = n.line - 1
				break
			}
		}
		sec := strings.Join(lines[h.line-1:end], "\n")
		fmt.Fprintf(&b, "%d %d %s %s%s\n", h.line, end-h.line+1, approx(tokens.Estimate(sec)), strings.Repeat("  ", h.level-1), h.title)
	}
	return b.String()
}

func approx(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprint(n)
}

// partRanges reads the part list a composed file's header carries.
func partRanges(doc string) []string {
	head := doc
	if len(head) > 4096 {
		head = head[:4096]
	}
	i := strings.Index(head, "read it in parts with offset and limit, ")
	if i < 0 {
		return nil
	}
	rest := head[i+len("read it in parts with offset and limit, "):]
	if j := strings.Index(rest, ". -->"); j >= 0 {
		rest = rest[:j]
	}
	var out []string
	for _, p := range strings.Split(rest, "; ") {
		if k := strings.Index(p, ": "); k >= 0 {
			out = append(out, p[k+2:])
		}
	}
	return out
}

// Mentions finds document paths named in a prompt that exist under root.
func Mentions(root, cwd, prompt string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range strings.FieldsFunc(prompt, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == '"' || r == '\'' || r == '`' || r == '(' || r == ')' || r == ',' || r == ';'
	}) {
		f = strings.TrimRight(f, ".:?!")
		if !Supported(f) {
			continue
		}
		for _, base := range []string{cwd, root} {
			p := f
			if !filepath.IsAbs(p) {
				p = filepath.Join(base, f)
			}
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && !seen[p] {
				seen[p] = true
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// ReadTarget is the file the agent should read for a document and a
// request: the focused file, the outline, or the whole Markdown.
func ReadTarget(r *Result, ask string, outline bool) string {
	if r.Outline != "" && outline {
		if f := Focus(r.Outline, r.Path, ask); f != "" {
			return f
		}
		return r.Outline
	}
	return r.Path
}
