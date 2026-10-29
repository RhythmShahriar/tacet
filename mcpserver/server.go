// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package mcpserver is tacet's MCP server. It gives the agent symbol
// tools over the code index, so it finds code by name instead of
// grepping and reading whole files. Every tool schema is sent with each
// request, so descriptions and inputs are kept short.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/RhythmShahriar/tacet/codegraph"
)

// Options configures the server.
type Options struct {
	Root    string // the project
	Version string
	Map     bool // offer the code tools; off gives a server with no tools
}

// Result caps keep one answer from flooding the context.
const (
	maxSymbols = 30
	maxCallers = 50
	maxOutline = 300
	maxBudget  = 8192
)

// fresh is how long an update counts as current, so a burst of calls
// lists and stats the files once.
const fresh = 2 * time.Second

// instructions tell the agent when the tools beat grep; it costs a few
// tokens once per session.
const instructions = "Code index tools: use symbols, callers and outline to find code before grep or reading whole files, then Read only the lines they point to."

// New builds the server for opts.
func New(opts Options) *mcp.Server {
	so := &mcp.ServerOptions{}
	if opts.Map {
		so.Instructions = instructions
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "tacet", Version: opts.Version}, so)
	if opts.Map {
		addCodeTools(s, &index{root: opts.Root})
	}
	return s
}

// Run serves on stdin and stdout until the client hangs up.
func Run(ctx context.Context, opts Options) error {
	return New(opts).Run(ctx, &mcp.StdioTransport{})
}

// index opens the code index on first use and refreshes it before each
// call when the last refresh is older than fresh.
type index struct {
	root string
	mu   sync.Mutex
	ix   *codegraph.Index
	last time.Time
}

func (x *index) get(ctx context.Context) (*codegraph.Index, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.ix == nil {
		ix, err := codegraph.Open(x.root)
		if err != nil {
			return nil, err
		}
		x.ix = ix
	}
	if time.Since(x.last) > fresh {
		if _, err := x.ix.Update(ctx); err != nil {
			return nil, err
		}
		x.last = time.Now()
	}
	return x.ix, nil
}

type nameIn struct {
	Name string `json:"name"`
}

type pathIn struct {
	Path string `json:"path"`
}

type mapIn struct {
	Budget int    `json:"budget,omitempty" jsonschema:"tokens, default 1024"`
	Dir    string `json:"dir,omitempty" jsonschema:"only files under this folder"`
}

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func addCodeTools(s *mcp.Server, x *index) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "symbols",
		Description: "Find where a function, method, class or type is defined, by name or part of it (Type.method works). Returns file:line and signature.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in nameIn) (*mcp.CallToolResult, any, error) {
		ix, err := x.get(ctx)
		if err != nil {
			return nil, nil, err
		}
		syms, total, err := ix.Symbols(ctx, in.Name, maxSymbols)
		if err != nil {
			return nil, nil, err
		}
		if len(syms) == 0 {
			return text(fmt.Sprintf("no symbol matches %q", in.Name)), nil, nil
		}
		var b strings.Builder
		for _, sy := range syms {
			fmt.Fprintf(&b, "%s:%d-%d %s\n", sy.Path, sy.Line, sy.End, sy.Signature)
		}
		more(&b, total, len(syms), "use a longer name")
		return text(b.String()), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "callers",
		Description: "Find where a symbol is used: file:line, the enclosing symbol and the line. Matches by name.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in nameIn) (*mcp.CallToolResult, any, error) {
		ix, err := x.get(ctx)
		if err != nil {
			return nil, nil, err
		}
		refs, total, err := ix.Callers(ctx, in.Name, maxCallers)
		if err != nil {
			return nil, nil, err
		}
		if len(refs) == 0 {
			return text(fmt.Sprintf("no uses of %q found", in.Name)), nil, nil
		}
		var b strings.Builder
		for _, r := range refs {
			in := ""
			if r.In != "" {
				in = " (" + r.In + ")"
			}
			fmt.Fprintf(&b, "%s:%d%s %s\n", r.Path, r.Line, in, r.Text)
		}
		more(&b, total, len(refs), "grep within one folder for the rest")
		return text(b.String()), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "outline",
		Description: "List the symbols in one file with their line ranges, so you can Read only the part you need.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pathIn) (*mcp.CallToolResult, any, error) {
		ix, err := x.get(ctx)
		if err != nil {
			return nil, nil, err
		}
		syms, err := ix.Outline(ctx, in.Path)
		if errors.Is(err, codegraph.ErrNotIndexed) {
			return text(fmt.Sprintf("%s is not in the code index (missing, ignored, over 1 MB or not a supported language); Read it instead", in.Path)), nil, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if len(syms) == 0 {
			return text(in.Path + " defines no symbols"), nil, nil
		}
		var b strings.Builder
		for i, sy := range syms {
			if i == maxOutline {
				more(&b, len(syms), i, "Read the file in parts")
				break
			}
			fmt.Fprintf(&b, "%s%d-%d %s\n", strings.Repeat("  ", sy.Depth), sy.Line, sy.End, sy.Signature)
		}
		return text(b.String()), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "map",
		Description: "Ranked map of the code: the most used files and symbols with line numbers, within a token budget.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mapIn) (*mcp.CallToolResult, any, error) {
		ix, err := x.get(ctx)
		if err != nil {
			return nil, nil, err
		}
		files, err := ix.Map(ctx, min(in.Budget, maxBudget), in.Dir)
		if err != nil {
			return nil, nil, err
		}
		if len(files) == 0 {
			return text("no indexed code here"), nil, nil
		}
		return text(codegraph.Render(files)), nil, nil
	})
}

// more notes how many results were left out.
func more(b *strings.Builder, total, shown int, hint string) {
	if total > shown {
		fmt.Fprintf(b, "... %d more; %s\n", total-shown, hint)
	}
}
