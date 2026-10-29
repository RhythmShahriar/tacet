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

// Exts are the document types tacet converts.
var Exts = map[string]bool{
	".pdf": true, ".docx": true, ".pptx": true, ".xlsx": true, ".xls": true,
	".epub": true, ".ipynb": true,
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
	FirstPart int // lines in part 1 when the document is split; 0 when it fits one read
}

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
		return &Result{Source: path, Path: out, Cached: true, Chars: len(b), FirstPart: FirstPartLimit(b)}, nil
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
	return &Result{Source: path, Path: out, Chars: len(text), FirstPart: FirstPartLimit([]byte(doc))}, nil
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
