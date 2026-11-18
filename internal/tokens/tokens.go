package tokens

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"telegram-paulobot/internal/config"
)

type ClaudeQuota struct {
	Available           bool    `json:"available"`
	SessionUsedPct      float64 `json:"session_used_pct"`
	SessionRemainingPct float64 `json:"session_remaining_pct"`
	SessionResets       string  `json:"session_resets"`
	WeekUsedPct         float64 `json:"week_used_pct"`
	WeekRemainingPct    float64 `json:"week_remaining_pct"`
	WeekResets          string  `json:"week_resets"`
}

type AgyQuotaBucket struct {
	RemainingPct float64 `json:"five_hour_remaining_pct,omitempty"`
	FiveHReset   string  `json:"five_hour_reset,omitempty"`
	WeekPct      float64 `json:"week_remaining_pct,omitempty"`
	WeekReset    string  `json:"week_reset,omitempty"`
}

type ExtraQuota struct {
	Label        string  `json:"label"`
	RemainingPct float64 `json:"remaining_pct"`
	Reset        string  `json:"reset"`
}

type Quotas struct {
	Timestamp   string                    `json:"timestamp"`
	ClaudeCode  ClaudeQuota               `json:"claude_code"`
	Antigravity map[string]AgyQuotaBucket `json:"antigravity"`
	Extra       []ExtraQuota              `json:"extra,omitempty"`
}

var (
	pctResetRe   = regexp.MustCompile(`(\d+(?:\.\d+)?)%\s+(.*)$`)
	claudeSessRe = regexp.MustCompile(`Current session:\s*(\d+(?:\.\d+)?)%\s*used(?:\s*·\s*resets\s*(.*))?`)
	claudeWeekRe = regexp.MustCompile(`Current week[^:]*:\s*(\d+(?:\.\d+)?)%\s*used(?:\s*·\s*resets\s*(.*))?`)
	parenEndRe   = regexp.MustCompile(`\s*\(.*\)\s*$`)
)

func FetchClaudeQuota(cfg *config.Config) ClaudeQuota {
	cmd := exec.Command(cfg.ClaudeBin, "--no-session-persistence", "-p", "/usage")
	cmd.Dir = os.TempDir()
	out, err := cmd.Output()
	if err != nil {
		return ClaudeQuota{}
	}
	q := ClaudeQuota{Available: true}
	for _, rawLine := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(rawLine)
		if m := claudeSessRe.FindStringSubmatch(line); len(m) > 1 {
			val, _ := strconv.ParseFloat(m[1], 64)
			q.SessionUsedPct = val
			q.SessionRemainingPct = maxFloat(0, 100-val)
			if len(m) > 2 {
				q.SessionResets = strings.TrimSpace(m[2])
			}
		} else if strings.Contains(line, "Current week (all models):") || (strings.Contains(line, "Current week") && !strings.Contains(line, "Fable") && q.WeekUsedPct == 0) {
			if m := claudeWeekRe.FindStringSubmatch(line); len(m) > 1 {
				val, _ := strconv.ParseFloat(m[1], 64)
				q.WeekUsedPct = val
				q.WeekRemainingPct = maxFloat(0, 100-val)
				if len(m) > 2 {
					q.WeekResets = strings.TrimSpace(m[2])
				}
			}
		}
	}
	return q
}

func FetchAgyQuota(cfg *config.Config) (map[string]AgyQuotaBucket, []ExtraQuota) {
	buckets := map[string]AgyQuotaBucket{
		"gemini":     {RemainingPct: 100, WeekPct: 100},
		"claude_gpt": {RemainingPct: 100, WeekPct: 100},
		"argon":      {WeekPct: 100},
	}
	var extras []ExtraQuota

	cachePath := filepath.Join(cfg.BaseCLIDir, "ai_quotas_cache.json")
	if raw, err := os.ReadFile(cachePath); err == nil {
		var prev Quotas
		if json.Unmarshal(raw, &prev) == nil && len(prev.Antigravity) > 0 {
			for k, v := range prev.Antigravity {
				buckets[k] = v
			}
			extras = prev.Extra
		}
	}

	cmd := exec.Command(cfg.AgyBin, "-p", "/usage")
	cmd.Dir = os.TempDir()
	out, err := cmd.Output()
	if err != nil {
		return buckets, extras
	}

	g := buckets["gemini"]
	c := buckets["claude_gpt"]
	a := buckets["argon"]
	var freshExtras []ExtraQuota

	for _, rawLine := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "Quota:") {
			continue
		}
		loc := pctResetRe.FindStringSubmatchIndex(line)
		if len(loc) < 6 {
			continue
		}
		pctStr := line[loc[2]:loc[3]]
		resetStr := strings.TrimSpace(line[loc[4]:loc[5]])
		pct, err := strconv.ParseFloat(pctStr, 64)
		if err != nil {
			continue
		}

		switch {
		case strings.Contains(line, "Gemini Models") && strings.Contains(line, "Five Hour Limit"):
			g.RemainingPct = pct
			g.FiveHReset = resetStr
		case strings.Contains(line, "Gemini Models") && strings.Contains(line, "Weekly Limit"):
			g.WeekPct = pct
			g.WeekReset = resetStr
		case strings.Contains(line, "Claude and GPT") && strings.Contains(line, "Five Hour Limit"):
			c.RemainingPct = pct
			c.FiveHReset = resetStr
		case strings.Contains(line, "Claude and GPT") && strings.Contains(line, "Weekly Limit"):
			c.WeekPct = pct
			c.WeekReset = resetStr
		case strings.Contains(line, "Gemini 4 Argon"):
			a.WeekPct = pct
			a.WeekReset = resetStr
		default:
			lbl := strings.TrimSpace(line[:loc[0]])
			if lbl != "" {
				freshExtras = append(freshExtras, ExtraQuota{
					Label:        lbl,
					RemainingPct: pct,
					Reset:        resetStr,
				})
			}
		}
	}
	buckets["gemini"] = g
	buckets["claude_gpt"] = c
	buckets["argon"] = a
	return buckets, freshExtras
}

func FetchAll(cfg *config.Config) Quotas {
	var q Quotas
	q.Timestamp = time.Now().UTC().Format(time.RFC3339)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		q.ClaudeCode = FetchClaudeQuota(cfg)
	}()
	go func() {
		defer wg.Done()
		q.Antigravity, q.Extra = FetchAgyQuota(cfg)
	}()
	wg.Wait()

	cachePath := filepath.Join(cfg.BaseCLIDir, "ai_quotas_cache.json")
	if raw, err := json.Marshal(q); err == nil {
		_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
		_ = os.WriteFile(cachePath, raw, 0o644)
	}
	return q
}

func LoadCachedOrFetch(cfg *config.Config, maxAge time.Duration) Quotas {
	cachePath := filepath.Join(cfg.BaseCLIDir, "ai_quotas_cache.json")
	if maxAge > 0 {
		if info, err := os.Stat(cachePath); err == nil && time.Since(info.ModTime()) < maxAge {
			if raw, err := os.ReadFile(cachePath); err == nil {
				var q Quotas
				if json.Unmarshal(raw, &q) == nil && len(q.Antigravity) > 0 {
					return q
				}
			}
		}
	}
	return FetchAll(cfg)
}

func WriteWaybarJSON(cfg *config.Config) error {
	q := FetchAll(cfg)
	g := q.Antigravity["gemini"]
	c := q.Antigravity["claude_gpt"]
	a := q.Antigravity["argon"]

	tooltip := fmt.Sprintf(
		"<b>󰧑 Uso de IA (Click para abrir panel)</b>\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"+
			"<b>Claude Code (%% Usado):</b>\n"+
			"  • Sesión 5h: %.0f%% usado\n"+
			"  • Semanal:   %.0f%% usado\n"+
			"<b>Antigravity (%% Libre):</b>\n"+
			"  • Gemini 5h:       %.0f%%\n"+
			"  • Gemini Semanal:  %.0f%%\n"+
			"  • Claude/GPT 5h:   %.0f%%\n"+
			"  • Claude/GPT Sem:  %.0f%%\n"+
			"  • Gemini 4 Argon:  %.0f%%",
		q.ClaudeCode.SessionUsedPct,
		q.ClaudeCode.WeekUsedPct,
		g.RemainingPct, g.WeekPct,
		c.RemainingPct, c.WeekPct,
		a.WeekPct,
	)

	payload := map[string]string{
		"text":    "󰧑",
		"tooltip": tooltip,
		"class":   "tokens",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	outPath := filepath.Join(cfg.BaseCLIDir, "waybar_tokens.json")
	tmp := outPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, outPath)
}

func ShowRofiDialog(cfg *config.Config, useCache bool) {
	var q Quotas
	if useCache {
		q = LoadCachedOrFetch(cfg, 10*time.Minute)
	} else {
		q = FetchAll(cfg)
		_ = WriteWaybarJSON(cfg)
	}

	g := q.Antigravity["gemini"]
	c := q.Antigravity["claude_gpt"]
	a := q.Antigravity["argon"]

	lines := []string{
		"󰑐  [Click aquí para Sincronizar en Vivo ahora]",
		"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━",
		"🟠 CLAUDE CODE (Suscripción Anthropic · % Usado)",
		fmt.Sprintf("   Sesión · 5 h       [%s]  %5.0f%% usado   · Reinicia: %s", makeBar(q.ClaudeCode.SessionUsedPct, 12), q.ClaudeCode.SessionUsedPct, fmtClaudeReset(q.ClaudeCode.SessionResets)),
		fmt.Sprintf("   Semanal (Todos)    [%s]  %5.0f%% usado   · Reinicia: %s", makeBar(q.ClaudeCode.WeekUsedPct, 12), q.ClaudeCode.WeekUsedPct, fmtClaudeReset(q.ClaudeCode.WeekResets)),
		"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━",
		"🔵 ANTIGRAVITY (Google / Modelos · % Libre)",
		fmt.Sprintf("   Gemini · 5 h       [%s]  %5.1f%% libre   · Reinicia: %s", makeBar(g.RemainingPct, 12), g.RemainingPct, fmtISOReset(g.FiveHReset)),
		fmt.Sprintf("   Gemini · Semanal   [%s]  %5.1f%% libre   · Reinicia: %s", makeBar(g.WeekPct, 12), g.WeekPct, fmtISOReset(g.WeekReset)),
		fmt.Sprintf("   Claude & GPT · 5h  [%s]  %5.1f%% libre   · Reinicia: %s", makeBar(c.RemainingPct, 12), c.RemainingPct, fmtISOReset(c.FiveHReset)),
		fmt.Sprintf("   Claude & GPT · Sem [%s]  %5.1f%% libre   · Reinicia: %s", makeBar(c.WeekPct, 12), c.WeekPct, fmtISOReset(c.WeekReset)),
		fmt.Sprintf("   Gemini 4 Argon·Sem [%s]  %5.1f%% libre   · Reinicia: %s", makeBar(a.WeekPct, 12), a.WeekPct, fmtISOReset(a.WeekReset)),
	}

	for _, ex := range q.Extra {
		lbl := ex.Label
		if len([]rune(lbl)) > 18 {
			lbl = string([]rune(lbl)[:18])
		}
		lines = append(lines, fmt.Sprintf("   %-18s [%s]  %5.1f%% libre   · Reinicia: %s", lbl, makeBar(ex.RemainingPct, 12), ex.RemainingPct, fmtISOReset(ex.Reset)))
	}

	cmd := exec.Command(
		"rofi", "-dmenu", "-i", "-p", "󰧑 Uso de IA:",
		"-theme-str", "window {width: 760px;} inputbar {enabled: false;} listview {lines: 13;}",
	)
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n"))
	out, _ := cmd.Output()
	if strings.Contains(string(out), "Sincronizar en Vivo") {
		_ = exec.Command("notify-send", "Uso de IA", "Sincronizando cuotas en vivo...").Run()
		ShowRofiDialog(cfg, false)
	}
}

func makeBar(pct float64, width int) string {
	filled := int((pct/100.0)*float64(width) + 0.5)
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func fmtISOReset(isoStr string) string {
	if isoStr == "" {
		return "N/A"
	}
	t, err := time.Parse(time.RFC3339, isoStr)
	if err != nil {
		return isoStr
	}
	loc := t.Local()
	dias := []string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}
	return fmt.Sprintf("%s %s", dias[loc.Weekday()], loc.Format("03:04 PM"))
}

func fmtClaudeReset(s string) string {
	if strings.TrimSpace(s) == "" {
		return "Sin uso activo"
	}
	return parenEndRe.ReplaceAllString(s, "")
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
