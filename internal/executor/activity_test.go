package executor

import "testing"

func TestClassifyAgyStep(t *testing.T) {
	cases := []struct {
		step  map[string]any
		state string
		label string
	}{
		{map[string]any{"edit_file": map[string]any{"file_path": "/x/internal/bot/queue.go"}}, ActivityCoding, "Editando · queue.go"},
		{map[string]any{"create_file": map[string]any{"file_path": "/x/internal/bot/new.go"}}, ActivityCoding, "Creando · new.go"},
		{map[string]any{"run_command": map[string]any{"command_line": "go test ./..."}}, ActivityExecuting, "Ejecutando · go test ./..."},
		{map[string]any{"view_file": map[string]any{"file_path": "lib/ws_client.dart"}}, ActivityReading, "Leyendo · ws_client.dart"},
		{map[string]any{"thinking_delta": "hm"}, ActivityThinking, "Analizando contexto"},
	}
	for _, c := range cases {
		got, ok := classifyAgyStep(c.step)
		if !ok || got.State != c.state || got.Label != c.label {
			t.Errorf("step %v => %+v, want %s %q", c.step, got, c.state, c.label)
		}
	}
	if _, ok := classifyAgyStep(map[string]any{"text_delta": "hola"}); ok {
		t.Error("text_delta must not produce an activity")
	}
}

func TestClassifyClaudeTool(t *testing.T) {
	got := classifyClaudeTool("Edit", map[string]any{"file_path": "/a/b/server.go"})
	if got.State != ActivityCoding || got.Label != "Editando · server.go" || got.Detail != "/a/b/server.go" {
		t.Errorf("Edit => %+v", got)
	}
	if got := classifyClaudeTool("Write", map[string]any{"file_path": "/a/b/main.go"}); got.Label != "Creando · main.go" {
		t.Errorf("Write => %+v", got)
	}
	if got := classifyClaudeTool("Bash", map[string]any{"command": "ls -la"}); got.State != ActivityExecuting {
		t.Errorf("Bash => %+v", got)
	}
}
