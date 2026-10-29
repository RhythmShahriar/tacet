// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package codegraph indexes a project's code by symbol: where each
// function, method and type is defined and where its name is used. The
// index lives in .tacet/index.db and only re-reads files that changed,
// so the agent can find code without grepping or reading whole files.
package codegraph

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure Go SQLite driver
)

// MaxFileSize is the largest file indexed; bigger ones are skipped.
const MaxFileSize = 1 << 20

// maxWorkers caps the parsers at work: each holds a syntax tree and
// past eight the single writer is the bottleneck anyway.
const maxWorkers = 8

// schema changes when the tables or what the parser extracts change; a
// different value rebuilds the index.
const schema = "4"

// skipDirs are never walked when the project isn't a git checkout.
var skipDirs = map[string]bool{
	".git": true, ".tacet": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, "target": true, ".next": true,
	"__pycache__": true, ".venv": true, "venv": true,
}

// Index is an open code index for one project.
type Index struct {
	root string
	db   *sql.DB
}

// Stats says what one Update did.
type Stats struct {
	Files   int           `json:"files"`   // indexed files after the update
	Parsed  int           `json:"parsed"`  // files read again
	Removed int           `json:"removed"` // files dropped from the index
	Took    time.Duration `json:"took_ns"`
}

// DBPath is where the index of root is kept.
func DBPath(root string) string { return filepath.Join(root, ".tacet", "index.db") }

// Open opens the index of root, creating it when missing. Call Update
// to bring it up to date with the files.
func Open(root string) (*Index, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(DBPath(abs)), 0o750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(DBPath(abs))+
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ix := &Index{root: abs, db: db}
	if err := ix.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return ix, nil
}

// Close closes the database.
func (ix *Index) Close() error { return ix.db.Close() }

// Root is the project the index covers.
func (ix *Index) Root() string { return ix.root }

func (ix *Index) migrate() error {
	if _, err := ix.db.Exec(`CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		return err
	}
	var v string
	_ = ix.db.QueryRow(`SELECT value FROM meta WHERE key='schema'`).Scan(&v)
	if v == schema {
		return nil
	}
	_, err := ix.db.Exec(`
DROP TABLE IF EXISTS files; DROP TABLE IF EXISTS defs; DROP TABLE IF EXISTS refs;
CREATE TABLE files(id INTEGER PRIMARY KEY, path TEXT UNIQUE NOT NULL, lang TEXT, mtime INTEGER, size INTEGER);
CREATE TABLE defs(file INTEGER NOT NULL, name TEXT NOT NULL, kind TEXT, line INTEGER, end_line INTEGER, sig TEXT);
CREATE INDEX defs_name ON defs(name);
CREATE INDEX defs_file ON defs(file);
CREATE TABLE refs(file INTEGER NOT NULL, name TEXT NOT NULL, lines TEXT, strong INTEGER, total INTEGER);
CREATE INDEX refs_name ON refs(name);
CREATE INDEX refs_file ON refs(file);
INSERT OR REPLACE INTO meta(key, value) VALUES('schema', '` + schema + `');`)
	return err
}

// stamp is what tells a changed file from an unchanged one.
type stamp struct {
	id          int64
	mtime, size int64
}

// parsed is one file's extraction.
type parsed struct {
	path        string
	mtime, size int64
	defs        []def
	refs        []refGroup
}

// Update re-reads the files that changed since the last update and
// drops the ones that are gone. When nothing changed it only lists and
// stats the files.
func (ix *Index) Update(ctx context.Context) (Stats, error) {
	start := time.Now()
	paths, err := listFiles(ix.root)
	if err != nil {
		return Stats{}, err
	}
	known := map[string]stamp{}
	rows, err := ix.db.QueryContext(ctx, `SELECT id, path, mtime, size FROM files`)
	if err != nil {
		return Stats{}, err
	}
	for rows.Next() {
		var p string
		var s stamp
		if err := rows.Scan(&s.id, &p, &s.mtime, &s.size); err != nil {
			_ = rows.Close()
			return Stats{}, err
		}
		known[p] = s
	}
	_ = rows.Close()

	var todo []parsed
	live := map[string]bool{}
	for _, p := range paths {
		fi, err := os.Stat(filepath.Join(ix.root, filepath.FromSlash(p)))
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > MaxFileSize {
			continue
		}
		live[p] = true
		s, ok := known[p]
		if ok && s.mtime == fi.ModTime().UnixNano() && s.size == fi.Size() {
			continue
		}
		todo = append(todo, parsed{path: p, mtime: fi.ModTime().UnixNano(), size: fi.Size()})
	}
	var gone []int64
	for p, s := range known {
		if !live[p] {
			gone = append(gone, s.id)
		}
	}
	st := Stats{Files: len(live), Parsed: len(todo), Removed: len(gone)}
	if len(todo) == 0 && len(gone) == 0 {
		st.Took = time.Since(start)
		return st, nil
	}
	// Parsed files stream into one write transaction, so a big first
	// index never holds every file's symbols in memory at once.
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan parsed, 64)
	go func() {
		ix.parseAll(pctx, todo, results)
		close(results)
	}()
	err = ix.write(ctx, results, gone, known)
	cancel()
	for range results { // let the workers finish when the write stopped early
	}
	if err != nil {
		return st, err
	}
	st.Took = time.Since(start)
	return st, nil
}

// parseAll reads and parses each file, one parser per language per
// worker. Results go to out.
func (ix *Index) parseAll(ctx context.Context, todo []parsed, out chan<- parsed) {
	jobs := make(chan parsed)
	var wg sync.WaitGroup
	workers := min(runtime.GOMAXPROCS(0), maxWorkers, len(todo))
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			parsers := map[string]*parser{}
			for f := range jobs {
				l := langOf(f.path)
				ps := parsers[l.name]
				if ps == nil {
					var err error
					if ps, err = newParser(l); err != nil {
						continue
					}
					parsers[l.name] = ps
				}
				src, err := os.ReadFile(filepath.Join(ix.root, filepath.FromSlash(f.path))) // #nosec G304 -- a file of the project being indexed
				if err == nil && bytes.IndexByte(src, 0) < 0 {
					var refs []ref
					f.defs, refs = ps.parse(src)
					f.refs = group(refs)
				} // else unreadable or binary: indexed as empty
				select {
				case out <- f:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
feed:
	for _, f := range todo {
		select {
		case jobs <- f:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
}

// write stores the parsed files and drops the gone ones in one
// transaction, so readers never see a half-written index.
func (ix *Index) write(ctx context.Context, todo <-chan parsed, gone []int64, known map[string]stamp) (err error) {
	conn, err := ix.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
			return
		}
		_, err = conn.ExecContext(ctx, "COMMIT")
	}()
	prep := func(q string) (*sql.Stmt, error) { return conn.PrepareContext(ctx, q) }
	stmts := map[string]*sql.Stmt{}
	for k, q := range map[string]string{
		"delDefs":  `DELETE FROM defs WHERE file=?`,
		"delRefs":  `DELETE FROM refs WHERE file=?`,
		"delFile":  `DELETE FROM files WHERE id=?`,
		"addFile":  `INSERT INTO files(path, lang, mtime, size) VALUES(?, ?, ?, ?)`,
		"setFile":  `UPDATE files SET mtime=?, size=? WHERE id=?`,
		"addDef":   `INSERT INTO defs(file, name, kind, line, end_line, sig) VALUES(?, ?, ?, ?, ?, ?)`,
		"addRef":   `INSERT INTO refs(file, name, lines, strong, total) VALUES(?, ?, ?, ?, ?)`,
		"findFile": `SELECT id FROM files WHERE path=?`,
	} {
		s, err := prep(q)
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
		stmts[k] = s
	}
	wipe := func(id int64) error {
		if _, err := stmts["delDefs"].ExecContext(ctx, id); err != nil {
			return err
		}
		_, err := stmts["delRefs"].ExecContext(ctx, id)
		return err
	}
	for _, id := range gone {
		if err := wipe(id); err != nil {
			return err
		}
		if _, err := stmts["delFile"].ExecContext(ctx, id); err != nil {
			return err
		}
	}
	for f := range todo {
		var id int64
		if s, ok := known[f.path]; ok {
			id = s.id
			if err := wipe(id); err != nil {
				return err
			}
			if _, err := stmts["setFile"].ExecContext(ctx, f.mtime, f.size, id); err != nil {
				return err
			}
		} else {
			// Another process may have added it since we listed.
			if stmts["findFile"].QueryRowContext(ctx, f.path).Scan(&id) == nil {
				if err := wipe(id); err != nil {
					return err
				}
				if _, err := stmts["setFile"].ExecContext(ctx, f.mtime, f.size, id); err != nil {
					return err
				}
			} else {
				r, err := stmts["addFile"].ExecContext(ctx, f.path, langOf(f.path).name, f.mtime, f.size)
				if err != nil {
					return err
				}
				if id, err = r.LastInsertId(); err != nil {
					return err
				}
			}
		}
		for _, d := range f.defs {
			if _, err := stmts["addDef"].ExecContext(ctx, id, d.name, d.kind, d.line, d.end, d.sig); err != nil {
				return err
			}
		}
		for _, r := range f.refs {
			if _, err := stmts["addRef"].ExecContext(ctx, id, r.name, r.lines, r.strong, r.total); err != nil {
				return err
			}
		}
	}
	return ctx.Err() // canceled: the parse stopped early, so roll back
}

// listFiles returns the project's indexable files, slash-separated and
// relative to root. In a git checkout it asks git, so .gitignore holds;
// elsewhere it walks the tree, skipping dependency and build folders.
func listFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard") // #nosec G204 -- fixed git subcommand
	if out, err := cmd.Output(); err == nil {
		var paths []string
		seen := map[string]bool{}
		for _, p := range strings.Split(string(out), "\x00") {
			if p != "" && !seen[p] && Supported(p) && !strings.HasPrefix(p, ".tacet/") {
				seen[p] = true
				paths = append(paths, p)
			}
		}
		return paths, nil
	}
	var paths []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil
		}
		if d.IsDir() {
			if p != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if Supported(p) {
			rel, err := filepath.Rel(root, p)
			if err == nil {
				paths = append(paths, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return paths, err
}
