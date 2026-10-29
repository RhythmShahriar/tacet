// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package lint

import (
	"regexp"
	"strings"
)

// tomlEntry is one key in a TOML file: the table it sits in, its key and
// its raw value with multi-line arrays joined. Enough to read scripts,
// aliases and dependency names; not a full TOML parser.
type tomlEntry struct{ table, key, value string }

var tomlHeaderRe = regexp.MustCompile(`^\[\[?\s*([^\]]+?)\s*\]\]?\s*(#.*)?$`)

// scanTOML reads the tables and keys of a TOML document.
func scanTOML(text string) (entries []tomlEntry, tables []string) {
	table := ""
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if m := tomlHeaderRe.FindStringSubmatch(l); m != nil {
			table = unquoteKey(m[1])
			tables = append(tables, table)
			continue
		}
		eq := keyEnd(l)
		if eq < 0 {
			continue
		}
		key := unquoteKey(strings.TrimSpace(l[:eq]))
		val := strings.TrimSpace(l[eq+1:])
		switch {
		case strings.HasPrefix(val, `"""`) || strings.HasPrefix(val, `'''`):
			q := val[:3]
			body := val[3:]
			for !strings.Contains(body, q) && i+1 < len(lines) {
				i++
				body += "\n" + lines[i]
			}
			val = q + body
		case strings.HasPrefix(val, "[") || strings.HasPrefix(val, "{"):
			for depth(val) > 0 && i+1 < len(lines) {
				i++
				val += "\n" + stripComment(lines[i])
			}
		}
		entries = append(entries, tomlEntry{table, key, stripComment(val)})
	}
	return entries, tables
}

// keyEnd is the index of the = that ends a key, skipping quoted keys.
func keyEnd(l string) int {
	inQ := byte(0)
	for i := 0; i < len(l); i++ {
		c := l[i]
		switch {
		case inQ != 0:
			if c == inQ {
				inQ = 0
			}
		case c == '"' || c == '\'':
			inQ = c
		case c == '=':
			return i
		}
	}
	return -1
}

// depth counts open brackets and braces outside strings.
func depth(s string) int {
	n := 0
	inQ := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQ != 0:
			if c == inQ {
				inQ = 0
			}
		case c == '"' || c == '\'':
			inQ = c
		case c == '#':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '[' || c == '{':
			n++
		case c == ']' || c == '}':
			n--
		}
	}
	return n
}

// stripComment drops a trailing # comment outside strings.
func stripComment(s string) string {
	inQ := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQ != 0:
			if c == inQ {
				inQ = 0
			}
		case c == '"' || c == '\'':
			inQ = c
		case c == '#':
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

// unquoteKey turns `tool."my-task"` into tool.my-task.
func unquoteKey(k string) string {
	parts := strings.Split(k, ".")
	for i, p := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(p), `"'`)
	}
	return strings.Join(parts, ".")
}

var quotedRe = regexp.MustCompile(`"([^"]*)"|'([^']*)'`)

// tomlStrings is every quoted string in a raw value.
func tomlStrings(v string) []string {
	var out []string
	for _, m := range quotedRe.FindAllStringSubmatch(v, -1) {
		out = append(out, m[1]+m[2])
	}
	return out
}

// keysOf is the first segment of each key directly in table, plus the
// names of its subtables ([table.name]).
func keysOf(entries []tomlEntry, tables []string, table string) map[string]bool {
	out := map[string]bool{}
	for _, e := range entries {
		if e.table == table {
			out[strings.SplitN(e.key, ".", 2)[0]] = true
		}
	}
	for _, t := range tables {
		if rest, ok := strings.CutPrefix(t, table+"."); ok {
			out[strings.SplitN(rest, ".", 2)[0]] = true
		}
	}
	return out
}

// iniSections reads an INI file into section name, key and value.
// Indented lines continue the previous value, as tox reads them.
func iniSections(text string) map[string]map[string]string {
	out := map[string]map[string]string{}
	sec, key := "", ""
	for _, raw := range strings.Split(text, "\n") {
		l := strings.TrimSpace(raw)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, ";") {
			continue
		}
		if strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") {
			sec, key = strings.TrimSpace(l[1:len(l)-1]), ""
			if out[sec] == nil {
				out[sec] = map[string]string{}
			}
			continue
		}
		if (raw[0] == ' ' || raw[0] == '\t') && key != "" && out[sec] != nil {
			out[sec][key] += "\n" + l
			continue
		}
		if i := strings.IndexAny(l, "=:"); i > 0 && out[sec] != nil {
			key = strings.TrimSpace(l[:i])
			out[sec][key] = strings.TrimSpace(l[i+1:])
		}
	}
	return out
}
