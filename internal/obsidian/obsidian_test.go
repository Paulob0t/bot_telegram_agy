package obsidian

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agente-bot/internal/config"
)

func TestSavePDFToVaultAndDirectory(t *testing.T) {
	tmp := t.TempDir()
	vaultDir := filepath.Join(tmp, "wicode")
	_ = os.MkdirAll(filepath.Join(vaultDir, "Stover"), 0o755)
	_ = os.MkdirAll(filepath.Join(vaultDir, "Wicode"), 0o755)

	cfg := &config.Config{
		ObsidianVaultDir: vaultDir,
	}

	projs := ListVaultProjects(cfg)
	if len(projs) != 2 || projs[0].Name != "Stover" || projs[1].Name != "Wicode" {
		t.Fatalf("unexpected vault projects: %+v", projs)
	}

	srcPDF := filepath.Join(tmp, "upload_123_req.pdf")
	if err := os.WriteFile(srcPDF, []byte("%PDF-1.7 test content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Guardar en carpeta de proyecto (cwd)
	cwdDir := filepath.Join(tmp, "my_project")
	destCWD, err := SaveFileToDir(srcPDF, cwdDir, "req.pdf")
	if err != nil {
		t.Fatalf("SaveFileToDir failed: %v", err)
	}
	raw, err := os.ReadFile(destCWD)
	if err != nil || string(raw) != "%PDF-1.7 test content" {
		t.Fatalf("unexpected saved content: %v", err)
	}

	// 2. Guardar en Obsidian <Proyecto>/PDF
	destObs, err := SavePDFToVaultProject(cfg, srcPDF, "Stover", "modulo_clientes.pdf")
	if err != nil {
		t.Fatalf("SavePDFToVaultProject failed: %v", err)
	}
	if !strings.HasSuffix(destObs, filepath.Join("Stover", "PDF", "modulo_clientes.pdf")) {
		t.Fatalf("unexpected obsidian PDF path: %s", destObs)
	}

	// 3. Generar prompt de Wicode
	prompt, _ := BuildWicodeGeneratorPrompt(cfg, "Stover", destObs, cwdDir)
	if !strings.Contains(prompt, "wicode-prompt-generator") || !strings.Contains(prompt, "modulo_clientes.pdf") {
		t.Fatalf("unexpected generator prompt: %s", prompt)
	}
}
