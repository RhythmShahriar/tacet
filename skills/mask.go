// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package skills

import (
	"os"
	"regexp"
	"strings"
)

// Masked replaces anything that looks like a secret.
const Masked = "[masked]"

// Token shapes with a known prefix, then generic ones.
var secretRes = []*regexp.Regexp{
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(-----END [A-Z ]*PRIVATE KEY-----|$)`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{16,}`),
	regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{20,}`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9\-]{10,}`),
	regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}`),
	regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`),
	regexp.MustCompile(`\b(sk|pk|rk)_(live|test)_[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`),
	regexp.MustCompile(`\b[a-z][a-z0-9+.\-]*://[^\s:/@]+:[^\s@/]+@`), // credentials in a URL
}

// key=value and key: value where the key names a secret.
var assignRe = regexp.MustCompile(`(?i)\b([a-z0-9_\-]*(?:password|passwd|pwd|secret|token|api[_\-]?key|access[_\-]?key|private[_\-]?key|auth))(\s*[:=]\s*)("[^"]*"|'[^']*'|\S+)`)

// A long run of letters and digits that mixes cases and digits, or a
// long hex string: the shape of a key with no known prefix.
var blobRe = regexp.MustCompile(`[A-Za-z0-9_\-+=]{32,}`)

var homeRes = []*regexp.Regexp{
	regexp.MustCompile(`/home/[A-Za-z0-9_.\-]+`),
	regexp.MustCompile(`/Users/[A-Za-z0-9_.\-]+`),
	regexp.MustCompile(`(?i)[A-Z]:\\Users\\[A-Za-z0-9_.\-]+`),
}

// Mask hides secrets in s and writes absolute home paths as ~.
func Mask(s string) string {
	for _, re := range secretRes {
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			if strings.Contains(m, "://") { // keep the scheme, drop the user:pass
				return m[:strings.Index(m, "://")+3] + Masked + "@"
			}
			return Masked
		})
	}
	s = assignRe.ReplaceAllStringFunc(s, func(m string) string {
		p := assignRe.FindStringSubmatch(m)
		if p[3] == Masked || len(strings.Trim(p[3], `"'`)) < 6 {
			return m // a short word after "token:" is prose, not a token
		}
		return p[1] + p[2] + Masked
	})
	s = blobRe.ReplaceAllStringFunc(s, func(m string) string {
		if looksLikeKey(m) {
			return Masked
		}
		return m
	})
	if h, err := os.UserHomeDir(); err == nil && len(h) > 1 {
		s = strings.ReplaceAll(s, h, "~")
	}
	for _, re := range homeRes {
		s = re.ReplaceAllString(s, "~")
	}
	return s
}

func looksLikeKey(m string) bool {
	var upper, lower, digit bool
	hex := true
	for _, r := range m {
		switch {
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= '0' && r <= '9':
			digit = true
		}
		if !strings.ContainsRune("0123456789abcdef", r) {
			hex = false
		}
	}
	return (upper && lower && digit) || (hex && digit)
}
