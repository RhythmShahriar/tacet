// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

//go:build !windows

package feedback

import (
	"os/exec"
	"syscall"
)

// killGroup runs cmd in its own process group and kills the whole group
// on timeout, so a compiler a checker started doesn't outlive it.
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
