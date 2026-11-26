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
	pctResetRe      = regexp.MustCompile(`(\d+(?:\.\d+)?)%\s+(.*)$`)
	claudeSessRe    = regexp.MustCompile(`Current session:\s*(\d+(?:\.\d+)?)%\s*used(?:\s*·\s*resets\s*(.*))?`)
	claudeWeekRe    = regexp.MustCompile(`Current week[^:]*:\s*(\d+(?:\.\d+)?)%\s*used(?:\s*·\s*resets\s*(.*))?`)
	parenEndRe      = regexp.MustCompile(`\s*\(.*\)\s*$`)
	claudeDateTimeR = regexp.MustCompile(`(?i)^(?:([a-z]{3})\s+(\d{1,2}),?\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)$`)
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

func FormatTelegramDashboard(q Quotas) string {
	g := q.Antigravity["gemini"]
	c := q.Antigravity["claude_gpt"]

	updatedStr := time.Now().Format("03:04 PM")
	if t, err := time.Parse(time.RFC3339, q.Timestamp); err == nil {
		updatedStr = t.Local().Format("03:04 PM")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("◈ **Cuotas de IA** *(Act: `%s`)*\n\n", updatedStr))
	sb.WriteString("🟠 **Claude Code (% Usado):**\n")
	sb.WriteString(fmt.Sprintf("• **5h:** `%.0f%%` usado · ↻ *%s*\n", q.ClaudeCode.SessionUsedPct, fmtClaudeReset(q.ClaudeCode.SessionResets)))
	sb.WriteString(fmt.Sprintf("• **Sem:** `%.0f%%` usado · ↻ *%s*\n\n", q.ClaudeCode.WeekUsedPct, fmtClaudeReset(q.ClaudeCode.WeekResets)))

	sb.WriteString("🔵 **Antigravity (% Libre):**\n")
	sb.WriteString(fmt.Sprintf("• **Gemini 5h:** `%.0f%%` libre · ↻ *%s*\n", g.RemainingPct, fmtISOReset(g.FiveHReset)))
	sb.WriteString(fmt.Sprintf("• **Gemini Sem:** `%.0f%%` libre · ↻ *%s*\n", g.WeekPct, fmtISOReset(g.WeekReset)))
	sb.WriteString(fmt.Sprintf("• **Claude/GPT 5h:** `%.0f%%` libre · ↻ *%s*\n", c.RemainingPct, fmtISOReset(c.FiveHReset)))
	sb.WriteString(fmt.Sprintf("• **Claude/GPT Sem:** `%.0f%%` libre · ↻ *%s*", c.WeekPct, fmtISOReset(c.WeekReset)))

	for _, ex := range q.Extra {
		sb.WriteString(fmt.Sprintf("\n• **%s:** `%.0f%%` libre · ↻ *%s*", ex.Label, ex.RemainingPct, fmtISOReset(ex.Reset)))
	}
	return sb.String()
}

func WriteWaybarJSON(cfg *config.Config) error {
	q := FetchAll(cfg)
	g := q.Antigravity["gemini"]
	c := q.Antigravity["claude_gpt"]

	updatedStr := time.Now().Format("03:04 PM")
	if t, err := time.Parse(time.RFC3339, q.Timestamp); err == nil {
		updatedStr = t.Local().Format("03:04 PM")
	}

	tooltip := fmt.Sprintf(
		"<b>󰧑 Uso de IA (Act: %s · Click para abrir panel)</b>\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"+
			"<b>🟠 Claude Code (%% Usado):</b>\n"+
			"  • Sesión 5h:      %3.0f%% usado  · ↻ %s\n"+
			"  • Semanal:        %3.0f%% usado  · ↻ %s\n"+
			"<b>🔵 Antigravity (%% Libre):</b>\n"+
			"  • Gemini 5h:      %3.0f%% libre  · ↻ %s\n"+
			"  • Gemini Sem:     %3.0f%% libre  · ↻ %s\n"+
			"  • Claude/GPT 5h:  %3.0f%% libre  · ↻ %s\n"+
			"  • Claude/GPT Sem: %3.0f%% libre  · ↻ %s",
		updatedStr,
		q.ClaudeCode.SessionUsedPct, fmtClaudeReset(q.ClaudeCode.SessionResets),
		q.ClaudeCode.WeekUsedPct, fmtClaudeReset(q.ClaudeCode.WeekResets),
		g.RemainingPct, fmtISOReset(g.FiveHReset),
		g.WeekPct, fmtISOReset(g.WeekReset),
		c.RemainingPct, fmtISOReset(c.FiveHReset),
		c.WeekPct, fmtISOReset(c.WeekReset),
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

	updatedStr := time.Now().Format("03:04 PM")
	if t, err := time.Parse(time.RFC3339, q.Timestamp); err == nil {
		updatedStr = t.Local().Format("03:04 PM")
	}

	lines := []string{
		fmt.Sprintf("󰑐  [Click aquí para Sincronizar en Vivo ahora]  (Última act: %s)", updatedStr),
		"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━",
		"🟠 CLAUDE CODE (Suscripción Anthropic · % Usado)",
		fmt.Sprintf("   Sesión · 5 h       [%s]  %5.0f%% usado   · Reinicia: %s", makeBar(q.ClaudeCode.SessionUsedPct, 12), q.ClaudeCode.SessionUsedPct, fmtClaudeReset(q.ClaudeCode.SessionResets)),
		fmt.Sprintf("   Semanal (Todos)    [%s]  %5.0f%% usado   · Reinicia: %s", makeBar(q.ClaudeCode.WeekUsedPct, 12), q.ClaudeCode.WeekUsedPct, fmtClaudeReset(q.ClaudeCode.WeekResets)),
		"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━",
		"🔵 ANTIGRAVITY (Google / Modelos · % Libre)",
		fmt.Sprintf("   Gemini · 5 h       [%s]  %5.1f%% libre   · Reinicia: %s", makeBar(g.RemainingPct, 12), g.RemainingPct, fmtISOReset(g.FiveHReset)),
		fmt.Sprintf("   Gemini · Semanal   [%s]  %5.1f%% libre   · Reinicia: %s", makeBar(g.WeekPct, 12), g.WeekPct, fmtISOReset(g.WeekReset)),
		fmt.Sprintf("   Claude & GPT · 5h  [%s]  %5.1f%% libre   · Reinicia: %s", makeBar(c.RemainingPct, 12), c.RemainingPct, fmtISOReset(c.FiveHReset)),
		fmt.Sprintf("   Claude & GPT · Sem [%s]  %5.1f%% libre   · Reinicia: %s", makeBar(c.WeekPct, 12), c.WeekPct, fmtISOReset(c.WeekReset)),
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
		"-theme-str", "window {width: 860px;} inputbar {enabled: false;} listview {lines: 13;}",
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

func formatResetTime(target time.Time) string {
	loc := target.Local()
	now := time.Now()

	diff := loc.Sub(now)
	var countdown string
	if diff <= 0 {
		countdown = "ya disponible"
	} else {
		totalMins := int(diff.Minutes())
		days := totalMins / (60 * 24)
		hours := (totalMins % (60 * 24)) / 60
		mins := totalMins % 60
		switch {
		case days > 0:
			countdown = fmt.Sprintf("en %dd %dh", days, hours)
		case hours > 0:
			countdown = fmt.Sprintf("en %dh %dm", hours, mins)
		default:
			countdown = fmt.Sprintf("en %dm", maxInt(1, mins))
		}
	}

	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	targetStart := time.Date(loc.Year(), loc.Month(), loc.Day(), 0, 0, 0, 0, now.Location())
	dayDiff := int(targetStart.Sub(todayStart).Hours() / 24)

	timePart := loc.Format("03:04 PM")
	dias := []string{"Dom", "Lun", "Mar", "Mié", "Jue", "Vie", "Sáb"}
	meses := []string{"", "Ene", "Feb", "Mar", "Abr", "May", "Jun", "Jul", "Ago", "Sep", "Oct", "Nov", "Dic"}

	switch dayDiff {
	case 0:
		return fmt.Sprintf("Hoy %s (%s)", timePart, countdown)
	case 1:
		return fmt.Sprintf("Mañana %s (%s)", timePart, countdown)
	default:
		return fmt.Sprintf("%s %d %s, %s (%s)", dias[loc.Weekday()], loc.Day(), meses[int(loc.Month())], timePart, countdown)
	}
}

func fmtISOReset(isoStr string) string {
	if strings.TrimSpace(isoStr) == "" {
		return "Sin límite activo"
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(isoStr))
	if err != nil {
		return isoStr
	}
	return formatResetTime(t)
}

func fmtClaudeReset(s string) string {
	clean := strings.TrimSpace(parenEndRe.ReplaceAllString(s, ""))
	if clean == "" {
		return "Sin uso activo"
	}
	m := claudeDateTimeR.FindStringSubmatch(clean)
	if len(m) == 0 {
		return clean
	}

	now := time.Now()
	monthMap := map[string]time.Month{
		"jan": time.January, "feb": time.February, "mar": time.March, "apr": time.April,
		"may": time.May, "jun": time.June, "jul": time.July, "aug": time.August,
		"sep": time.September, "oct": time.October, "nov": time.November, "dec": time.December,
	}

	month := now.Month()
	day := now.Day()
	hasExplicitDate := m[1] != "" && m[2] != ""
	if hasExplicitDate {
		if parsedM, ok := monthMap[strings.ToLower(m[1])]; ok {
			month = parsedM
		}
		if d, err := strconv.Atoi(m[2]); err == nil {
			day = d
		}
	}

	hour, _ := strconv.Atoi(m[3])
	minute := 0
	if m[4] != "" {
		minute, _ = strconv.Atoi(m[4])
	}
	ampm := strings.ToLower(m[5])
	if ampm == "pm" && hour < 12 {
		hour += 12
	} else if ampm == "am" && hour == 12 {
		hour = 0
	}

	target := time.Date(now.Year(), month, day, hour, minute, 0, 0, now.Location())
	if !hasExplicitDate && target.Before(now) {
		target = target.Add(24 * time.Hour)
	} else if hasExplicitDate && target.Before(now.Add(-48*time.Hour)) {
		target = target.AddDate(1, 0, 0)
	}
	return formatResetTime(target)
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
