// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Scope says which file a setting came from.
type Scope string

// Settings scopes, lowest precedence first.
const (
	UserScope    Scope = "user"
	ProjectScope Scope = "project"
	LocalScope   Scope = "local"
)

// SettingsFile is one settings.json, kept as raw JSON so tacet can write
// it back without dropping keys it doesn't know.
type SettingsFile struct {
	Scope Scope
	Path  string
	Data  map[string]any
}

// SettingsPaths are the settings files for root, lowest precedence first.
func SettingsPaths(root string) []SettingsFile {
	return []SettingsFile{
		{Scope: UserScope, Path: filepath.Join(Home(), "settings.json")},
		{Scope: ProjectScope, Path: filepath.Join(root, ".claude", "settings.json")},
		{Scope: LocalScope, Path: filepath.Join(root, ".claude", "settings.local.json")},
	}
}

// ReadSettings loads every settings file that exists.
func ReadSettings(root string) []SettingsFile {
	var out []SettingsFile
	for _, f := range SettingsPaths(root) {
		b, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		if json.Unmarshal(b, &f.Data) != nil {
			continue
		}
		out = append(out, f)
	}
	return out
}

// Env is the merged env block: a later scope wins, the process env last.
func Env(files []SettingsFile, key string) (string, bool) {
	if v, ok := os.LookupEnv(key); ok {
		return v, true
	}
	val, found := "", false
	for _, f := range files {
		if env, ok := f.Data["env"].(map[string]any); ok {
			if v, ok := env[key].(string); ok {
				val, found = v, true
			}
		}
	}
	return val, found
}

// Denies is every permissions.deny rule across scopes.
func Denies(files []SettingsFile) []string {
	var out []string
	for _, f := range files {
		p, _ := f.Data["permissions"].(map[string]any)
		out = append(out, asStrings(p["deny"])...)
	}
	return out
}

func asStrings(v any) []string {
	arr, _ := v.([]any)
	var out []string
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// MCPServer is one configured MCP server.
type MCPServer struct {
	Name    string
	Scope   Scope
	Source  string // file it was found in
	Command string
	Args    []string
	URL     string
	Plugin  string // set when a plugin provides it
}

type mcpFile struct {
	MCPServers map[string]struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
		URL     string   `json:"url"`
	} `json:"mcpServers"`
}

// MCPServers lists the servers a session in root would start: user and
// local scope from ~/.claude.json, project scope from .mcp.json, plus
// enabled plugins' servers.
func MCPServers(root string) []MCPServer {
	var out []MCPServer
	add := func(m mcpFile, sc Scope, src, plugin string) {
		names := make([]string, 0, len(m.MCPServers))
		for n := range m.MCPServers {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			s := m.MCPServers[n]
			out = append(out, MCPServer{Name: n, Scope: sc, Source: src, Command: s.Command, Args: s.Args, URL: s.URL, Plugin: plugin})
		}
	}
	if b, err := os.ReadFile(GlobalConfig()); err == nil {
		var g struct {
			mcpFile
			Projects map[string]mcpFile `json:"projects"`
		}
		if json.Unmarshal(b, &g) == nil {
			add(g.mcpFile, UserScope, GlobalConfig(), "")
			if p, ok := g.Projects[root]; ok {
				add(p, LocalScope, GlobalConfig(), "")
			}
		}
	}
	proj := filepath.Join(root, ".mcp.json")
	if b, err := os.ReadFile(proj); err == nil { // #nosec G304 -- the project's MCP config
		var m mcpFile
		if json.Unmarshal(b, &m) == nil {
			add(m, ProjectScope, proj, "")
		}
	}
	for _, p := range Plugins() {
		b, err := os.ReadFile(filepath.Join(p.Path, ".mcp.json")) // #nosec G304 -- an installed plugin
		if err != nil {
			continue
		}
		var m mcpFile
		if json.Unmarshal(b, &m) == nil {
			add(m, UserScope, p.Path, p.Name)
		}
	}
	return out
}

// GlobalConfig is ~/.claude.json, beside the config dir.
func GlobalConfig() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, ".claude.json")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude.json")
}

// Plugin is an installed plugin.
type Plugin struct {
	Name string // name@marketplace
	Path string
}

// Plugins lists installed plugins from installed_plugins.json.
func Plugins() []Plugin {
	b, err := os.ReadFile(filepath.Join(Home(), "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var f struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	var out []Plugin
	for name, raw := range f.Plugins {
		var many []struct {
			InstallPath string `json:"installPath"`
		}
		var one struct {
			InstallPath string `json:"installPath"`
		}
		switch {
		case json.Unmarshal(raw, &many) == nil && len(many) > 0:
			out = append(out, Plugin{name, many[len(many)-1].InstallPath})
		case json.Unmarshal(raw, &one) == nil && one.InstallPath != "":
			out = append(out, Plugin{name, one.InstallPath})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Skill is one skill's listing entry: what stays in context.
type Skill struct {
	Name        string
	Path        string
	Description string // description plus when_to_use, as listed
	Scope       Scope
	Plugin      string
	ModelHidden bool // disable-model-invocation: the listing skips it
}

// Skills lists user, project and plugin skills.
func Skills(root string) []Skill {
	var out []Skill
	scan := func(dir string, sc Scope, plugin string) {
		m, _ := filepath.Glob(filepath.Join(dir, "*", "SKILL.md"))
		for _, p := range m {
			sk := readSkill(p)
			sk.Scope, sk.Plugin = sc, plugin
			out = append(out, sk)
		}
	}
	scan(filepath.Join(Home(), "skills"), UserScope, "")
	scan(filepath.Join(root, ".claude", "skills"), ProjectScope, "")
	for _, p := range Plugins() {
		scan(filepath.Join(p.Path, "skills"), UserScope, p.Name)
	}
	return out
}

func readSkill(path string) Skill {
	sk := Skill{Name: filepath.Base(filepath.Dir(path)), Path: path}
	f, err := os.Open(path) // #nosec G304 -- a skill file Claude Code lists
	if err != nil {
		return sk
	}
	defer func() { _ = f.Close() }()
	fm := Frontmatter(bufio.NewReader(f))
	if v := fm["name"]; v != "" {
		sk.Name = v
	}
	sk.Description = strings.TrimSpace(fm["description"] + " " + fm["when_to_use"])
	sk.ModelHidden = fm["disable-model-invocation"] == "true"
	return sk
}

// Frontmatter reads simple "key: value" pairs from a leading --- block.
// Folded and quoted values are joined into one line.
func Frontmatter(r *bufio.Reader) map[string]string {
	out := map[string]string{}
	first, err := r.ReadString('\n')
	if err != nil || strings.TrimSpace(first) != "---" {
		return out
	}
	key := ""
	for {
		line, err := r.ReadString('\n')
		t := strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(t) == "---" {
			break
		}
		if k, v, ok := strings.Cut(t, ":"); ok && !strings.HasPrefix(t, " ") && !strings.HasPrefix(t, "\t") {
			key = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if v == ">" || v == "|" || v == ">-" || v == "|-" {
				v = ""
			}
			out[key] = strings.Trim(v, `"'`)
		} else if key != "" && strings.TrimSpace(t) != "" {
			out[key] = strings.TrimSpace(out[key] + " " + strings.TrimSpace(t))
		}
		if err != nil {
			break
		}
	}
	return out
}
