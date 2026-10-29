// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package feedback

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MaxRetries caps how often the test gate stops the agent from finishing
// in one session.
const MaxRetries = 2

// TestTimeout caps one test run. A suite that takes longer is skipped
// rather than holding the session.
var TestTimeout = 240 * time.Second

// TestCommand is the project's test command at root, or nil when tacet
// can't find one it can run without installing anything.
func TestCommand(root string) *Check {
	has := func(name string) bool { _, err := os.Stat(filepath.Join(root, name)); return err == nil }
	if has("go.mod") {
		if g := Tool(root, root, "go"); g != "" {
			return &Check{Name: "go test ./...", Dir: root, Args: []string{g, "test", "./..."},
				Filter: dropMatching(`^# `, `^ok\s`, `^\?\s`, `^=== (RUN|PAUSE|CONT)`, `^\s*--- PASS`, `^PASS$`, `no test files`)}
		}
	}
	if has("Cargo.toml") {
		if c := Tool(root, root, "cargo"); c != "" {
			return &Check{Name: "cargo test -q", Dir: root, Args: []string{c, "test", "-q"},
				Filter: dropMatching(`^running \d+ tests?$`, `^test result: ok`, `^[.i]+( \d+/\d+)?$`, `^\s*(Compiling|Finished|Running|Doc-tests)\s`)}
		}
	}
	if has("pyproject.toml") || has("pytest.ini") || has("setup.cfg") || has("tox.ini") || has("conftest.py") {
		if p := Tool(root, root, "pytest"); p != "" {
			return &Check{Name: "pytest -q --tb=short", Dir: root, Args: []string{p, "-q", "--tb=short"},
				Filter: dropMatching(`^[.sxX]+\s*\[\s*\d+%\]$`)}
		}
	}
	if has("package.json") && has("node_modules") {
		if npm := Tool(root, root, "npm"); npm != "" && npmHasTest(filepath.Join(root, "package.json")) {
			return &Check{Name: "npm test", Dir: root, Args: []string{npm, "test"}, Filter: dropMatching(`^> `)}
		}
	}
	return nil
}

func npmHasTest(path string) bool {
	b, err := os.ReadFile(path) // #nosec G304 -- the project's package.json
	if err != nil {
		return false
	}
	var p struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(b, &p) != nil {
		return false
	}
	t := p.Scripts["test"]
	return t != "" && !strings.Contains(t, "no test specified")
}

// gateState is the test gate's memory for one session, in .tacet.
type gateState struct {
	Session      string `json:"session"`
	Edits        int    `json:"edits"`          // source edits this session
	EditsAtBlock int    `json:"edits_at_block"` // Edits when the gate last blocked
	Retries      int    `json:"retries"`        // times the gate blocked a stop
	Dirty        bool   `json:"dirty"`          // edits since the last green run
}

func gatePath(root string) string { return filepath.Join(root, ".tacet", "testgate.json") }

func loadGate(root, session string) gateState {
	var st gateState
	if b, err := os.ReadFile(gatePath(root)); err == nil { // #nosec G304 -- tacet's own state
		_ = json.Unmarshal(b, &st)
	}
	if st.Session != session {
		st = gateState{Session: session}
	}
	return st
}

func saveGate(root string, st gateState) {
	b, _ := json.Marshal(st)
	p := gatePath(root)
	if os.MkdirAll(filepath.Dir(p), 0o750) != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

// MarkEdit records that the agent changed a source file, so the next
// stop runs the tests.
func MarkEdit(root, session string) {
	st := loadGate(root, session)
	st.Edits++
	st.Dirty = true
	saveGate(root, st)
}

// Gate runs the tests when source changed since the last green run and
// returns the failures as the reason to keep going, or "" to let the
// agent stop. It blocks at most MaxRetries times a session and never
// again when the agent made no edit since the last block, which is what
// stop_hook_active (active) warns about.
func Gate(ctx context.Context, root, session string, active bool) (reason string, timedOut bool) {
	st := loadGate(root, session)
	if !st.Dirty {
		return "", false
	}
	if st.Retries >= MaxRetries || (active && st.Retries > 0 && st.Edits == st.EditsAtBlock) {
		st.Dirty = false
		saveGate(root, st)
		return "", false
	}
	c := TestCommand(root)
	if c == nil {
		return "", false
	}
	lines, timedOut := run(ctx, *c, TestTimeout)
	if timedOut {
		return "", true
	}
	if len(lines) == 0 {
		st.Dirty = false
		saveGate(root, st)
		return "", false
	}
	st.Retries++
	st.EditsAtBlock = st.Edits
	saveGate(root, st)
	head := fmt.Sprintf("tacet testgate: `%s` fails after your changes (check %d of %d). Fix the failures below, then finish:", c.Name, st.Retries, MaxRetries)
	return Cap(head + "\n" + strings.Join(lines, "\n")), false
}
