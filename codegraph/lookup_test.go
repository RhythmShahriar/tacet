// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package codegraph

import (
	"errors"
	"os"
	"testing"
)

func TestOpenReadOnly(t *testing.T) {
	dir := project(t, goProject)
	if _, err := OpenReadOnly(dir); !errors.Is(err, ErrNoIndex) {
		t.Fatalf("missing index: %v", err)
	}
	if _, err := os.Stat(DBPath(dir)); !os.IsNotExist(err) {
		t.Fatal("OpenReadOnly created the index")
	}
	ix := open(t, dir)
	update(t, ix)

	// Readable while the writer still holds it open.
	ro, err := OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Close() }()
	defs, total, err := ro.Define(ctx, "Lookup", 5)
	if err != nil || total != 1 || len(defs) != 1 || defs[0].Path != "store/store.go" {
		t.Fatalf("Define: %v %d %v", defs, total, err)
	}
	if !defs[0].Fresh(dir) {
		t.Error("an unchanged file reads as stale")
	}
	if d, _, _ := ro.Define(ctx, "lookup", 5); len(d) != 0 {
		t.Errorf("Define is case-sensitive, got %v", d)
	}
	n, err := ro.CallerCount(ctx, "Lookup")
	_, want, _ := ix.Callers(ctx, "Lookup", 0)
	if err != nil || n != want || n == 0 {
		t.Errorf("CallerCount = %d, Callers total = %d, err %v", n, want, err)
	}
	if _, err := ro.db.Exec(`DELETE FROM defs`); err == nil {
		t.Error("a read-only index accepted a write")
	}

	// Another schema reads as no index.
	if _, err := ix.db.Exec(`UPDATE meta SET value='old' WHERE key='schema'`); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenReadOnly(dir); !errors.Is(err, ErrNoIndex) {
		t.Errorf("old schema: %v", err)
	}
}
