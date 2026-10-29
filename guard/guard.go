// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Package guard keeps secrets out of the agent's context: it recognizes
// secret files by name and shell commands that would print them.
package guard

import (
	"path/filepath"
	"regexp"
	"strings"
)

// safeSuffixes are template files that hold no real secrets.
var safeSuffixes = []string{".example", ".sample", ".template", ".dist", ".defaults"}

var secretNames = []*regexp.Regexp{
	regexp.MustCompile(`^\.env(\..+)?$`),
	regexp.MustCompile(`^\.envrc$`),
	regexp.MustCompile(`\.(pem|key|p12|pfx|jks|keystore|ppk|asc|gpg)$`),
	regexp.MustCompile(`^id_(rsa|dsa|ecdsa|ed25519)(\.pub)?$`),
	regexp.MustCompile(`^\.(netrc|pgpass|pypirc|npmrc|git-credentials)$`),
	regexp.MustCompile(`^(credentials|secrets?)(\.[a-z]+)?$`),
	regexp.MustCompile(`^service[-_]?account.*\.json$`),
	regexp.MustCompile(`^\.htpasswd$`),
	regexp.MustCompile(`\.tfstate(\.backup)?$`),
	regexp.MustCompile(`^auth\.json$`),
}

// secretDirs hold secrets except for the public files listed in public.
var secretDirs = []string{"/.ssh/", "/.aws/", "/.gnupg/"}

var public = map[string]bool{"known_hosts": true, "config": true, "authorized_keys": true}

// secretFiles are exact paths that hold credentials.
var secretFiles = []string{"/.docker/config.json", "/.kube/config"}

// IsSecretPath reports whether reading p would show a secret. A public
// key (.pub) isn't one.
func IsSecretPath(p string) bool {
	slash := "/" + strings.TrimPrefix(filepath.ToSlash(p), "/")
	base := strings.ToLower(filepath.Base(slash))
	if strings.HasSuffix(base, ".pub") {
		return false
	}
	for _, s := range safeSuffixes {
		if strings.HasSuffix(base, s) {
			return false
		}
	}
	for _, f := range secretFiles {
		if strings.HasSuffix(slash, f) {
			return true
		}
	}
	for _, d := range secretDirs {
		if strings.Contains(slash, d) && !public[base] {
			return true
		}
	}
	for _, re := range secretNames {
		if re.MatchString(base) {
			return true
		}
	}
	return false
}

// readers are commands that print or copy a file's contents.
var readers = map[string]bool{
	"cat": true, "less": true, "more": true, "head": true, "tail": true, "bat": true, "batcat": true,
	"grep": true, "rg": true, "ag": true, "sed": true, "awk": true, "xxd": true, "od": true, "hexdump": true,
	"strings": true, "base64": true, "cp": true, "scp": true, "rsync": true, "nl": true, "tac": true,
	"sort": true, "uniq": true, "cut": true, "jq": true, "yq": true, "source": true, ".": true, "openssl": true,
	"curl": true, "wget": true, "nc": true, "type": true, "Get-Content": true, "gc": true,
}

var splitRe = regexp.MustCompile(`\|\||&&|[;|&\n()]`)

// BashReadsSecret reports the secret file a command would print, if any.
func BashReadsSecret(cmd string) (string, bool) {
	for _, seg := range splitRe.Split(cmd, -1) {
		words := fields(seg)
		for len(words) > 0 && (strings.Contains(words[0], "=") || words[0] == "sudo" || words[0] == "env" || words[0] == "command") {
			words = words[1:]
		}
		if len(words) == 0 || !readers[filepath.Base(words[0])] {
			continue
		}
		args := words[1:]
		if c := filepath.Base(words[0]); c == "cp" || c == "scp" || c == "rsync" {
			// The last argument is where the copy goes, not a read.
			if len(args) > 0 {
				args = args[:len(args)-1]
			}
		}
		for _, w := range args {
			w = strings.TrimLeft(w, "<@")
			if strings.HasPrefix(w, "-") && !strings.Contains(w, "=") {
				continue
			}
			if _, v, ok := strings.Cut(w, "="); ok && strings.HasPrefix(w, "-") {
				w = v
			}
			if IsSecretPath(w) {
				return w, true
			}
		}
	}
	return "", false
}

// fields splits on spaces, keeping quoted words whole and dropping the quotes.
func fields(s string) []string {
	var out []string
	var cur strings.Builder
	var q rune
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case q != 0 && r == q:
			q = 0
		case q == 0 && (r == '\'' || r == '"'):
			q = r
		case q == 0 && (r == ' ' || r == '\t' || r == '>' || r == '<'):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}
