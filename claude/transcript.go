// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package claude

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
)

// Usage is what one model request billed.
type Usage struct {
	Input      int `json:"input_tokens"`
	CacheWrite int `json:"cache_creation_input_tokens"`
	CacheRead  int `json:"cache_read_input_tokens"`
	Output     int `json:"output_tokens"`
}

// Total is every token the request processed.
func (u Usage) Total() int { return u.Input + u.CacheWrite + u.CacheRead + u.Output }

// Turn is one line of a transcript tacet cares about.
type Turn struct {
	Role      string // "user" or "assistant"
	Time      time.Time
	Model     string
	MessageID string
	Usage     Usage
	Text      string   // the human's words, or an assistant turn's text blocks
	Tools     []string // tool names an assistant turn called
	Skills    []string // skills it invoked through the Skill tool
	Files     []string // file paths it passed to Read
	Human     bool     // a prompt the person typed, not a tool result or hook
	// ToolIDs pairs with Tools: the tool_use id of each call.
	ToolIDs []string
	// Results maps a tool_use id to the bytes of text its tool_result
	// carried, on the user line that returned it.
	Results map[string]int
}

// Session is one transcript file read into turns.
type Session struct {
	ID       string
	Path     string
	Turns    []Turn
	Subagent bool // a subagent's transcript, billed but not a session of its own
}

type rawLine struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"sessionId"`
	IsMeta    bool      `json:"isMeta"`
	Origin    *struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	Message *struct {
		ID      string          `json:"id"`
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   *Usage          `json:"usage"`
	} `json:"message"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	Input     struct {
		Skill    string `json:"skill"`
		FilePath string `json:"file_path"`
	} `json:"input"`
}

// ReadSession reads a transcript. Lines it doesn't understand are skipped,
// so a newer Claude Code format degrades instead of failing.
func ReadSession(path string) (*Session, error) {
	f, err := os.Open(path) // #nosec G304 -- a transcript path tacet listed itself
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	s, err := ParseSession(f)
	if s != nil {
		s.Path = path
	}
	return s, err
}

// ParseSession reads transcript lines from r.
func ParseSession(r io.Reader) (*Session, error) {
	s := &Session{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	seen := map[string]bool{}
	for sc.Scan() {
		var l rawLine
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Message == nil {
			continue
		}
		if s.ID == "" {
			s.ID = l.SessionID
		}
		t := Turn{Role: l.Message.Role, Time: l.Timestamp, Model: l.Message.Model, MessageID: l.Message.ID}
		switch l.Type {
		case "assistant":
			// One response is written as several lines, one per content
			// block, each repeating the usage; count it once.
			if t.MessageID != "" && seen[t.MessageID] {
				if n := len(s.Turns); n > 0 {
					addTools(&s.Turns[n-1], l.Message.Content)
				}
				continue
			}
			seen[t.MessageID] = true
			if l.Message.Usage != nil {
				t.Usage = *l.Message.Usage
			}
			addTools(&t, l.Message.Content)
		case "user":
			t.Text = userText(l.Message.Content)
			t.Human = t.Text != "" && !l.IsMeta && (l.Origin == nil || l.Origin.Kind == "human")
			t.Results = toolResults(l.Message.Content)
		default:
			continue
		}
		s.Turns = append(s.Turns, t)
	}
	return s, sc.Err()
}

func addTools(t *Turn, raw json.RawMessage) {
	var bs []block
	if json.Unmarshal(raw, &bs) != nil {
		return
	}
	for _, b := range bs {
		if b.Type == "text" && b.Text != "" {
			if t.Text != "" {
				t.Text += "\n"
			}
			t.Text += b.Text
		}
		if b.Type != "tool_use" || b.Name == "" {
			continue
		}
		t.Tools = append(t.Tools, b.Name)
		t.ToolIDs = append(t.ToolIDs, b.ID)
		if b.Name == "Skill" && b.Input.Skill != "" {
			t.Skills = append(t.Skills, b.Input.Skill)
		}
		if b.Name == "Read" && b.Input.FilePath != "" {
			t.Files = append(t.Files, b.Input.FilePath)
		}
	}
}

// userText is the typed text of a user line: a plain string, or the text
// blocks of a block list. Tool results and harness-injected blocks
// (system reminders, command output) aren't the person's words.
func userText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return cleanPrompt(s)
	}
	var bs []block
	if json.Unmarshal(raw, &bs) != nil {
		return ""
	}
	var parts []string
	for _, b := range bs {
		if b.Type == "tool_result" {
			return ""
		}
		if b.Type == "text" {
			if t := cleanPrompt(b.Text); t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// toolResults is the size in bytes of each tool_result on a user
// line, by the tool_use id it answers. Content is a string or a list of
// blocks; only text counts.
func toolResults(raw json.RawMessage) map[string]int {
	var bs []block
	if json.Unmarshal(raw, &bs) != nil {
		return nil
	}
	var out map[string]int
	for _, b := range bs {
		if b.Type != "tool_result" || b.ToolUseID == "" {
			continue
		}
		n := 0
		var s string
		if json.Unmarshal(b.Content, &s) == nil {
			n = len(s)
		} else {
			var inner []block
			if json.Unmarshal(b.Content, &inner) == nil {
				for _, x := range inner {
					n += len(x.Text)
				}
			}
		}
		if out == nil {
			out = map[string]int{}
		}
		out[b.ToolUseID] += n
	}
	return out
}

func cleanPrompt(s string) string {
	s = strings.TrimSpace(s)
	for _, p := range []string{"<system-reminder>", "<command-", "<local-command", "<ide_", "[Request interrupted", "<task-notification>"} {
		if strings.HasPrefix(s, p) {
			return ""
		}
	}
	return s
}
