// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package skills turns what a person keeps asking for into skills: it
// mines past prompts for repeated instructions and drafts a SKILL.md for
// each. It also moves numbered procedures out of always-loaded files.
package skills

import (
	"hash/fnv"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/RhythmShahriar/tacet/claude"
)

// Thresholds a repeated instruction has to meet.
const (
	MinCount    = 3   // times it was asked
	MinSessions = 2   // in this many sessions at least
	MaxSessions = 50  // most recent transcripts read
	Similar     = 0.5 // word 3-gram Jaccard for two requests to match
	minWords    = 4   // shorter is "yes", "go on", "check now"
	minContent  = 3   // words that aren't filler
	maxWords    = 80  // a longer sentence is pasted text
	maxPrompt   = 2000
)

// Prompt is one thing the person typed in one session.
type Prompt struct {
	Session string
	Text    string
}

// Prompts reads the human turns of the project's recent sessions.
// Subagent transcripts live in sub folders and are never read.
func Prompts(root string) []Prompt {
	paths := claude.Transcripts(root)
	if len(paths) > MaxSessions {
		paths = paths[len(paths)-MaxSessions:]
	}
	var out []Prompt
	for _, p := range paths {
		s, err := claude.ReadSession(p)
		if s == nil || (err != nil && len(s.Turns) == 0) || s.Subagent {
			continue
		}
		id := s.ID
		if id == "" {
			id = p
		}
		for _, t := range s.Turns {
			if t.Human {
				out = append(out, Prompt{Session: id, Text: t.Text})
			}
		}
	}
	return out
}

// unit is one instruction sentence taken from a prompt.
type unit struct {
	session string
	prompt  int // index of the prompt it came from
	text    string
	shingle map[uint64]bool
	sig     []uint64
}

// Cluster is a repeated instruction: every sentence that says about the
// same thing plus where it was said.
type Cluster struct {
	Count    int      // prompts that asked for it
	Sessions int      // distinct sessions among them
	Texts    []string // the requests, masked, representative first
}

// Score ranks clusters: occurrences times distinct sessions.
func (c Cluster) Score() int { return c.Count * c.Sessions }

// Mine groups the prompts' instruction sentences by similarity and
// returns the clusters that repeat enough, best first.
func Mine(prompts []Prompt) []Cluster {
	var us []unit
	for i, p := range prompts {
		if skipPrompt(p.Text) {
			continue
		}
		for _, s := range sentences(Mask(p.Text)) {
			if !instruction(s) {
				continue
			}
			sh := shingles(normalize(s))
			if len(sh) == 0 {
				continue
			}
			us = append(us, unit{session: p.Session, prompt: i, text: s, shingle: sh, sig: minhash(sh)})
		}
	}
	groups := cluster(us)
	var out []Cluster
	for _, g := range groups {
		prompts, sessions := map[int]bool{}, map[string]bool{}
		for _, i := range g {
			prompts[us[i].prompt] = true
			sessions[us[i].session] = true
		}
		if len(prompts) < MinCount || len(sessions) < MinSessions {
			continue
		}
		c := Cluster{Count: len(prompts), Sessions: len(sessions)}
		rep := medoid(us, g)
		c.Texts = append(c.Texts, us[rep].text)
		seen := map[string]bool{normalize(us[rep].text): true}
		for _, i := range g {
			k := normalize(us[i].text)
			if !seen[k] {
				seen[k] = true
				c.Texts = append(c.Texts, us[i].text)
			}
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score() != out[j].Score() {
			return out[i].Score() > out[j].Score()
		}
		return out[i].Texts[0] < out[j].Texts[0]
	})
	return out
}

// skipPrompt drops whole prompts that aren't instructions: a resumed
// session's summary, slash commands, pasted logs and documents.
func skipPrompt(s string) bool {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, "This session is being continued"),
		strings.HasPrefix(s, "/"),
		len(s) > maxPrompt:
		return true
	}
	return wordShare(s) < 0.5
}

// wordShare is the part of the whitespace-separated fields that are
// plain words: low for stack traces, JSON and command output.
func wordShare(s string) float64 {
	fs := strings.Fields(s)
	if len(fs) == 0 {
		return 0
	}
	n := 0
	for _, f := range fs {
		f = strings.Trim(f, `.,;:!?'"()[]`)
		if f == "" {
			continue
		}
		plain := true
		for _, r := range f {
			if !unicode.IsLetter(r) && r != '\'' && r != '-' {
				plain = false
				break
			}
		}
		if plain {
			n++
		}
	}
	return float64(n) / float64(len(fs))
}

// tagRe is a leading [label] a script or template put before a prompt.
var tagRe = regexp.MustCompile(`^(\[[^\]]{1,40}\]\s*)+`)

var sentenceEnd = regexp.MustCompile(`([.!?])\s+`)

// sentences splits a prompt into lines and sentences.
func sentences(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "-*•> ")
		line = strings.TrimSpace(tagRe.ReplaceAllString(line, ""))
		if line == "" {
			continue
		}
		for _, p := range strings.Split(sentenceEnd.ReplaceAllString(line, "$1\x00"), "\x00") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// A question that opens like this is a request ("can you run the
// tests?"); any other question asks about something.
var requestStart = []string{"can you", "could you", "would you", "will you", "can we", "could we", "shall we", "please", "pls"}

func asksForWork(norm string) bool {
	for _, p := range requestStart {
		if strings.HasPrefix(norm, p+" ") {
			return true
		}
	}
	return false
}

var codeRef = regexp.MustCompile("`|\\w\\(\\)|\\b\\w+_\\w+\\b|\\b[a-z]+[A-Z]\\w*\\b")

// logRe spots a pasted error or stack trace on one line.
var logRe = regexp.MustCompile(`\b\w+\.(go|js|mjs|ts|tsx|py|rb|php|java|rs|c|cpp):\d+|\b0x[0-9a-f]{2,}|\bgoroutine \d+|Traceback \(most recent|^\s*at \S+ \(|\bpanic: |\bfatal error: |Exception: `)

// instruction keeps sentences that ask for something again and again:
// long enough, not a question, not mostly filler. A
// request phrased as a question counts unless it names code.
func instruction(s string) bool {
	words := strings.Fields(normalize(s))
	if len(words) < minWords || len(words) > maxWords {
		return false
	}
	if wordShare(s) < 0.6 || logRe.MatchString(s) {
		return false
	}
	if strings.HasSuffix(strings.TrimSpace(s), "?") && (!asksForWork(strings.Join(words, " ")) || codeRef.MatchString(s)) {
		return false
	}
	content := 0
	for _, w := range words {
		if !stop[w] {
			content++
		}
	}
	return content >= minContent
}

var nonWord = regexp.MustCompile(`[^a-z0-9']+`)

func normalize(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), Masked, " ")
	return strings.TrimSpace(nonWord.ReplaceAllString(s, " "))
}

// shingles are the hashed word 3-grams of normalized text; a shorter
// text is one shingle.
func shingles(norm string) map[uint64]bool {
	ws := strings.Fields(norm)
	out := map[uint64]bool{}
	if len(ws) == 0 {
		return out
	}
	if len(ws) < 3 {
		out[hash(strings.Join(ws, " "))] = true
		return out
	}
	for i := 0; i+3 <= len(ws); i++ {
		out[hash(ws[i]+" "+ws[i+1]+" "+ws[i+2])] = true
	}
	return out
}

func hash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// MinHash with bands of two rows: pairs that agree on any band become
// candidates, then the exact Jaccard decides.
const (
	numHash = 64
	rows    = 2
)

var seeds = func() [numHash][2]uint64 {
	var s [numHash][2]uint64
	x := uint64(0x9e3779b97f4a7c15)
	for i := range s {
		for j := range s[i] {
			x ^= x << 13
			x ^= x >> 7
			x ^= x << 17
			s[i][j] = x | 1
		}
	}
	return s
}()

func minhash(sh map[uint64]bool) []uint64 {
	sig := make([]uint64, numHash)
	for i := range sig {
		sig[i] = ^uint64(0)
	}
	for v := range sh {
		for i := range sig {
			h := v*seeds[i][0] + seeds[i][1]
			h ^= h >> 29
			if h < sig[i] {
				sig[i] = h
			}
		}
	}
	return sig
}

func jaccard(a, b map[uint64]bool) float64 {
	if len(a) > len(b) {
		a, b = b, a
	}
	in := 0
	for k := range a {
		if b[k] {
			in++
		}
	}
	union := len(a) + len(b) - in
	if union == 0 {
		return 0
	}
	return float64(in) / float64(union)
}

// cluster joins units whose Jaccard reaches Similar, through LSH
// candidates, with union-find. Groups come back in first-seen order.
func cluster(us []unit) [][]int {
	parent := make([]int, len(us))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	checked := map[[2]int]bool{}
	for b := 0; b < numHash/rows; b++ {
		buckets := map[[rows]uint64][]int{}
		for i, u := range us {
			var k [rows]uint64
			copy(k[:], u.sig[b*rows:(b+1)*rows])
			buckets[k] = append(buckets[k], i)
		}
		for _, ids := range buckets {
			if len(ids) > 500 {
				ids = ids[:500] // a degenerate bucket; enough to link it
			}
			for x := 0; x < len(ids); x++ {
				for y := x + 1; y < len(ids); y++ {
					i, j := ids[x], ids[y]
					if find(i) == find(j) || checked[[2]int{i, j}] {
						continue
					}
					checked[[2]int{i, j}] = true
					if jaccard(us[i].shingle, us[j].shingle) >= Similar {
						parent[find(i)] = find(j)
					}
				}
			}
		}
	}
	idx := map[int]int{}
	var out [][]int
	for i := range us {
		r := find(i)
		k, ok := idx[r]
		if !ok {
			k = len(out)
			idx[r] = k
			out = append(out, nil)
		}
		out[k] = append(out[k], i)
	}
	return out
}

// medoid is the member most like the others: the clearest wording.
func medoid(us []unit, g []int) int {
	sample := g
	if len(sample) > 60 {
		sample = sample[:60]
	}
	best, bestScore := g[0], -1.0
	for _, i := range sample {
		sum := 0.0
		for _, j := range sample {
			if i != j {
				sum += jaccard(us[i].shingle, us[j].shingle)
			}
		}
		// Ties go to the shorter wording.
		if sum > bestScore+1e-9 || (sum > bestScore-1e-9 && len(us[i].text) < len(us[best].text)) {
			best, bestScore = i, sum
		}
	}
	return best
}

// stop words carry no meaning on their own; they don't count toward an
// instruction's content and never name a skill.
var stop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an the and or but if then so to of in on at by for with from into onto as is are was were be been being
		it its it's this that these those there here i i'm i've me my we our us you your you're he she they them their
		do does did done doing can could would should will shall may might must have has had get got make made let let's
		please pls plz just also again now still yet very really more most some any all every each no not don't dont
		ok okay yes yeah sure thanks thank hi hey like want wanted need needs i'd ensure up out about what which when how why
		than too one other same new way thing things`) {
		stop[w] = true
	}
}
