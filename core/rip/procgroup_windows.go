//go:build windows

package rip

import "os/exec"

func killProcessGroup(*exec.Cmd) {}
