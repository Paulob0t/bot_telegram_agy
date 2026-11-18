package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func CaptureScreen() (string, error) {
	dir := filepath.Join(os.TempDir(), "telegram-paulobot-screens")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	outPath := filepath.Join(dir, fmt.Sprintf("screen-%d.png", time.Now().Unix()))
	cmd := exec.Command("grim", outPath)
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return outPath, nil
}

func Notify(appName, wsName string) {
	title := "Tarea lista"
	if wsName != "" {
		title = fmt.Sprintf("Tarea lista · %s", wsName)
	}
	if runtime.GOOS == "windows" {
		safeApp := strings.ReplaceAll(appName, "'", "''")
		safeTitle := strings.ReplaceAll("✓ "+title, "'", "''")
		psCmd := fmt.Sprintf(
			"Add-Type -AssemblyName System.Windows.Forms,System.Drawing; "+
				"$n = New-Object System.Windows.Forms.NotifyIcon; "+
				"$n.Icon = [System.Drawing.SystemIcons]::Information; "+
				"$n.Visible = $true; "+
				"$n.ShowBalloonTip(4000, '%s', '%s', [System.Windows.Forms.ToolTipIcon]::Info); "+
				"Start-Sleep -Milliseconds 4200; $n.Dispose()",
			safeApp, safeTitle,
		)
		_ = exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", psCmd).Start()
		return
	}

	env := os.Environ()
	hasDbus := false
	for _, e := range env {
		if strings.HasPrefix(e, "DBUS_SESSION_BUS_ADDRESS=") {
			hasDbus = true
			break
		}
	}
	if !hasDbus {
		env = append(env, fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", os.Getuid()))
	}
	if p, err := exec.LookPath("omarchy-notification-send"); err == nil {
		cmd := exec.Command(p, "--app-name", appName, "-g", "✓", "-u", "normal", "-t", "4000", title)
		cmd.Env = env
		_ = cmd.Run()
		return
	}
	cmd := exec.Command("notify-send", "-a", appName, "-u", "normal", "✓ "+title)
	cmd.Env = env
	_ = cmd.Run()
}
