//go:build !windows

package magpiecli

import "os/exec"

func hide(cmd *exec.Cmd) {}
