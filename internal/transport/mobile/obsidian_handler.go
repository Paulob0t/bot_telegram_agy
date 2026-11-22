package mobile

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type obsidianEntry struct {
	Name    string `json:"name"`
	RelPath string `json:"rel_path"`
	IsDir   bool   `json:"is_dir"`
}

func (s *Server) handleObsidian(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}

	vaultRoot := filepath.Clean(s.cfg.ObsidianVaultDir)
	reqRel := strings.TrimSpace(r.URL.Query().Get("path"))
	reqRel = strings.TrimPrefix(filepath.Clean("/"+reqRel), "/")
	if reqRel == "." {
		reqRel = ""
	}

	targetPath := filepath.Join(vaultRoot, reqRel)
	if !strings.HasPrefix(targetPath, vaultRoot) {
		http.Error(w, "ruta fuera de bóveda", http.StatusForbidden)
		return
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		http.Error(w, "no encontrado", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Si es un archivo .md, devolver su contenido completo (0 tokens) para copiar/enviar
	if !info.IsDir() {
		raw, err := os.ReadFile(targetPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"is_file":  true,
			"name":     filepath.Base(targetPath),
			"rel_path": reqRel,
			"content":  string(raw),
		})
		return
	}

	// Si es carpeta, listar subcarpetas y archivos .md
	dirEntries, err := os.ReadDir(targetPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var items []obsidianEntry
	for _, e := range dirEntries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !e.IsDir() && !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		rel := name
		if reqRel != "" {
			rel = filepath.ToSlash(filepath.Join(reqRel, name))
		}
		items = append(items, obsidianEntry{
			Name:    name,
			RelPath: rel,
			IsDir:   e.IsDir(),
		})
	}

	// Ordenar: carpetas primero (con "Prompts" hasta arriba si existe), luego archivos alfabéticamente
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		if items[i].IsDir && strings.EqualFold(items[i].Name, "Prompts") {
			return true
		}
		if items[j].IsDir && strings.EqualFold(items[j].Name, "Prompts") {
			return false
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":       true,
		"is_file":  false,
		"rel_path": reqRel,
		"items":    items,
	})
}
