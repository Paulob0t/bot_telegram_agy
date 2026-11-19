//go:build !windows

package executor

import (
	"os/exec"
	"testing"
	"time"
)

func TestKillProcessTreeKillsDetachedGrandchildren(t *testing.T) {
	// Simula un padre que lanza un nieto con su propio setsid/pgid reteniendo stdout (como npm/pip)
	cmd := exec.Command("bash", "-c", "setsid sleep 30 & wait")
	configureProcessGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Dar 50ms para que arranque el subproceso nieto
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	killProcessTree(cmd)
	_ = stdout.Close()
	_ = cmd.Wait()
	elapsed := time.Since(start)

	if elapsed > 300*time.Millisecond {
		t.Fatalf("killProcessTree tardó demasiado (%v); un nieto retuvo el pipe", elapsed)
	}
}
