// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package skills

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/RhythmShahriar/tacet/claude"
)

// GitHub endpoints, variables so tests can point them at a local server.
var (
	GitHubAPI = "https://api.github.com"
	GitHubRaw = "https://raw.githubusercontent.com"
)

// Limits on what one import downloads.
const (
	MaxImportFiles = 100
	MaxImportBytes = 2 << 20
)

// Errors an import can end with.
var (
	ErrSource        = errors.New("not a skill source")
	ErrNoSkill       = errors.New("no SKILL.md there")
	ErrNoFrontmatter = errors.New("SKILL.md has no frontmatter with a description")
	ErrTooBig        = errors.New("skill is too big to import")
)

// Example is one well-known public skill for tacet skills add --list.
type Example struct {
	Source string `json:"source"`
	About  string `json:"about"`
}

// Examples are public skills people often start from: examples, not
// endorsements. Nothing is fetched until one is added.
var Examples = []Example{
	{"anthropics/skills/skills/skill-creator", "Anthropic's guide to writing and testing skills"},
	{"anthropics/skills/skills/frontend-design", "distinctive, production-grade frontend pages and components"},
	{"anthropics/skills/skills/webapp-testing", "tests a local web app with Playwright (has scripts)"},
	{"anthropics/skills/skills/mcp-builder", "building MCP servers that expose an API as tools"},
	{"anthropics/skills/skills/pdf", "reads, fills and merges PDF files (has scripts)"},
	{"obra/superpowers/skills/systematic-debugging", "finds the root cause before changing code"},
	{"obra/superpowers/skills/test-driven-development", "writes the failing test first, then the code"},
}

// Source is where a skill comes from: a folder in a GitHub repo, or one
// SKILL.md at a URL.
type Source struct {
	Owner, Repo, Path, Ref string
	File                   bool   // only the SKILL.md at Path, not the folder
	URL                    string // a SKILL.md outside GitHub
}

var (
	ghName   = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	shaRe    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	skillRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	badPaths = regexp.MustCompile(`(^|/)\.\.?(/|$)`)
)

// ParseSource reads owner/repo/path[@ref], a github.com tree or blob
// link, a raw.githubusercontent.com SKILL.md or any https SKILL.md URL.
func ParseSource(s string) (Source, error) {
	s = strings.TrimSpace(s)
	bad := fmt.Errorf("%w: %q (use owner/repo/path/to/skill[@ref] or a SKILL.md URL)", ErrSource, s)
	switch {
	case strings.HasPrefix(s, "https://raw.githubusercontent.com/"):
		parts := strings.SplitN(strings.TrimPrefix(s, "https://raw.githubusercontent.com/"), "/", 4)
		if len(parts) < 4 || path.Base(parts[3]) != "SKILL.md" {
			return Source{}, bad
		}
		return checkGitHub(Source{Owner: parts[0], Repo: parts[1], Ref: parts[2], Path: path.Dir(parts[3]), File: true}, bad)
	case strings.HasPrefix(s, "https://github.com/"), strings.HasPrefix(s, "github.com/"):
		rest := strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "github.com/")
		parts := strings.SplitN(strings.Trim(rest, "/"), "/", 5)
		if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
			src := Source{Owner: parts[0], Repo: parts[1], Ref: parts[3]}
			if len(parts) == 5 {
				src.Path = parts[4]
			}
			if path.Base(src.Path) == "SKILL.md" {
				src.Path = path.Dir(src.Path)
			}
			return checkGitHub(src, bad)
		}
		return parseShort(rest, bad)
	case strings.HasPrefix(s, "https://"):
		u, err := url.Parse(s)
		if err != nil || u.Host == "" || path.Base(u.Path) != "SKILL.md" {
			return Source{}, bad
		}
		return Source{URL: s, File: true}, nil
	case strings.Contains(s, "://"):
		return Source{}, fmt.Errorf("%w: %q (only https)", ErrSource, s)
	}
	return parseShort(s, bad)
}

func parseShort(s string, bad error) (Source, error) {
	s, ref, _ := strings.Cut(s, "@")
	parts := strings.SplitN(strings.Trim(s, "/"), "/", 3)
	if len(parts) < 2 {
		return Source{}, bad
	}
	src := Source{Owner: parts[0], Repo: parts[1], Ref: ref}
	if len(parts) == 3 {
		src.Path = strings.Trim(parts[2], "/")
	}
	if path.Base(src.Path) == "SKILL.md" {
		src.Path, src.File = path.Dir(src.Path), true
	}
	return checkGitHub(src, bad)
}

func checkGitHub(src Source, bad error) (Source, error) {
	if src.Path == "." {
		src.Path = ""
	}
	if !ghName.MatchString(src.Owner) || !ghName.MatchString(src.Repo) || badPaths.MatchString(src.Path) ||
		strings.ContainsAny(src.Ref, " \t\n?#") {
		return Source{}, bad
	}
	return src, nil
}

// String is the source as a person reads it, without a scheme.
func (s Source) String() string {
	if s.URL != "" {
		return strings.TrimPrefix(s.URL, "https://")
	}
	out := "github.com/" + s.Owner + "/" + s.Repo
	if s.Path != "" {
		out += "/" + s.Path
	}
	return out
}

// ImportFile is one file of an imported skill.
type ImportFile struct {
	Path   string `json:"path"` // relative to the skill folder, with /
	Size   int    `json:"size"`
	Script string `json:"script,omitempty"` // why it counts as code: script, executable, binary, link
	Data   []byte `json:"-"`
}

// Import is a skill downloaded and ready to write.
type Import struct {
	Source      Source            `json:"-"`
	Origin      string            `json:"source"`
	Commit      string            `json:"commit"` // a git commit, or sha256:<hex> for a plain URL
	Name        string            `json:"name"`
	Frontmatter map[string]string `json:"frontmatter"`
	Files       []ImportFile      `json:"files"`
}

// Size is the bytes of every file.
func (im *Import) Size() int {
	n := 0
	for _, f := range im.Files {
		n += f.Size
	}
	return n
}

// Scripts are the files that would run code: scripts, executables,
// binaries and links.
func (im *Import) Scripts() []ImportFile {
	var out []ImportFile
	for _, f := range im.Files {
		if f.Script != "" {
			out = append(out, f)
		}
	}
	return out
}

// Description is the text Claude Code lists for the skill every session.
func (im *Import) Description() string {
	return strings.TrimSpace(im.Frontmatter["description"] + " " + im.Frontmatter["when_to_use"])
}

// Fetch downloads a skill, pinned to the commit its ref names today.
func Fetch(ctx context.Context, client *http.Client, src Source) (*Import, error) {
	im := &Import{Source: src, Origin: src.String()}
	if src.URL != "" {
		b, err := get(ctx, client, src.URL, "", MaxImportBytes)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		im.Commit = "sha256:" + hex.EncodeToString(sum[:])
		im.Files = []ImportFile{importFile("SKILL.md", "100644", b)}
		return im, im.finish(path.Base(path.Dir(mustPath(src.URL))))
	}
	ref := src.Ref
	if ref == "" {
		ref = "HEAD"
	}
	sha, err := get(ctx, client, GitHubAPI+"/repos/"+src.Owner+"/"+src.Repo+"/commits/"+url.PathEscape(ref), "application/vnd.github.sha", 128)
	if err != nil {
		return nil, fmt.Errorf("resolve %s@%s: %w", src.Owner+"/"+src.Repo, ref, err)
	}
	im.Commit = strings.TrimSpace(string(sha))
	if !shaRe.MatchString(im.Commit) {
		return nil, fmt.Errorf("resolve %s@%s: GitHub answered %q, not a commit", src.Owner+"/"+src.Repo, ref, shortText(im.Commit))
	}
	type entry struct {
		Path, Mode, Type string
		Size             int
	}
	var want []entry
	if src.File {
		want = []entry{{Path: path.Join(src.Path, "SKILL.md"), Mode: "100644", Type: "blob"}}
	} else {
		b, err := get(ctx, client, GitHubAPI+"/repos/"+src.Owner+"/"+src.Repo+"/git/trees/"+im.Commit+"?recursive=1", "application/vnd.github+json", 32<<20)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", im.Origin, err)
		}
		var tree struct {
			Tree      []entry `json:"tree"`
			Truncated bool    `json:"truncated"`
		}
		if err := json.Unmarshal(b, &tree); err != nil {
			return nil, fmt.Errorf("list %s: %w", im.Origin, err)
		}
		prefix := ""
		if src.Path != "" {
			prefix = src.Path + "/"
		}
		total := 0
		for _, e := range tree.Tree {
			if e.Type != "blob" || !strings.HasPrefix(e.Path, prefix) {
				continue
			}
			want = append(want, e)
			total += e.Size
		}
		if len(want) > MaxImportFiles || total > MaxImportBytes {
			return nil, fmt.Errorf("%w: %d files, %d bytes (the cap is %d files, %d bytes)", ErrTooBig, len(want), total, MaxImportFiles, MaxImportBytes)
		}
	}
	total := 0
	for _, e := range want {
		rel := strings.TrimPrefix(strings.TrimPrefix(e.Path, src.Path), "/")
		if e.Mode == "120000" {
			im.Files = append(im.Files, ImportFile{Path: rel, Size: e.Size, Script: "link"})
			continue // a link is never followed or written
		}
		b, err := get(ctx, client, GitHubRaw+"/"+src.Owner+"/"+src.Repo+"/"+im.Commit+"/"+escapePath(e.Path), "", MaxImportBytes-total)
		if errors.Is(err, errNotFound) && src.File {
			return nil, fmt.Errorf("%w: %s at %s", ErrNoSkill, im.Origin, im.Commit[:7])
		}
		if err != nil {
			return nil, fmt.Errorf("download %s: %w", e.Path, err)
		}
		total += len(b)
		im.Files = append(im.Files, importFile(rel, e.Mode, b))
	}
	name := src.Repo
	if src.Path != "" {
		name = path.Base(src.Path)
	}
	return im, im.finish(name)
}

// finish reads SKILL.md's frontmatter and picks the folder name.
func (im *Import) finish(fallback string) error {
	var skill *ImportFile
	for i := range im.Files {
		if im.Files[i].Path == "SKILL.md" {
			skill = &im.Files[i]
		}
	}
	if skill == nil || skill.Script == "link" {
		return fmt.Errorf("%w: %s", ErrNoSkill, im.Origin)
	}
	skill.Script = ""
	if bytes.IndexByte(skill.Data, 0) >= 0 {
		return fmt.Errorf("%w: SKILL.md is binary", ErrNoSkill)
	}
	im.Frontmatter = claude.Frontmatter(bufio.NewReader(bytes.NewReader(skill.Data)))
	if im.Frontmatter["description"] == "" {
		return ErrNoFrontmatter
	}
	for _, n := range []string{im.Frontmatter["name"], strings.ToLower(fallback), kebab(fallback)} {
		if skillRe.MatchString(n) {
			im.Name = n
			return nil
		}
	}
	return fmt.Errorf("%w: no usable skill name in the frontmatter or the folder", ErrNoSkill)
}

// scriptExt are file types that run as code.
var scriptExt = map[string]bool{
	".sh": true, ".bash": true, ".zsh": true, ".fish": true, ".py": true, ".js": true, ".mjs": true, ".cjs": true,
	".ts": true, ".rb": true, ".pl": true, ".php": true, ".ps1": true, ".bat": true, ".cmd": true, ".exe": true,
	".dll": true, ".so": true, ".dylib": true, ".jar": true, ".wasm": true, ".lua": true, ".go": true, ".rs": true,
}

func importFile(rel, mode string, b []byte) ImportFile {
	f := ImportFile{Path: rel, Size: len(b), Data: b}
	head := b
	if len(head) > 8000 {
		head = head[:8000]
	}
	switch {
	case bytes.IndexByte(head, 0) >= 0:
		f.Script = "binary"
	case mode == "100755":
		f.Script = "executable"
	case scriptExt[strings.ToLower(path.Ext(rel))], bytes.HasPrefix(b, []byte("#!")):
		f.Script = "script"
	}
	return f
}

// Write puts the skill in root/.claude/skills/<name>/, recording where
// it came from in SKILL.md's frontmatter. It never overwrites a skill
// and writes no links; scripts only when allowScripts is set.
func (im *Import) Write(root string, allowScripts bool) (string, error) {
	if s := im.Scripts(); len(s) > 0 && !allowScripts {
		return "", fmt.Errorf("skill has %d script or binary file(s)", len(s))
	}
	dir := filepath.Join(root, ".claude", "skills", im.Name)
	if _, err := os.Lstat(dir); err == nil {
		return "", fmt.Errorf("%w: %s", ErrExists, filepath.Join(".claude", "skills", im.Name))
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return "", err
	}
	if err := os.Mkdir(dir, 0o750); err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("%w: %s", ErrExists, filepath.Join(".claude", "skills", im.Name))
		}
		return "", err
	}
	for _, f := range im.Files {
		if f.Script == "link" {
			continue
		}
		if !filepath.IsLocal(filepath.FromSlash(f.Path)) {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("%w: unsafe path %q", ErrSource, f.Path)
		}
		data, mode := f.Data, os.FileMode(0o644)
		if f.Path == "SKILL.md" {
			data = im.withHeader(data)
		}
		if f.Script == "executable" {
			mode = 0o755
		}
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			_ = os.RemoveAll(dir)
			return "", err
		}
		if err := os.WriteFile(p, data, mode); err != nil { // #nosec G306 -- skill files are read by Claude Code
			_ = os.RemoveAll(dir)
			return "", err
		}
	}
	return dir, nil
}

// withHeader adds a YAML comment right after the opening --- that says
// where the skill came from. YAML ignores it, so the skill loads as before.
func (im *Import) withHeader(b []byte) []byte {
	head := fmt.Sprintf("# imported by tacet skills add from %s at %s\n", im.Origin, im.Commit)
	nl := bytes.IndexByte(b, '\n')
	if nl < 0 {
		return b
	}
	out := append([]byte{}, b[:nl+1]...)
	out = append(out, head...)
	return append(out, b[nl+1:]...)
}

var errNotFound = errors.New("not found")

// get fetches one URL, at most max bytes.
func get(ctx context.Context, client *http.Client, u, accept string, max int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("User-Agent", "tacet")
	resp, err := client.Do(req) // #nosec G107 -- the source the person typed
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnprocessableEntity:
		return nil, fmt.Errorf("%w (%s)", errNotFound, resp.Status)
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("GitHub said %s: the API allows 60 requests an hour without a token, try again later", resp.Status)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s", resp.Status)
	}
	if max <= 0 {
		return nil, ErrTooBig
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > max {
		return nil, ErrTooBig
	}
	return b, nil
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, x := range parts {
		parts[i] = url.PathEscape(x)
	}
	return strings.Join(parts, "/")
}

func mustPath(u string) string {
	if p, err := url.Parse(u); err == nil {
		return p.Path
	}
	return ""
}

func shortText(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}
