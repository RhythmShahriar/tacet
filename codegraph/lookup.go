// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package codegraph

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
)

// ErrNoIndex means the project has no current code index to read.
var ErrNoIndex = errors.New("no code index")

// OpenReadOnly opens an existing index for reading only: it never
// creates, migrates or writes, so a hook can use it in a few
// milliseconds next to a running update. It fails with ErrNoIndex when
// the index is missing or was built by another schema.
func OpenReadOnly(root string) (*Index, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	p := DBPath(abs)
	if fi, err := os.Stat(p); err != nil || fi.Size() == 0 {
		return nil, ErrNoIndex
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(p)+"?mode=ro&_pragma=busy_timeout(200)&_pragma=query_only(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var v string
	if db.QueryRow(`SELECT value FROM meta WHERE key='schema'`).Scan(&v) != nil || v != schema {
		_ = db.Close()
		return nil, ErrNoIndex
	}
	return &Index{root: abs, db: db}, nil
}

// Definition is where a name is defined, with the stamp the index read
// the file at, so a caller can tell whether the line is still right.
type Definition struct {
	Symbol
	MTime int64 // the file's modification time when indexed, in ns
	Size  int64
}

// Fresh reports whether the defining file is unchanged since it was
// indexed, so its line number can be trusted.
func (d Definition) Fresh(root string) bool {
	fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(d.Path)))
	return err == nil && fi.ModTime().UnixNano() == d.MTime && fi.Size() == d.Size
}

// Define returns the definitions of exactly name, case-sensitive, real
// code before tests, at most limit of them and how many there are.
func (ix *Index) Define(ctx context.Context, name string, limit int) ([]Definition, int, error) {
	rows, err := ix.db.QueryContext(ctx, `SELECT f.path, f.mtime, f.size, d.name, d.kind, d.line, d.end_line, d.sig
FROM defs d JOIN files f ON f.id = d.file WHERE d.name = ? ORDER BY f.path, d.line`, name)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var real, tests []Definition
	for rows.Next() {
		var d Definition
		if err := rows.Scan(&d.Path, &d.MTime, &d.Size, &d.Name, &d.Kind, &d.Line, &d.End, &d.Signature); err != nil {
			return nil, 0, err
		}
		if isTest(d.Path) {
			tests = append(tests, d)
		} else {
			real = append(real, d)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	out := append(real, tests...)
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

// CallerCount is how many lines Callers would list for name, counted
// from the index alone without reading any file.
func (ix *Index) CallerCount(ctx context.Context, name string) (int, error) {
	rows, err := ix.db.QueryContext(ctx, `SELECT lines FROM refs WHERE name = ?`, name)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	strong, all := 0, 0
	for rows.Next() {
		var lines string
		if err := rows.Scan(&lines); err != nil {
			return 0, err
		}
		for _, l := range unpackLines(lines) {
			all++
			if l.strong {
				strong++
			}
		}
	}
	if strong > 0 {
		return strong, rows.Err()
	}
	return all, rows.Err()
}
