// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package update

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// CheckEvery is how often tacet asks GitHub for a newer release.
const CheckEvery = 24 * time.Hour

// State is what the last check found, kept in update.json.
type State struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	Notified  string    `json:"notified,omitempty"` // version the session hook already announced
	AskedAt   time.Time `json:"asked_at,omitempty"` // when the person was last asked or reminded
}

// Dir is tacet's folder in the user cache. TACET_CACHE_DIR overrides it,
// as it does for the speech engine.
func Dir() string {
	if d := os.Getenv("TACET_CACHE_DIR"); d != "" {
		return d
	}
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "tacet")
}

func statePath() string { return filepath.Join(Dir(), "update.json") }
func lockPath() string  { return filepath.Join(Dir(), "update.lock") }

// Load reads the cached state; a missing or broken file is an empty one.
func Load() State {
	var s State
	if b, err := os.ReadFile(statePath()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// Save writes the state through a temporary file, so a reader never
// sees half of it.
func Save(s State) error {
	if err := os.MkdirAll(Dir(), 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(Dir(), "update-*.json")
	if err != nil {
		return err
	}
	_, err = tmp.Write(b)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), statePath())
}

// Due reports whether the last check is older than CheckEvery.
func (s State) Due(now time.Time) bool { return now.Sub(s.CheckedAt) >= CheckEvery }

// Available is the newer version the last check found, or "".
func (s State) Available(current string) string {
	if Newer(s.Latest, current) {
		return s.Latest
	}
	return ""
}

// ErrLocked means another tacet process is checking right now.
var ErrLocked = errors.New("another check is running")

// staleLock is when a lock left by a crashed process is ignored.
const staleLock = time.Minute

// lock takes the check lock. The returned func releases it.
func lock() (func(), error) {
	if err := os.MkdirAll(Dir(), 0o750); err != nil {
		return nil, err
	}
	p := lockPath()
	for range 2 {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- tacet's cache
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(p) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		fi, serr := os.Stat(p)
		if serr != nil || time.Since(fi.ModTime()) < staleLock {
			return nil, ErrLocked
		}
		_ = os.Remove(p)
	}
	return nil, ErrLocked
}

// Refresh asks GitHub for the latest release when the cached answer is
// a day old, under the lock and the 2-second timeout. A failed lookup
// still counts as a check, so an offline machine isn't asked again for
// a day.
func Refresh(ctx context.Context, client *http.Client, now time.Time) (State, error) {
	s := Load()
	if !s.Due(now) {
		return s, nil
	}
	unlock, err := lock()
	if err != nil {
		return s, err
	}
	defer unlock()
	s = Load() // another process may have finished while we waited
	if !s.Due(now) {
		return s, nil
	}
	ctx, cancel := context.WithTimeout(ctx, LookupTimeout)
	defer cancel()
	r, lerr := Latest(ctx, client, "")
	s.CheckedAt = now
	if lerr == nil {
		s.Latest, s.Notes = r.Version, r.Notes
	}
	if err := Save(s); err != nil {
		return s, err
	}
	return s, lerr
}
