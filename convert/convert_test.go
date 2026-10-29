// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package convert

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pdf builds a one-page PDF that shows text, with a correct xref table.
func pdf(text string) []byte {
	stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

func docx(paragraphs ...string) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	add := func(name, body string) {
		w, _ := z.Create(name)
		w.Write([]byte(body))
	}
	add("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`)
	add("_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`)
	var body strings.Builder
	for _, p := range paragraphs {
		body.WriteString(`<w:p><w:r><w:t>` + p + `</w:t></w:r></w:p>`)
	}
	add("word/document.xml", `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`+body.String()+`</w:body></w:document>`)
	z.Close()
	return b.Bytes()
}

func TestConvertAndCache(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("The retry policy backs off exponentially. ", 8)
	cases := map[string][]byte{"spec.pdf": pdf(long), "notes.docx": docx(long, "Second paragraph about timeouts.")}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(root, name)
			os.WriteFile(p, body, 0o644)
			r, err := File(root, p)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(r.Path)
			if !strings.Contains(string(b), "retry policy backs off") || !strings.HasPrefix(string(b), "<!-- tacet converted "+name) {
				t.Fatalf("bad markdown:\n%s", b)
			}
			again, err := File(root, p)
			if err != nil || !again.Cached || again.Path != r.Path {
				t.Fatalf("second read should hit the cache: %+v %v", again, err)
			}
		})
	}
}

func TestScanIsLeftAlone(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "scan.pdf")
	os.WriteFile(p, pdf("x"), 0o644)
	if _, err := File(root, p); !errors.Is(err, ErrScanned) {
		t.Fatalf("want ErrScanned, got %v", err)
	}
	if _, err := File(root, p); !errors.Is(err, ErrScanned) {
		t.Fatalf("cached scan: want ErrScanned, got %v", err)
	}
}

func TestSupported(t *testing.T) {
	if !Supported("a/B.PDF") || Supported("main.go") || Supported("notes.md") {
		t.Fatal("wrong types")
	}
}

func TestComposeParts(t *testing.T) {
	short := Compose("a.pdf", "hello\n")
	if FirstPartLimit([]byte(short)) != 0 || strings.Contains(short, "Long document") {
		t.Fatal("a short document is one part")
	}
	para := strings.Repeat("word ", 400) // about 570 tokens
	var body []string
	for i := 0; i < 80; i++ {
		body = append(body, fmt.Sprintf("para %d %s", i, para), "")
	}
	doc := Compose("big.pdf", strings.Join(body, "\n"))
	limit := FirstPartLimit([]byte(doc))
	if limit == 0 {
		t.Fatal("no parts in a long document")
	}
	lines := strings.Split(doc, "\n")
	// Part 1 (file lines 1..limit) stays under the cap and part 2 starts
	// where the header says.
	if got := tokensOf(lines[:limit]); got > PartTokens {
		t.Fatalf("part 1 is %d tokens", got)
	}
	var off2 int
	i := strings.Index(doc, "part 2: offset ")
	fmt.Sscanf(doc[i:], "part 2: offset %d", &off2)
	if off2 != limit+1 || !strings.HasPrefix(lines[off2-1], "para") && lines[off2-1] != "" {
		t.Fatalf("part 2 offset %d, limit %d, line %q", off2, limit, lines[off2-1])
	}
}

func tokensOf(ls []string) int {
	n := 0
	for _, l := range ls {
		n += len([]rune(l))*2/7 + 1
	}
	return n
}
