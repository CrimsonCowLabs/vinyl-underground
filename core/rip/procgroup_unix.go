//go:build !windows

package rip

import (
	"os/exec"
	"syscall"
)

// killProcessGroup makes cancelling cmd kill everything it started: the standalone yt-dlp is a
// launcher that runs the real program, and ffmpeg runs under that.
func killProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
