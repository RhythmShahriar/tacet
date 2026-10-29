// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package skills

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/RhythmShahriar/tacet/claude"
)

// ErrExists means a skill by that name is already there; tacet never
// overwrites one.
var ErrExists = errors.New("a skill by that name already exists")

// ErrNoSession means there was no transcript or no request in it.
var ErrNoSession = errors.New("no session with a request to draft from")

// On-demand draft limits.
const (
	maxRequests = 3  // recent requests a session draft reads
	maxSteps    = 15 // numbered steps it keeps
	maxCommand  = 120
)

// OnDemand is a skill drafted when the person asks for one: from a
// description or from what happened in a session.
type OnDemand struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Path        string   `json:"path"`
	Source      string   `json:"source"` // "description" or "session"
	Requests    []string `json:"requests,omitempty"`
	Steps       []string `json:"steps,omitempty"`
	Skill       string   `json:"skill"` // the SKILL.md it writes
}

// NewFromDescription drafts a skill from what the person described.
// name overrides the derived one when not empty.
func NewFromDescription(root, text, name string) (OnDemand, error) {
	text = strings.TrimSpace(Mask(text))
	if text == "" {
		return OnDemand{}, errors.New("describe the skill: tacet skills new \"<what it does>\"")
	}
	d := OnDemand{Source: "description", Description: Sentence(text), Steps: splitSteps(text)}
	d.name(root, name, text)
	body := text
	if len(d.Steps) > 0 {
		body = "" // the steps say it
	}
	d.Skill = d.draft(body, "a description")
	return d, nil
}

// NewFromSession drafts a skill from a transcript: the person's recent
// requests plus the steps the agent took for them. An empty transcript
// means the project's newest session, which is the open one when this
// runs inside Claude Code. hint, when given, is the description.
func NewFromSession(root, transcript, hint, name string) (OnDemand, error) {
	if transcript == "" {
		for _, p := range claude.Transcripts(root) {
			transcript = p // newest last
		}
	}
	if transcript == "" {
		return OnDemand{}, ErrNoSession
	}
	reqs, steps, err := readSession(root, transcript)
	if err != nil {
		return OnDemand{}, err
	}
	if len(reqs) == 0 && strings.TrimSpace(hint) == "" {
		return OnDemand{}, ErrNoSession
	}
	d := OnDemand{Source: "session", Requests: reqs, Steps: steps}
	about := strings.TrimSpace(Mask(hint))
	if about == "" {
		about = mainRequest(reqs)
	}
	d.Description = Sentence(about)
	d.name(root, name, about)
	d.Skill = d.draft("", "this session")
	return d, nil
}

func (d *OnDemand) name(root, name, about string) {
	if name = kebab(name); name == "" {
		name = Name([]string{about})
		if name == "repeated-request" {
			name = "new-skill"
		}
		name = uniqueName(name, Used(root))
	}
	d.Name = name
	d.Path = filepath.Join(root, ".claude", "skills", name, "SKILL.md")
}

// Write creates the skill file. It fails with ErrExists rather than
// touch a skill that is already there.
func (d OnDemand) Write() error {
	if err := os.MkdirAll(filepath.Dir(d.Path), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(d.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- under the project's .claude/skills
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%w: %s", ErrExists, d.Path)
	}
	if err != nil {
		return err
	}
	if _, err := f.WriteString(d.Skill); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Sentence is a one-sentence description starting "Use when", with the
// trigger words first and at most MaxDescription characters.
func Sentence(text string) string {
	first, _ := FirstSentence(text, MaxDescription)
	if strings.HasPrefix(strings.ToLower(first), "use when") {
		first = strings.TrimRight(first, ".!? ")
	} else {
		first = strings.TrimSuffix(Describe(first), ".")
	}
	if r := []rune(first); len(r) > MaxDescription-1 {
		first, _ = FirstSentence(first, MaxDescription-1)
		first = strings.TrimSuffix(first, "...")
	}
	return first + "."
}

func (d OnDemand) draft(body, from string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + d.Name + "\n")
	b.WriteString("description: " + quote(d.Description) + "\n")
	b.WriteString("---\n")
	fmt.Fprintf(&b, "<!-- tacet · drafted from %s -->\n\n", from)
	b.WriteString("# " + title(d.Name) + "\n\n")
	if body != "" {
		b.WriteString(body + "\n")
	}
	if len(d.Requests) > 0 {
		b.WriteString("What was asked:\n\n")
		for _, r := range d.Requests {
			b.WriteString("- " + strings.Join(strings.Fields(r), " ") + "\n")
		}
	}
	if len(d.Steps) > 0 {
		if body != "" || len(d.Requests) > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## Steps\n\n")
		for i, s := range d.Steps {
			fmt.Fprintf(&b, "%d. %s\n", i+1, s)
		}
	}
	return b.String()
}

var stepSplit = regexp.MustCompile(`(?i)\s*;\s*(?:and\s+)?(?:then\s+)?|,?\s+(?:and\s+)?then\s+|[.!]\s+`)

// splitSteps reads "run the tests, then tag a release" as two steps; a
// description with one step has none.
func splitSteps(text string) []string {
	var out []string
	for _, p := range stepSplit.Split(text, -1) {
		if p = strings.TrimRight(unpolite(p), "."); p != "" {
			out = append(out, capital(p))
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

var kebabRe = regexp.MustCompile(`[^a-z0-9]+`)

func kebab(s string) string {
	s = strings.Trim(kebabRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 48 {
		s = strings.TrimRight(s[:48], "-")
	}
	return s
}

// toolUse is the part of a tool_use block a step is made from.
type toolUse struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Input struct {
		Command      string `json:"command"`
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Pattern      string `json:"pattern"`
		Skill        string `json:"skill"`
	} `json:"input"`
}

// readSession is the recent requests of a transcript and the steps
// taken after the first of them, up to the request to make a skill.
func readSession(root, path string) (reqs, steps []string, err error) {
	s, err := claude.ReadSession(path)
	if s == nil {
		return nil, nil, err
	}
	uses, err := toolUses(path)
	if err != nil {
		return nil, nil, err
	}
	turns := s.Turns
	// The trailing "make this a skill" exchange is not part of the work.
	cut := len(turns)
	for i := len(turns) - 1; i >= 0; i-- {
		t := turns[i]
		if !t.Human {
			continue
		}
		if asksForSkill(t.Text) {
			cut = i
			continue
		}
		if len(strings.Fields(t.Text)) < minWords {
			continue // "yes", "go ahead"
		}
		break
	}
	start := cut
	var picked []string
	for i := cut - 1; i >= 0 && len(picked) < maxRequests; i-- {
		t := turns[i]
		if !t.Human {
			continue
		}
		if asksForSkill(t.Text) {
			break // an earlier skill was made from what came before
		}
		if skipPrompt(t.Text) || len(strings.Fields(t.Text)) < minWords {
			continue
		}
		picked = append(picked, Mask(t.Text))
		start = i
	}
	for i := len(picked) - 1; i >= 0; i-- {
		reqs = append(reqs, picked[i])
	}
	var all []step
	for _, t := range turns[start:cut] {
		if t.Role != "assistant" || t.MessageID == "" {
			continue
		}
		for _, u := range uses[t.MessageID] {
			if st, ok := toStep(root, u); ok {
				all = append(all, st)
			}
		}
	}
	return reqs, condense(all), nil
}

// mainRequest is the request that says the most: the task, where the
// others are follow-ups and corrections.
func mainRequest(reqs []string) string {
	best := reqs[0]
	for _, r := range reqs[1:] {
		if len(contentWords(r)) > len(contentWords(best)) {
			best = r
		}
	}
	return best
}

// asksForSkill is a short request to save what was done as a skill.
func asksForSkill(text string) bool {
	low := strings.ToLower(strings.TrimSpace(text))
	if strings.HasPrefix(low, "/tacet skill") {
		return true
	}
	if !strings.Contains(low, "skill") || len(strings.Fields(low)) > 20 {
		return false
	}
	for _, v := range []string{"make", "save", "turn", "create", "draft", "write", "tacet"} {
		if strings.Contains(low, v) {
			return true
		}
	}
	return false
}

// toolUses reads every assistant tool_use block, by message id, in order.
func toolUses(path string) (map[string][]toolUse, error) {
	f, err := os.Open(path) // #nosec G304 -- a transcript tacet listed or was given
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	out := map[string][]toolUse{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var l struct {
			Type    string `json:"type"`
			Message *struct {
				ID      string          `json:"id"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Type != "assistant" || l.Message == nil {
			continue
		}
		var bs []toolUse
		if json.Unmarshal(l.Message.Content, &bs) != nil {
			continue
		}
		for _, b := range bs {
			if b.Type == "tool_use" && b.Name != "" {
				out[l.Message.ID] = append(out[l.Message.ID], b)
			}
		}
	}
	return out, sc.Err()
}

// step is one thing the agent did: a verb and what it did it to.
type step struct {
	verb, arg string
}

// Tools that plan or look things up rather than do the work.
var quietTools = map[string]bool{
	"TodoWrite": true, "ToolSearch": true, "AskUserQuestion": true, "ExitPlanMode": true, "EnterPlanMode": true,
	"TaskOutput": true, "BashOutput": true, "KillShell": true, "Monitor": true, "SubagentHandback": true,
}

// lookRe is a shell command that only looks around.
var lookRe = regexp.MustCompile(`^(cd \S+ && )?(ls|cat|head|tail|less|grep|rg|find|wc|pwd|echo|which|tree|file|stat|sed -n|git (status|diff|log|show|branch))\b`)

func toStep(root string, u toolUse) (step, bool) {
	switch u.Name {
	case "Bash":
		c := strings.TrimSpace(strings.SplitN(u.Input.Command, "\n", 2)[0])
		if c == "" || lookRe.MatchString(c) || strings.Contains(c, "skills new") {
			return step{}, false
		}
		c = strings.ReplaceAll(c, root+string(filepath.Separator), "")
		c = strings.ReplaceAll(c, "cd "+root+" && ", "")
		if r := []rune(c); len(r) > maxCommand {
			c = string(r[:maxCommand]) + "..."
		}
		return step{"Run", "`" + Mask(c) + "`"}, true
	case "Read":
		return step{"Read", file(root, u.Input.FilePath)}, u.Input.FilePath != ""
	case "Edit", "MultiEdit":
		return step{"Edit", file(root, u.Input.FilePath)}, u.Input.FilePath != ""
	case "Write":
		return step{"Write", file(root, u.Input.FilePath)}, u.Input.FilePath != ""
	case "NotebookEdit":
		return step{"Edit", file(root, u.Input.NotebookPath)}, u.Input.NotebookPath != ""
	case "Grep", "Glob":
		return step{"Search for", "`" + Mask(u.Input.Pattern) + "`"}, u.Input.Pattern != ""
	case "Skill":
		return step{"Use the skill", "`" + u.Input.Skill + "`"}, u.Input.Skill != ""
	}
	if quietTools[u.Name] {
		return step{}, false
	}
	return step{"Use", u.Name}, true
}

func file(root, p string) string {
	return "`" + Mask(filepath.ToSlash(rel(root, p))) + "`"
}

// condense numbers the steps: a command run twice counts once and a run
// of the same verb joins into one step ("Edit a, b").
func condense(all []step) []string {
	seen := map[step]bool{}
	var out []string
	var verb string
	var args []string
	flush := func() {
		if verb != "" {
			out = append(out, verb+" "+strings.Join(args, ", "))
		}
		verb, args = "", nil
	}
	for _, s := range all {
		if seen[s] {
			continue
		}
		seen[s] = true
		if s.verb == verb && s.verb != "Run" && len(args) < 5 {
			args = append(args, s.arg)
			continue
		}
		flush()
		verb, args = s.verb, []string{s.arg}
	}
	flush()
	if len(out) > maxSteps {
		out = out[:maxSteps]
	}
	return out
}
