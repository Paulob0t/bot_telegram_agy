//go:build windows

package executor

import (
	"os/exec"
	"strconv"
)

func configureProcessGroup(cmd *exec.Cmd) {}

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
	_ = cmd.Process.Kill()
}
