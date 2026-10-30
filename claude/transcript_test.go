// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `{"type":"user","timestamp":"2026-10-29T01:00:00Z","sessionId":"s1","origin":{"kind":"human"},"message":{"role":"user","content":[{"type":"text","text":"run the tests please"}]}}
{"type":"assistant","timestamp":"2026-10-29T01:00:05Z","message":{"id":"m1","role":"assistant","model":"claude-opus-5-5","content":[{"type":"thinking","thinking":""}],"usage":{"input_tokens":2,"cache_creation_input_tokens":1000,"cache_read_input_tokens":20000,"output_tokens":50}}}
{"type":"assistant","timestamp":"2026-10-29T01:00:05Z","message":{"id":"m1","role":"assistant","model":"claude-opus-5-5","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/p/spec.pdf"}}],"usage":{"input_tokens":2,"cache_creation_input_tokens":1000,"cache_read_input_tokens":20000,"output_tokens":50}}}
{"type":"user","timestamp":"2026-10-29T01:00:06Z","message":{"role":"user","content":[{"type":"tool_result","content":"..."}]}}
{"type":"user","timestamp":"2026-10-29T01:00:07Z","origin":{"kind":"task-notification"},"message":{"role":"user","content":"<task-notification>done</task-notification>"}}
not json at all
{"type":"assistant","timestamp":"2026-10-29T01:00:09Z","message":{"id":"m2","role":"assistant","model":"claude-opus-5-5","content":[{"type":"tool_use","name":"Skill","input":{"skill":"deploy"}},{"type":"tool_use","name":"mcp__github__get_issue","input":{}}],"usage":{"input_tokens":1,"cache_creation_input_tokens":10,"cache_read_input_tokens":21000,"output_tokens":5}}}
`

func TestParseSession(t *testing.T) {
	s, err := ParseSession(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "s1" {
		t.Fatalf("id %q", s.ID)
	}
	var human, assist int
	var u Usage
	for _, tu := range s.Turns {
		if tu.Human {
			human++
		}
		if tu.Role == "assistant" {
			assist++
			u.Input += tu.Usage.Input
			u.CacheRead += tu.Usage.CacheRead
		}
	}
	if human != 1 || assist != 2 {
		t.Fatalf("human %d assistant %d", human, assist)
	}
	if u.CacheRead != 41000 {
		t.Fatalf("usage counted twice: %d", u.CacheRead)
	}
	a := s.Turns[1]
	if strings.Join(a.Tools, ",") != "Read" || strings.Join(a.Files, ",") != "/p/spec.pdf" {
		t.Fatalf("tools %v files %v", a.Tools, a.Files)
	}
	last := s.Turns[len(s.Turns)-1]
	if strings.Join(last.Skills, ",") != "deploy" || len(last.Tools) != 2 {
		t.Fatalf("skills %v tools %v", last.Skills, last.Tools)
	}
}

func FuzzParseSession(f *testing.F) {
	f.Add(sample)
	f.Fuzz(func(t *testing.T, in string) {
		_, _ = ParseSession(strings.NewReader(in))
	})
}

func TestEncodeDir(t *testing.T) {
	if got := EncodeDir("/home/me/ws/uplytics.ai"); got != "-home-me-ws-uplytics-ai" {
		t.Fatal(got)
	}
}

func TestSubagentType(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "agent-a1.jsonl")
	if SubagentType(p) != "" {
		t.Fatal("type without a meta file")
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-a1.meta.json"), []byte(`{"agentType":"Explore","finished":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := SubagentType(p); got != "Explore" {
		t.Fatalf("got %q", got)
	}
}
