// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package skills

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/docs/lint"
	"github.com/RhythmShahriar/tacet/tokens"
)

// ErrMoved means the procedure isn't in the file as it was planned.
var ErrMoved = errors.New("the procedure changed since the scan")

// Procedure is a numbered list in an always-loaded file, found the way
// the move/procedure lint rule finds it.
type Procedure struct {
	Path    string // the instruction file
	Line    int    // first step, 1-based
	Heading string
	Steps   int
	Text    string // the lines moved, exactly as written
}

var stepRe = regexp.MustCompile(`^\s*(\d+)[.)]\s+\S`)

// Procedures lists the procedures in one file's body.
func Procedures(path, body string) []Procedure {
	var out []Procedure
	lines := strings.Split(body, "\n")
	heading := ""
	start, steps, last := 0, 0, 0
	flush := func() {
		if steps >= lint.MinProcedureStep {
			out = append(out, Procedure{
				Path: path, Line: start + 1, Heading: heading, Steps: steps,
				Text: strings.Join(lines[start:last+1], "\n"),
			})
		}
		steps = 0
	}
	for n, l := range lines {
		if strings.HasPrefix(l, "#") {
			flush()
			heading = strings.TrimSpace(strings.TrimLeft(l, "#"))
			continue
		}
		if stepRe.MatchString(l) {
			if steps == 0 {
				start = n
			}
			steps++
			last = n
			continue
		}
		if strings.TrimSpace(l) == "" {
			continue
		}
		if strings.HasPrefix(l, "   ") || strings.HasPrefix(l, "\t") {
			if steps > 0 {
				last = n
			}
			continue
		}
		flush()
	}
	flush()
	return out
}

// AttachProcedureFixes gives each move/procedure finding in fs a fix
// that moves the steps into a project skill and leaves one line that
// points to it. Only files inside the project are changed; used names
// keep skill names unique across every suggestion.
func AttachProcedureFixes(set *lint.Set, fs []core.Finding, used map[string]bool) {
	byKey := map[string]Procedure{}
	for _, d := range set.All() {
		if !d.Always || d.Kind == lint.User || !inside(set.Root, d.Path) {
			continue
		}
		for _, p := range Procedures(d.Path, d.Body) {
			byKey[fmt.Sprintf("%s:%d", rel(set.Root, d.Path), p.Line)] = p
		}
	}
	for i := range fs {
		f := &fs[i]
		if f.Rule != "move/procedure" || f.Fix != nil {
			continue
		}
		p, ok := byKey[fmt.Sprintf("%s:%d", f.Path, f.Line)]
		if !ok {
			continue
		}
		f.Fix = MoveFix(set.Root, p, used)
	}
}

// MoveFix plans the move of one procedure into
// .claude/skills/<name>/SKILL.md.
func MoveFix(root string, p Procedure, used map[string]bool) *core.Fix {
	base := Name([]string{p.Heading})
	if p.Heading == "" {
		base = Name([]string{firstStep(p.Text)})
	}
	name := uniqueName(base, used)
	skill := filepath.Join(root, ".claude", "skills", name, "SKILL.md")
	pointer := pointerLine(p, name)
	old := p.Text
	return &core.Fix{
		ID:     "skill-" + name,
		Title:  fmt.Sprintf("Move the %d-step %s procedure into skill %s", p.Steps, label(p), name),
		Safe:   false,
		Tokens: tokens.Estimate(old) - tokens.Estimate(pointer),
		Edits: []core.Edit{
			{Path: skill, Old: nil, New: []byte(ProcedureSkill(p, name, rel(root, p.Path)))},
			{Path: p.Path, Change: func(cur []byte) ([]byte, error) {
				s := string(cur)
				i := strings.Index(s, old)
				if i < 0 {
					return nil, ErrMoved
				}
				return []byte(s[:i] + pointer + s[i+len(old):]), nil
			}},
		},
	}
}

// ProcedureSkill is the SKILL.md that holds a moved procedure.
func ProcedureSkill(p Procedure, name, from string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + name + "\n")
	b.WriteString("description: " + quote(fmt.Sprintf("Use for %s: the %d steps this project follows.", strings.ToLower(label(p)), p.Steps)) + "\n")
	b.WriteString("---\n")
	fmt.Fprintf(&b, "<!-- tacet · moved from %s -->\n\n", filepath.ToSlash(from))
	b.WriteString("# " + capital(label(p)) + "\n\n")
	b.WriteString(Mask(p.Text) + "\n")
	return b.String()
}

func pointerLine(p Procedure, name string) string {
	return fmt.Sprintf("%s: follow the `%s` skill.", capital(label(p)), name)
}

func label(p Procedure) string {
	if p.Heading != "" {
		return p.Heading
	}
	return "procedure"
}

func capital(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}

func firstStep(text string) string {
	l := strings.SplitN(text, "\n", 2)[0]
	return stepRe.ReplaceAllStringFunc(l, func(m string) string { return m[len(m)-1:] })
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return p
}

func inside(root, p string) bool {
	r, err := filepath.Rel(root, p)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}
