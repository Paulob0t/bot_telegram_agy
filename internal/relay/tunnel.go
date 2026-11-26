package relay

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

var tryCloudflareRegex = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)

// findExecutable avoids os/exec.LookPath (which uses faccessat2, blocked by Android seccomp).
func findExecutable(name string) string {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return name
}

// RunCloudflaredLoop starts cloudflared with HTTP/2, captures the trycloudflare URL from stderr,
// publishes it to the GitHub Gist, and restarts the tunnel if it exits.
func RunCloudflaredLoop(ctx context.Context, port int, gistToken, gistID string) {
	target := fmt.Sprintf("http://localhost:%d", port)
	bin := findExecutable("cloudflared")
	for {
		if ctx.Err() != nil {
			return
		}

		cmd := exec.CommandContext(ctx, bin, "tunnel", "--protocol", "http2", "--url", target)
		stderr, err := cmd.StderrPipe()
		if err != nil {
			log.Printf("relay hub: error creando pipe stderr de cloudflared: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}

		if err := cmd.Start(); err != nil {
			log.Printf("relay hub: error iniciando cloudflared: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}

		published := false
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if !published {
				if match := tryCloudflareRegex.FindString(line); match != "" {
					published = true
					log.Printf("relay hub: túnel activo en %s", match)
					if err := UpdateGistHubURL(ctx, gistToken, gistID, match); err != nil {
						log.Printf("relay hub: error actualizando Gist: %v", err)
					} else {
						log.Printf("relay hub: Gist actualizado con %s", match)
					}
				}
			}
		}

		err = cmd.Wait()
		if ctx.Err() != nil {
			return
		}
		log.Printf("relay hub: cloudflared terminó (%v), reiniciando en 3s...", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}
