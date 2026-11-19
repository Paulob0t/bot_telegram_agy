//go:build !windows

package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// collectDescendants recorre /proc/<pid>/task/*/children recursivamente para encontrar
// todos los hijos/nietos aunque hayan creado su propio Process Group (ej. npm, pip, bun).
func collectDescendants(rootPid int) []int {
	visited := make(map[int]bool)
	var order []int

	var walk func(pid int)
	walk = func(pid int) {
		if pid <= 1 || visited[pid] {
			return
		}
		visited[pid] = true
		pattern := filepath.Join("/proc", strconv.Itoa(pid), "task", "*", "children")
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			raw, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			for _, field := range strings.Fields(string(raw)) {
				if childPid, err := strconv.Atoi(field); err == nil && childPid > 1 {
					walk(childPid)
				}
			}
		}
		order = append(order, pid)
	}

	walk(rootPid)
	return order
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	rootPid := cmd.Process.Pid

	// 1. Recolectar todo el árbol de descendientes desde /proc antes de matar al padre
	descendants := collectDescendants(rootPid)

	// 2. Matar cada descendiente y su propio process group (de hojas a raíz)
	for _, pid := range descendants {
		if pgid, err := syscall.Getpgid(pid); err == nil && pgid > 1 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}

	// 3. Asegurar kill del grupo raíz y proceso padre
	if pgid, err := syscall.Getpgid(rootPid); err == nil && pgid > 1 {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
	_ = cmd.Process.Kill()
}
