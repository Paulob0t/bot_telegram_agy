package commands

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"agente-bot/internal/chats"
	"agente-bot/internal/config"
	"agente-bot/internal/models"
	"agente-bot/internal/projects"
	"agente-bot/internal/state"
)

// maxResumeChats caps the resume list sent to the phone.
const maxResumeChats = 30

// NewChatID is the resume entry that starts a clean chat.
const NewChatID = "NEW"

type projectItem struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type chatItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// BuildOptions is the data behind the app's model, project and resume pickers.
func BuildOptions(cfg *config.Config, st *state.BotSessionState) map[string]any {
	var projList []projectItem
	for _, p := range projects.ListAvailableProjects(cfg) {
		projList = append(projList, projectItem{Name: p.Name, Path: p.Path})
	}

	chatList := []chatItem{{ID: NewChatID, Title: "➕ Nuevo chat limpio en este proyecto"}}
	if st.CurrentEngine == "claude" {
		chatList = append(chatList, claudeChats(cfg, st.CurrentCWD)...)
	} else {
		chatList = append(chatList, agyChats(cfg, st.CurrentCWD)...)
	}

	_, activeModID, _ := models.GetEffectiveModelInfo(cfg, st)
	return map[string]any{
		"ok":           true,
		"engine":       st.CurrentEngine,
		"active_model": activeModID,
		"active_cwd":   st.CurrentCWD,
		"models":       models.GetCatalog(cfg),
		"projects":     projList,
		"chats":        chatList,
	}
}

// claudeChats filters the full history before capping: a global top-N cut
// would leave most projects empty.
func claudeChats(cfg *config.Config, cwd string) []chatItem {
	var out []chatItem
	for _, cs := range chats.ListClaudeSessions(cfg, 0) {
		if cwd != "" && filepath.Clean(cs.CWD) != filepath.Clean(cwd) {
			continue
		}
		out = append(out, chatItem{ID: cs.SessionID, Title: cs.Title})
		if len(out) >= maxResumeChats {
			break
		}
	}
	return out
}

func agyChats(cfg *config.Config, cwd string) []chatItem {
	var out []chatItem
	for _, c := range chats.ListAgyConversations(cfg, 0) {
		if cwd != "" && !agyWorkspaceIs(c.WorkspaceURIs, cwd) {
			continue
		}
		if !chats.AgyConversationExists(cfg, c.ConversationID) {
			continue
		}
		out = append(out, chatItem{ID: c.ConversationID, Title: c.Title})
		if len(out) >= maxResumeChats {
			break
		}
	}
	return out
}

// agyWorkspaceIs matches the exact folder in AGY's `["file:///path"]` list; a substring
// test would make the home folder match every chat.
func agyWorkspaceIs(workspaceURIs, cwd string) bool {
	uri := "file://" + filepath.Clean(cwd)
	return strings.Contains(workspaceURIs, uri+`"`) || strings.Contains(workspaceURIs, uri+`/"`)
}

// Scan summarizes the PC environment for /scan and the HTTP endpoint.
func Scan(cfg *config.Config, st *state.BotSessionState) map[string]any {
	projs := projects.ListAvailableProjects(cfg)
	projNames := make([]string, 0, len(projs))
	for _, p := range projs {
		projNames = append(projNames, p.Name)
	}
	var clis []string
	for _, bin := range []string{"agy", "claude", "git", "go", "flutter", "node"} {
		if _, err := exec.LookPath(bin); err == nil {
			clis = append(clis, bin)
		}
	}
	effDisp, _, _ := models.GetEffectiveModelInfo(cfg, st)
	return map[string]any{
		"ok":           true,
		"engine":       st.CurrentEngine,
		"model":        effDisp,
		"cwd":          st.CurrentCWD,
		"cwd_short":    st.GetCWDShortName(st.CurrentCWD),
		"projects":     projNames,
		"total_projs":  len(projs),
		"clis":         clis,
		"tailscale_ip": config.DetectTailscaleIPv4(),
	}
}

// FormatScan renders Scan as a chat reply.
func FormatScan(sc map[string]any) string {
	projs, _ := sc["projects"].([]string)
	clis, _ := sc["clis"].([]string)
	return fmt.Sprintf(
		"Escaneo de PC completado:\n• Motor: %s · Modelo: %v\n• Proyecto activo: %v\n• Proyectos (%v): %s\n• CLIs detectados: %s",
		sc["engine"], sc["model"], sc["cwd_short"], sc["total_projs"], strings.Join(projs, ", "), strings.Join(clis, ", "),
	)
}
