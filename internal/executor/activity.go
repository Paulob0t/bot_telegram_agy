package executor

import (
	"path/filepath"
	"strings"
)

// Activity states sent to the mobile avatar as `agent_status.state`.
const (
	ActivityStarting   = "STARTING"
	ActivityThinking   = "THINKING"
	ActivityCoding     = "CODING_EDIT"
	ActivityExecuting  = "EXECUTING"
	ActivityReading    = "READING"
	ActivityWrappingUp = "WRAPPING_UP"
	ActivityIdle       = "IDLE"
	ActivitySuccess    = "SUCCESS"
	ActivityError      = "ERROR"
)

// Activity is a single granular action of the agent (tool call, reasoning, or lifecycle phase).
type Activity struct {
	State  string
	Label  string
	Detail string
}

func codingEditActivity(path string) Activity {
	return Activity{ActivityCoding, "Editando · " + cleanSnippet(filepath.Base(path), 36), path}
}

func codingCreateActivity(path string) Activity {
	return Activity{ActivityCoding, "Creando · " + cleanSnippet(filepath.Base(path), 36), path}
}

func readingActivity(path string) Activity {
	return Activity{ActivityReading, "Leyendo · " + cleanSnippet(filepath.Base(path), 36), path}
}

func execActivity(cmd string) Activity {
	return Activity{ActivityExecuting, "Ejecutando · " + cleanSnippet(cmd, 36), cmd}
}

func searchActivity(query string) Activity {
	if strings.TrimSpace(query) == "" {
		return Activity{ActivityReading, "Buscando en código", ""}
	}
	return Activity{ActivityReading, "Buscando · " + cleanSnippet(query, 36), query}
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// classifyAgyStep maps an agy `step_update` payload to an Activity.
func classifyAgyStep(step map[string]any) (Activity, bool) {
	if rc, ok := step["run_command"].(map[string]any); ok {
		if c := firstString(rc, "command_line", "command", "CommandLine"); c != "" {
			return execActivity(c), true
		}
	}
	for _, key := range []string{"create_file", "write_file"} {
		if m, ok := step[key].(map[string]any); ok {
			if fp := firstString(m, "file_path", "path", "Path", "TargetFile"); fp != "" {
				return codingCreateActivity(fp), true
			}
		}
	}
	for _, key := range []string{"edit_file", "replace_file_content", "multi_replace_file_content"} {
		if m, ok := step[key].(map[string]any); ok {
			if fp := firstString(m, "file_path", "path", "Path", "TargetFile"); fp != "" {
				return codingEditActivity(fp), true
			}
		}
	}
	for _, key := range []string{"view_file", "read_file", "list_dir"} {
		if m, ok := step[key].(map[string]any); ok {
			if fp := firstString(m, "file_path", "path", "Path", "AbsolutePath"); fp != "" {
				return readingActivity(fp), true
			}
		}
	}
	for _, key := range []string{"grep_search", "search_web", "find_by_name", "codebase_search"} {
		if m, ok := step[key].(map[string]any); ok {
			return searchActivity(firstString(m, "query", "Query", "pattern", "Pattern")), true
		}
	}
	if step["thinking_delta"] != nil || step["thinking"] != nil {
		return Activity{ActivityThinking, "Analizando contexto", ""}, true
	}
	return Activity{}, false
}

// classifyClaudeTool maps a Claude `tool_use` block (name + input) to an Activity.
func classifyClaudeTool(name string, input map[string]any) Activity {
	if input == nil {
		input = map[string]any{}
	}
	switch name {
	case "Write":
		if fp := firstString(input, "file_path", "notebook_path"); fp != "" {
			return codingCreateActivity(fp)
		}
	case "Edit", "MultiEdit", "NotebookEdit":
		if fp := firstString(input, "file_path", "notebook_path"); fp != "" {
			return codingEditActivity(fp)
		}
	case "Read", "LS":
		if fp := firstString(input, "file_path", "path"); fp != "" {
			return readingActivity(fp)
		}
	case "Bash":
		if c := firstString(input, "command"); c != "" {
			return execActivity(c)
		}
	case "Grep", "Glob", "WebSearch", "WebFetch":
		return searchActivity(firstString(input, "pattern", "query", "url"))
	}
	return Activity{ActivityThinking, "Procesando · " + name, ""}
}

// classifyAgyTranscriptLine parses a local 0-token `transcript.jsonl` entry (same source as Ctrl+O in AGY CLI).
func classifyAgyTranscriptLine(content string, stepType string, status string) (Activity, bool) {
	if strings.Contains(content, "Task Description: ") {
		rest := content[strings.Index(content, "Task Description: ")+len("Task Description: "):]
		if nl := strings.IndexByte(rest, '\n'); nl > 0 {
			rest = rest[:nl]
		}
		return execActivity(strings.TrimSpace(rest)), true
	}
	if strings.Contains(content, " replacement in ") || strings.Contains(content, " replacements in ") {
		idx := strings.Index(content, " in /")
		if idx > 0 {
			rest := content[idx+4:]
			if sp := strings.IndexAny(rest, " \n"); sp > 0 {
				rest = rest[:sp]
			}
			return codingEditActivity(rest), true
		}
	}
	if strings.Contains(content, "Created file") {
		return Activity{ActivityCoding, "Creando archivo", ""}, true
	}
	if strings.Contains(content, "Showing lines ") {
		return Activity{ActivityReading, "Leyendo código", ""}, true
	}
	if strings.Contains(content, "Command output:") && status == "RUNNING" {
		return Activity{ActivityExecuting, "Ejecutando comando en terminal", ""}, true
	}
	if stepType == "PLANNER_RESPONSE" && status == "RUNNING" {
		return Activity{ActivityThinking, "Analizando contexto", ""}, true
	}
	return Activity{}, false
}
