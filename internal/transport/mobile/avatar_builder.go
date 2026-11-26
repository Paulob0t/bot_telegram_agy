package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agente-bot/internal/uploads"
)

// AvatarTraits holds the stylized character traits extracted from a user photo.
type AvatarTraits struct {
	Variant     string `json:"variant"`     // "plush_hood", "vinyl_you", "cyber_pod", "spirit_plush"
	GenderStyle string `json:"genderStyle"` // "feminine", "masculine", "neutral"
	Eyelashes   bool   `json:"eyelashes"`
	SkinColor   string `json:"skinColor"`
	HairStyle   string `json:"hairStyle"` // "short", "curly", "spiky", "long", "wavy_long", "ponytail", "afro", "none"
	HairColor   string `json:"hairColor"`
	Glasses     string `json:"glasses"`    // "none", "round", "square", "visor"
	FacialHair  string `json:"facialHair"` // "none", "beard", "goatee", "mustache"
	Headwear    string `json:"headwear"`   // "none", "cap", "beanie", "headphones", "plush_hood"
	HoodieColor string `json:"hoodieColor"`
	AccentColor string `json:"accentColor"`
	Vibe        string `json:"vibe"`
}

func defaultAvatarVariants() []AvatarTraits {
	base := AvatarTraits{
		Variant:     "plush_hood",
		SkinColor:   "#F3D1BC",
		HairStyle:   "curly",
		HairColor:   "#231C18",
		Glasses:     "none",
		FacialHair:  "none",
		Headwear:    "plush_hood",
		HoodieColor: "#EFE6DC",
		AccentColor: "#D97757",
		Vibe:        "Cozy Plush",
	}
	return buildFourVariants(base)
}

func buildFourVariants(base AvatarTraits) []AvatarTraits {
	if !strings.HasPrefix(base.SkinColor, "#") {
		base.SkinColor = "#F3D1BC"
	}
	if !strings.HasPrefix(base.HairColor, "#") {
		base.HairColor = "#231C18"
	}
	if !strings.HasPrefix(base.HoodieColor, "#") {
		base.HoodieColor = "#EFE6DC"
	}
	if !strings.HasPrefix(base.AccentColor, "#") {
		base.AccentColor = "#D97757"
	}
	if base.HairStyle == "" {
		base.HairStyle = "curly"
	}
	if base.Glasses == "" {
		base.Glasses = "none"
	}
	if base.FacialHair == "" {
		base.FacialHair = "none"
	}
	if base.GenderStyle == "" {
		base.GenderStyle = "neutral"
	}
	if base.GenderStyle == "feminine" {
		base.Eyelashes = true
		base.FacialHair = "none"
	}

	// 1. Cozy Plush (exact look from user's reference photo: warm plush hood + soft blush + user traits)
	v1 := base
	v1.Variant = "plush_hood"
	v1.Headwear = "plush_hood"
	v1.Vibe = "Cozy Plush"

	// 2. Vinyl You (shows full hair/curls + glasses/beard from photo in 3D vinyl toy style)
	v2 := base
	v2.Variant = "vinyl_you"
	if v2.Headwear == "plush_hood" {
		v2.Headwear = "none"
	}
	v2.Vibe = "Vinyl Toy"

	// 3. Cyber Pod (plush hood + tech headphones/visor + glowing accent)
	v3 := base
	v3.Variant = "cyber_pod"
	v3.Headwear = "headphones"
	v3.Vibe = "Cyber Pod"

	// 4. Spirit Companion (plush creature hood with ears + user palette & traits)
	v4 := base
	v4.Variant = "spirit_plush"
	v4.Headwear = "plush_hood"
	v4.Vibe = "Spirit Plush"

	return []AvatarTraits{v1, v2, v3, v4}
}

func (s *Server) handleAvatarFromPhoto(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		http.Error(w, "multipart inválido", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "falta campo file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	uploadsDir := uploads.PrepareDir(s.cfg.HomeDir)
	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = ".jpg"
	}
	photoPath := filepath.Join(uploadsDir, fmt.Sprintf("avatar_src_%d%s", time.Now().Unix(), ext))
	out, err := os.Create(photoPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = io.Copy(out, file)
	_ = out.Close()

	variants := s.extractVariantsWithGemini(r.Context(), photoPath)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":       true,
		"traits":   variants[0],
		"variants": variants,
	})
}

func (s *Server) extractVariantsWithGemini(parentCtx context.Context, photoPath string) []AvatarTraits {
	ctx, cancel := context.WithTimeout(parentCtx, 35*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(
		`Read the image file at %q and extract the person's visual traits to build 4 cute 3D plush/vinyl companion character variants. Respond ONLY with a valid single-line JSON object (no markdown fences, no commentary) using this exact schema: {"genderStyle":"feminine|masculine|neutral","eyelashes":true,"skinColor":"#HEX","hairStyle":"short|curly|spiky|long|wavy_long|ponytail|afro|none","hairColor":"#HEX","glasses":"none|round|square|visor","facialHair":"none|beard|goatee|mustache","headwear":"none|cap|beanie|headphones","hoodieColor":"#HEX","accentColor":"#HEX","vibe":"2-word title"}`,
		photoPath,
	)

	bin := s.cfg.AgyBin
	if bin == "" {
		bin = "agy"
	}
	cmd := exec.CommandContext(
		ctx,
		bin,
		"-p", prompt,
		"--model", "gemini-4-hydrogen-low",
		"--dangerously-skip-permissions",
	)
	cmd.Dir = s.cfg.HomeDir
	cmd.Env = append(os.Environ(), "AGENTE_BOT_RUN=1")

	rawOut, err := cmd.CombinedOutput()
	if err != nil {
		return defaultAvatarVariants()
	}

	txt := string(rawOut)
	start := strings.Index(txt, "{")
	end := strings.LastIndex(txt, "}")
	if start == -1 || end <= start {
		return defaultAvatarVariants()
	}

	var parsed AvatarTraits
	if err := json.Unmarshal([]byte(txt[start:end+1]), &parsed); err != nil {
		return defaultAvatarVariants()
	}
	return buildFourVariants(parsed)
}
