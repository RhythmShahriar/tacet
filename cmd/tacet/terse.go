// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/RhythmShahriar/tacet/audit"
	"github.com/RhythmShahriar/tacet/core"
	"github.com/RhythmShahriar/tacet/fix"
	"github.com/RhythmShahriar/tacet/internal/config"
	"github.com/RhythmShahriar/tacet/plugin"
)

// styleFeatures are features that select one of the plugin's output
// styles through outputStyle in settings.local.json.
var styleFeatures = map[string]string{
	"terse":    plugin.TerseStyle,
	"lesscode": plugin.LessCodeStyle,
}

// styleState keeps the outputStyle tacet replaced, so off restores it.
type styleState struct {
	Had      bool `json:"had"`
	Previous any  `json:"previous,omitempty"`
}

func styleStatePath(root string) string { return filepath.Join(config.Dir(root), "style.json") }

func ourStyle(v any) bool {
	for _, s := range styleFeatures {
		if v == s {
			return true
		}
	}
	return false
}

// setStyleFeature turns a style feature on or off. The settings, the
// saved previous style and the config change in one fix run, so tacet
// undo reverts all three.
func setStyleFeature(a *app, f string, on bool) error {
	root := a.root()
	c := config.Load(root)
	settings := audit.LocalSettings(root)
	cur := map[string]any{}
	if b, err := os.ReadFile(settings); err == nil { // #nosec G304 -- the project's own settings
		if err := json.Unmarshal(b, &cur); err != nil {
			return fmt.Errorf("%s: %w", settings, err)
		}
	}
	now, had := cur["outputStyle"]
	style := styleFeatures[f]
	if on == c.On(f) && (!on || now == style) {
		fmt.Fprintf(a.out, "%s is already %s for this project\n", f, map[bool]string{true: "on", false: "off"}[on])
		return nil
	}
	var edits []core.Edit
	statePath := styleStatePath(root)
	if on {
		for g := range styleFeatures {
			_ = c.Set(g, g == f) // one style at a time
		}
		edits = append(edits, fix.JSONEdit(settings, func(m map[string]any) { m["outputStyle"] = style }))
		if !ourStyle(now) {
			edits = append(edits, fix.JSONEdit(statePath, func(m map[string]any) {
				for k := range m {
					delete(m, k)
				}
				m["had"] = had
				if had {
					m["previous"] = now
				}
			}))
		}
	} else {
		_ = c.Set(f, false)
		if now == style {
			var st styleState
			if b, err := os.ReadFile(statePath); err == nil { // #nosec G304 -- tacet's own state
				_ = json.Unmarshal(b, &st)
			}
			edits = append(edits, fix.JSONEdit(settings, func(m map[string]any) {
				if st.Had {
					m["outputStyle"] = st.Previous
				} else {
					delete(m, "outputStyle")
				}
			}))
		}
		if now == style || !ourStyle(now) {
			edits = append(edits, core.Edit{Path: statePath, Change: func([]byte) ([]byte, error) { return nil, nil }})
		}
	}
	edits = append(edits, core.Edit{Path: config.Path(root), Change: func([]byte) ([]byte, error) { return c.Marshal() }})
	if _, err := fix.Apply(root, []*core.Fix{{ID: f + map[bool]string{true: "-on", false: "-off"}[on], Edits: edits}}); err != nil {
		return err
	}
	if !on {
		fmt.Fprintf(a.out, "%s is off for this project; outputStyle restored (tacet undo reverts it)\n", f)
		return nil
	}
	fmt.Fprintf(a.out, "%s is on for this project: outputStyle %s from the next message (tacet undo reverts it)\n", f, style)
	if _, err := os.Stat(plugin.StylePath(plugin.Dir(), f)); err != nil { // #nosec G703 -- tacet's own plugin dir
		fmt.Fprintln(a.out, "the tacet plugin isn't installed here, so Claude Code uses its default style until tacet init installs it")
	}
	return nil
}
