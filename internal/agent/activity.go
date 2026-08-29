package agent

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// ActivityLabel turns a tool call + raw JSON args into a short, natural
// present-tense phrase ("Reading main.go", "Running go build") for live status
// display. Shared by the TUI and the Telegram gateway so both narrate tool
// activity identically.
//
// If the model supplied an "activity" arg (its own narration), that wins.
// Otherwise a label is derived from the tool name + key argument.
func ActivityLabel(name, rawArgs string) string {
	args := map[string]any{}
	if rawArgs != "" {
		_ = json.Unmarshal([]byte(rawArgs), &args)
	}
	str := func(k string) string {
		if v, ok := args[k].(string); ok {
			return v
		}
		return ""
	}
	// Model-authored narration takes priority.
	if a := strings.TrimSpace(str("activity")); a != "" {
		if len(a) > 56 {
			a = a[:56] + "…"
		}
		return a
	}
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := str(k); v != "" {
				return v
			}
			if arr, ok := args[k].([]any); ok && len(arr) > 0 {
				if s, ok := arr[0].(string); ok && s != "" {
					return s
				}
			}
		}
		return ""
	}
	clip := func(s string, n int) string {
		s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
		if len(s) > n {
			return s[:n] + "…"
		}
		return s
	}
	base := func(p string) string {
		if p == "" {
			return ""
		}
		return filepath.Base(p)
	}

	switch name {
	case "read_file":
		return join("Reading", base(str("path")))
	case "write_file":
		return join("Writing", base(str("path")))
	case "patch":
		return join("Editing", base(str("path")))
	case "search_files":
		q := clip(str("pattern"), 32)
		if str("target") == "files" {
			return join("Finding files", q)
		}
		return join("Searching", q)
	case "terminal":
		return join("Running", clip(str("command"), 44))
	case "execute_code", "execute_python":
		return "Running script"
	case "process":
		return join("Process:", str("action"))
	case "skill_view":
		return join("Opening skill", str("name"))
	case "skill_manage":
		return join("Skill:", str("action"))
	case "web_search":
		return join("Searching web", clip(str("query"), 40))
	case "web_extract", "web_fetch":
		return join("Fetching", clip(first("urls", "url"), 44))
	case "browser_navigate":
		return join("Opening", clip(str("url"), 44))
	case "browser_click":
		return "Clicking element"
	case "browser_type":
		return "Typing in browser"
	case "browser_fetch", "browser_exec":
		return "Browser automation"
	case "browser_screenshot", "browser_screenshot_cdp":
		return "Browser screenshot"
	case "memory":
		return join("Memory:", str("action"))
	case "todo":
		return "Updating todos"
	case "session_search":
		return join("Searching sessions", clip(str("query"), 40))
	}
	if d := clip(first("path", "query", "name", "command", "url"), 40); d != "" {
		return name + " " + d
	}
	return name
}

func join(verb, detail string) string {
	if detail == "" {
		return verb
	}
	return verb + " " + detail
}
