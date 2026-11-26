package relay

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const gistFileName = "hub_config.json"

func newGistHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "udp", "1.1.1.1:53")
			},
		},
	}

	var tlsCfg *tls.Config
	if pemBytes, err := os.ReadFile("/data/data/com.termux/files/usr/etc/tls/cert.pem"); err == nil {
		pool := x509.NewCertPool()
		if pool.AppendCertsFromPEM(pemBytes) {
			tlsCfg = &tls.Config{RootCAs: pool}
		}
	}

	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext:     dialer.DialContext,
			TLSClientConfig: tlsCfg,
		},
	}
}

type hubConfigPayload struct {
	HubURL    string `json:"hub_url"`
	UpdatedAt string `json:"updated_at"`
}

// UpdateGistHubURL updates hub_config.json in the private GitHub Gist with the active tunnel URL.
func UpdateGistHubURL(ctx context.Context, token, gistID, hubURL string) error {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(gistID) == "" {
		return fmt.Errorf("GITHUB_GIST_TOKEN y GITHUB_GIST_ID son requeridos")
	}

	cfgBytes, err := json.Marshal(hubConfigPayload{
		HubURL:    hubURL,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}

	body, err := json.Marshal(map[string]any{
		"files": map[string]any{
			gistFileName: map[string]string{
				"content": string(cfgBytes),
			},
		},
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, "https://api.github.com/gists/"+gistID, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	client := newGistHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("github gist PATCH %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// FetchActiveHubURL reads hub_config.json from the private GitHub Gist and returns hub_url.
func FetchActiveHubURL(ctx context.Context, token, gistID string) (string, error) {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(gistID) == "" {
		return "", fmt.Errorf("GITHUB_GIST_TOKEN y GITHUB_GIST_ID son requeridos")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/gists/"+gistID, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := newGistHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("github gist GET %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var gistResp struct {
		Files map[string]struct {
			Content string `json:"content"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gistResp); err != nil {
		return "", err
	}

	file, ok := gistResp.Files[gistFileName]
	if !ok || strings.TrimSpace(file.Content) == "" {
		return "", fmt.Errorf("%s no encontrado en el Gist", gistFileName)
	}

	var cfg hubConfigPayload
	if err := json.Unmarshal([]byte(file.Content), &cfg); err != nil {
		return "", err
	}
	if strings.TrimSpace(cfg.HubURL) == "" {
		return "", fmt.Errorf("hub_url vacío en %s", gistFileName)
	}
	return strings.TrimSpace(cfg.HubURL), nil
}
