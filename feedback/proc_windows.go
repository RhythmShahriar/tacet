// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

//go:build windows

package feedback

import "os/exec"

// killGroup keeps the default: the context kills the process and
// WaitDelay closes its pipes.
func killGroup(*exec.Cmd) {}
