// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var files = map[string]string{
	"store/store.go": "package store\n\ntype Store struct{}\n\nfunc OpenStore() *Store { return &Store{} }\n\nfunc (s *Store) Lookup(k string) string {\n\treturn k\n}\n",
	"api/api.go":     "package api\n\nfunc Handle(s *store.Store) string {\n\treturn s.Lookup(\"a\")\n}\n",
	"cli/cli.go":     "package cli\n\nfunc Run() {\n\tstore.OpenStore().Lookup(\"b\")\n}\n",
}

func project(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// connect runs the server in-process and returns a client session.
func connect(t *testing.T, opts Options) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	srv, closeIndex := NewWithClose(opts)
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close(); _ = ss.Close(); _ = closeIndex() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func TestToolsAreListedSmall(t *testing.T) {
	cs := connect(t, Options{Root: project(t), Version: "test", Map: true})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range res.Tools {
		names[tl.Name] = true
		if tl.OutputSchema != nil {
			t.Errorf("%s has an output schema; it only costs context", tl.Name)
		}
	}
	for _, n := range []string{"symbols", "callers", "outline", "map"} {
		if !names[n] {
			t.Errorf("tool %s missing", n)
		}
	}
	// Every schema is sent with each request: keep the whole list small.
	b, _ := json.Marshal(res.Tools)
	if len(b) > 1600 {
		t.Errorf("tool list is %d bytes: %s", len(b), b)
	}
	if cs.InitializeResult().Instructions == "" {
		t.Error("the server should say when to use its tools")
	}
}

func TestNoToolsWhenMapIsOff(t *testing.T) {
	cs := connect(t, Options{Root: project(t), Version: "test", Map: false})
	res, err := cs.ListTools(context.Background(), nil)
	if err == nil && len(res.Tools) != 0 {
		t.Errorf("tools offered with map off: %+v", res.Tools)
	}
	if cs.InitializeResult().Instructions != "" {
		t.Error("no instructions without tools")
	}
}

func TestSymbolsTool(t *testing.T) {
	cs := connect(t, Options{Root: project(t), Version: "test", Map: true})
	out, isErr := call(t, cs, "symbols", map[string]any{"name": "Lookup"})
	if isErr || !strings.Contains(out, "store/store.go:7-9 func (s *Store) Lookup(k string) string") {
		t.Errorf("symbols Lookup: %q", out)
	}
	out, _ = call(t, cs, "symbols", map[string]any{"name": "Nothing"})
	if !strings.Contains(out, "no symbol matches") {
		t.Errorf("symbols Nothing: %q", out)
	}
	if _, isErr := call(t, cs, "symbols", map[string]any{}); !isErr {
		t.Error("a call without name should be an error")
	}
}

func TestCallersTool(t *testing.T) {
	cs := connect(t, Options{Root: project(t), Version: "test", Map: true})
	out, _ := call(t, cs, "callers", map[string]any{"name": "Lookup"})
	if !strings.Contains(out, `api/api.go:4 (Handle) return s.Lookup("a")`) || !strings.Contains(out, "cli/cli.go:4 (Run)") {
		t.Errorf("callers Lookup: %q", out)
	}
	out, _ = call(t, cs, "callers", map[string]any{"name": "Nothing"})
	if !strings.Contains(out, "no uses") {
		t.Errorf("callers Nothing: %q", out)
	}
}

func TestOutlineTool(t *testing.T) {
	dir := project(t)
	cs := connect(t, Options{Root: dir, Version: "test", Map: true})
	out, _ := call(t, cs, "outline", map[string]any{"path": "store/store.go"})
	want := "3-3 type Store struct{}\n5-5 func OpenStore() *Store { return &Store{} }\n7-9 func (s *Store) Lookup(k string) string\n"
	if out != want {
		t.Errorf("outline:\n%s\nwant:\n%s", out, want)
	}
	if out2, _ := call(t, cs, "outline", map[string]any{"path": filepath.Join(dir, "store", "store.go")}); out2 != want {
		t.Errorf("absolute path outline: %q", out2)
	}
	out, _ = call(t, cs, "outline", map[string]any{"path": "missing.go"})
	if !strings.Contains(out, "not in the code index") {
		t.Errorf("outline missing: %q", out)
	}
}

func TestMapTool(t *testing.T) {
	dir := project(t)
	cs := connect(t, Options{Root: dir, Version: "test", Map: true})
	out, _ := call(t, cs, "map", map[string]any{})
	if !strings.HasPrefix(out, "store/store.go\n") || !strings.Contains(out, "Lookup") {
		t.Errorf("map: %q", out)
	}
	out, _ = call(t, cs, "map", map[string]any{"dir": "api", "budget": 200})
	if !strings.HasPrefix(out, "api/api.go\n") || strings.Contains(out, "store.go") {
		t.Errorf("map of api: %q", out)
	}
	out, _ = call(t, cs, "map", map[string]any{"dir": "nowhere"})
	if !strings.Contains(out, "no indexed code") {
		t.Errorf("map of nowhere: %q", out)
	}
}

func TestToolsSeeNewCode(t *testing.T) {
	dir := project(t)
	x := &index{root: dir}
	if _, err := x.get(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.ix.Close() })
	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("package x\n\nfunc Fresh() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	x.last = x.last.Add(-fresh * 2) // as if the last refresh were old
	ix, err := x.get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if syms, _, _ := ix.Symbols(context.Background(), "Fresh", 5); len(syms) != 1 {
		t.Errorf("new file not indexed: %+v", syms)
	}
}
