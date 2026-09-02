// Package tools defines the tool interface and the built-in tools:
// terminal, file read/write, web search/extract, memory, todo, skill loader.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Result of a tool execution.
type Result struct {
	Output  string
	IsError bool
}

// Tool is a callable the LLM can invoke (OpenAI function-calling shape).
type Tool struct {
	Name        string
	Description string
	// Parameters is a JSON schema.
	Parameters map[string]any
	// Exec runs the tool. args is the parsed JSON object of arguments.
	Exec func(ctx context.Context, args map[string]any) Result
}

// Schema helper.
func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}
func str(desc string) map[string]any   { return map[string]any{"type": "string", "description": desc} }
func boolp(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }

// Registry holds all available tools.
type Registry struct {
	tools map[string]*Tool
	order []string
}

func NewRegistry() *Registry { return &Registry{tools: map[string]*Tool{}} }

func (r *Registry) Register(t *Tool) {
	if _, dup := r.tools[t.Name]; !dup {
		r.order = append(r.order, t.Name)
	}
	r.tools[t.Name] = t
}

func (r *Registry) Get(name string) (*Tool, bool) { t, ok := r.tools[name]; return t, ok }

func (r *Registry) Names() []string { return r.order }

// LLMTools renders the registry into OpenAI tool definitions.
// Every tool gains an optional "activity" string param so the model can narrate
// each step in plain language (Hermes-style live status). Tools ignore it at
// Exec time; the UI reads it to label the tool_start/tool_end lines.
func (r *Registry) LLMTools() []llmTool {
	out := make([]llmTool, 0, len(r.order))
	for _, n := range r.order {
		t := r.tools[n]
		out = append(out, llmTool{
			Type: "function",
			Function: llmFn{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  withActivityParam(t.Parameters),
			},
		})
	}
	return out
}

// withActivityParam returns a shallow copy of a tool's JSON-schema parameters
// with an optional "activity" string property added. The original schema is
// left untouched (Exec still reads its own keys).
func withActivityParam(params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{"type": "object"}
	}
	cp := make(map[string]any, len(params)+1)
	for k, v := range params {
		cp[k] = v
	}
	props, _ := cp["properties"].(map[string]any)
	newProps := make(map[string]any, len(props)+1)
	for k, v := range props {
		newProps[k] = v
	}
	newProps["activity"] = map[string]any{
		"type":        "string",
		"description": "A short present-tense description (3-6 words) of what this call is doing, shown to the user as live status. E.g. 'Reading the config file', 'Running the test suite'.",
	}
	cp["properties"] = newProps
	return cp
}

// llmTool mirrors llm.Tool without import cycle (structurally identical JSON).
type llmTool struct {
	Type     string `json:"type"`
	Function llmFn  `json:"function"`
}
type llmFn struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// ---------- built-in tools ----------

// RegisterBuiltin wires up the default toolset (Hiroto's core tools).
func RegisterBuiltin(r *Registry, opts Options) {
	registerTerminal(r, opts)
	registerReadFile(r)
	registerWriteFile(r, opts)
	registerGlob(r)
	registerWebSearch(r, opts)
	registerWebExtract(r, opts)
	registerWebFetchAlias(r)
	registerBrowserTools(r)
	registerBrowserCDP(r)
	registerExecuteCode(r)
	registerExecutePython(r)
	registerDelegateTask(r)
	registerCronjob(r)
	registerNativeTools(r)
	registerPatch(r, opts)
	registerProcess(r)
	registerClarify(r)
	registerVision(r, opts)
	registerSkillManage(r)
	registerSessionSearch(r, opts)
	registerMemory(r, opts)
	registerTodo(r, opts)
	registerSkillView(r, opts)
}

type Options struct {
	TermTimeout   time.Duration
	Workdir       string
	WebSearch     func(query string, limit int) ([]SearchHit, error)
	WebExtract    func(urls []string) ([]PageResult, error)
	Memory        MemoryStore
	Todo          *TodoStore
	Skills        SkillIndex
	SessionSearch SessionSearchFunc
	LLMClient     LLMClient // for vision_analyze to call the model
}

// LLMClient is a minimal interface for vision_analyze to call the LLM.
type LLMClient interface {
	Chat(ctx context.Context, messages []LLMMessage) (string, error)
}

// LLMMessage is a simplified message for the LLM client.
type LLMMessage struct {
	Role    string
	Content string
}

type SearchHit struct {
	Title, URL, Description string
}
type PageResult struct {
	URL, Title, Content string
}

type MemoryStore interface {
	Add(target, content string) string
	Remove(target, idOrText string) bool
	List(target string) []string // rendered "id| content" lines
	PromptBlock() string
}

type SkillIndex interface {
	Find(name string) (path string, ok bool)
	Names() []string
}

// ---- terminal ----
func registerTerminal(r *Registry, opts Options) {
	cwd := opts.Workdir
	r.Register(&Tool{
		Name:        "terminal",
		Description: "Execute a shell command on the local machine. Working directory persists between calls. Use for builds, installs, git, processes, scripts, network. Do NOT use for reading single files (use read_file).",
		Parameters: obj(map[string]any{
			"command": str("The shell command to execute"),
			"workdir": str("Optional working directory for this command"),
			"timeout": map[string]any{"type": "integer", "description": "Timeout seconds (default from config)"},
		}, "command"),
		Exec: func(ctx context.Context, args map[string]any) Result {
			cmdStr, _ := args["command"].(string)
			if strings.TrimSpace(cmdStr) == "" {
				return Result{Output: "empty command", IsError: true}
			}
			// Dangerous command detection.
			if d := dangerousCmd(cmdStr); d != "" {
				return Result{Output: "BLOCKED: " + d + "\n\nGunakan --force untuk tetap menjalankan: terminal(command=\"--force " + cmdStr + "\")", IsError: true}
			}
			// Strip --force prefix if present.
			if strings.HasPrefix(cmdStr, "--force ") {
				cmdStr = strings.TrimPrefix(cmdStr, "--force ")
			}
			wd := cwd
			if w, ok := args["workdir"].(string); ok && w != "" {
				wd = w
			}
			timeout := opts.TermTimeout
			if t, ok := args["timeout"].(float64); ok && t > 0 {
				timeout = time.Duration(t) * time.Second
			}
			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			cmd := exec.CommandContext(cctx, shellCmd(), shellFlag(), cmdStr)
			cmd.Dir = wd
			out, err := cmd.CombinedOutput()
			res := string(out)
			if err != nil {
				if cctx.Err() == context.DeadlineExceeded {
					res += fmt.Sprintf("\n[command timed out after %s]", timeout)
				} else {
					res += fmt.Sprintf("\n[exit code: %v]", exitCode(err))
				}
			}
			if len(res) > 30000 {
				res = res[:30000] + "\n[output truncated]"
			}
			return Result{Output: res, IsError: err != nil}
		},
	})
}

func exitCode(err error) int {
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

// shellCmd returns the shell to use based on OS.
func shellCmd() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "bash"
}

// shellFlag returns the flag to pass a command to the shell.
func shellFlag() string {
	if runtime.GOOS == "windows" {
		return "/c"
	}
	return "-c"
}

// ---- read_file ----
func registerReadFile(r *Registry) {
	r.Register(&Tool{
		Name:        "read_file",
		Description: "Read a text file with line numbers. Returns 'LINE_NUM|CONTENT'. Supports offset/limit pagination for large files.",
		Parameters: obj(map[string]any{
			"path":   str("File path (absolute or relative)"),
			"offset": map[string]any{"type": "integer", "description": "1-indexed start line (default 1)"},
			"limit":  map[string]any{"type": "integer", "description": "Max lines (default 2000)"},
		}, "path"),
		Exec: func(ctx context.Context, args map[string]any) Result {
			path, _ := args["path"].(string)
			data, err := os.ReadFile(path)
			if err != nil {
				return Result{Output: err.Error(), IsError: true}
			}
			offset := 1
			if o, ok := args["offset"].(float64); ok && o > 0 {
				offset = int(o)
			}
			limit := 2000
			if l, ok := args["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
			total := len(lines)
			if offset > total {
				return Result{Output: fmt.Sprintf("file has %d lines; offset %d beyond end", total, offset), IsError: true}
			}
			end := offset - 1 + limit
			if end > total {
				end = total
			}
			var b strings.Builder
			for i := offset - 1; i < end; i++ {
				fmt.Fprintf(&b, "%d|%s\n", i+1, lines[i])
			}
			out := strings.TrimRight(b.String(), "\n")
			if end < total {
				out += fmt.Sprintf("\n[%d more lines; continue with offset=%d]", total-end, end+1)
			}
			return Result{Output: out}
		},
	})
}

// ---- write_file ----
func registerWriteFile(r *Registry, opts Options) {
	r.Register(&Tool{
		Name:        "write_file",
		Description: "Write content to a file, completely replacing it. Creates parent directories. Use terminal + heredoc only for appending. After writing, runs LSP diagnostics (go vet, py_compile) on the file and reports any errors.",
		Parameters: obj(map[string]any{
			"path":    str("File path"),
			"content": str("Complete file content"),
		}, "path", "content"),
		Exec: func(ctx context.Context, args map[string]any) Result {
			path, _ := args["path"].(string)
			content, _ := args["content"].(string)
			if path == "" {
				return Result{Output: "empty path", IsError: true}
			}
			if dir := filepath.Dir(path); dir != "" {
				_ = os.MkdirAll(dir, 0o755)
			}
			autoCheckpoint(opts.Workdir)
			before := ""
			if old, err := os.ReadFile(path); err == nil {
				before = string(old)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				return Result{Output: err.Error(), IsError: true}
			}
			out := fmt.Sprintf("wrote %s (%d bytes)", path, len(content))
			if d := unifiedDiff(path, before, content); d != "" {
				out += "\n" + d
			}
			if diag := lspCheck(path); diag != "" {
				out += "\n" + diag
			}
			return Result{Output: out}
		},
	})
}

// ---- glob ----
func registerGlob(r *Registry) {
	r.Register(&Tool{
		Name:        "search_files",
		Description: "Find files by glob pattern (e.g. '*.go', 'config*.yaml') under a directory, sorted by modification time.",
		Parameters: obj(map[string]any{
			"pattern": str("Glob pattern"),
			"path":    str("Directory to search (default: current)"),
		}, "pattern"),
		Exec: func(ctx context.Context, args map[string]any) Result {
			pattern, _ := args["pattern"].(string)
			dir, _ := args["path"].(string)
			if dir == "" {
				dir = "."
			}
			hits, err := filepath.Glob(filepath.Join(dir, pattern))
			if err != nil {
				return Result{Output: err.Error(), IsError: true}
			}
			if len(hits) == 0 {
				return Result{Output: "no matches"}
			}
			if len(hits) > 200 {
				hits = hits[:200]
			}
			return Result{Output: strings.Join(hits, "\n")}
		},
	})
}

// ---- web_search / web_extract (wired to optional web backend) ----
func registerWebSearch(r *Registry, opts Options) {
	r.Register(&Tool{
		Name:        "web_search",
		Description: "Search the web. Returns titles, URLs, and descriptions.",
		Parameters: obj(map[string]any{
			"query": str("Search query"),
			"limit": map[string]any{"type": "integer", "description": "Max results (default 5)"},
		}, "query"),
		Exec: func(ctx context.Context, args map[string]any) Result {
			if opts.WebSearch == nil {
				return Result{Output: "web_search backend not configured", IsError: true}
			}
			q, _ := args["query"].(string)
			limit := 5
			if l, ok := args["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			hits, err := opts.WebSearch(q, limit)
			if err != nil {
				return Result{Output: err.Error(), IsError: true}
			}
			var b strings.Builder
			for i, h := range hits {
				fmt.Fprintf(&b, "%d. %s\n   %s\n   %s\n", i+1, h.Title, h.URL, h.Description)
			}
			return Result{Output: b.String()}
		},
	})
}

func registerWebExtract(r *Registry, opts Options) {
	r.Register(&Tool{
		Name:        "web_extract",
		Description: "Extract readable content (markdown) from web page URLs.",
		Parameters: obj(map[string]any{
			"urls": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "URLs to extract"},
		}, "urls"),
		Exec: func(ctx context.Context, args map[string]any) Result {
			if opts.WebExtract == nil {
				return Result{Output: "web_extract backend not configured", IsError: true}
			}
			raw, _ := args["urls"].([]any)
			var urls []string
			for _, u := range raw {
				if s, ok := u.(string); ok {
					urls = append(urls, s)
				}
			}
			pages, err := opts.WebExtract(urls)
			if err != nil {
				return Result{Output: err.Error(), IsError: true}
			}
			var b strings.Builder
			for _, p := range pages {
				fmt.Fprintf(&b, "# %s\n%s\n\n", p.Title, p.Content)
			}
			out := b.String()
			if len(out) > 30000 {
				out = out[:30000] + "\n[truncated]"
			}
			return Result{Output: out}
		},
	})
}

// ---- memory ----
func registerMemory(r *Registry, opts Options) {
	r.Register(&Tool{
		Name:        "memory",
		Description: "Save a durable fact to persistent memory (injected into every future session). Actions: add / remove / list.",
		Parameters: obj(map[string]any{
			"action":  map[string]any{"type": "string", "enum": []string{"add", "remove", "list"}, "description": "Operation"},
			"target":  map[string]any{"type": "string", "enum": []string{"user", "memory"}, "description": "user = who the user is; memory = environment/convention notes"},
			"content": str("Entry text (for add) or id/text to match (for remove)"),
		}, "action"),
		Exec: func(ctx context.Context, args map[string]any) Result {
			if opts.Memory == nil {
				return Result{Output: "memory store not initialized", IsError: true}
			}
			action, _ := args["action"].(string)
			target := "memory"
			if t, ok := args["target"].(string); ok && t != "" {
				target = t
			}
			content, _ := args["content"].(string)
			switch action {
			case "add":
				id := opts.Memory.Add(target, content)
				return Result{Output: fmt.Sprintf("saved %s: %s", id, content)}
			case "remove":
				if opts.Memory.Remove(target, content) {
					return Result{Output: "removed"}
				}
				return Result{Output: "no matching entry", IsError: true}
			default:
				lines := opts.Memory.List(target)
				if len(lines) == 0 {
					return Result{Output: "(empty)"}
				}
				return Result{Output: strings.Join(lines, "\n")}
			}
		},
	})
}

// ---- todo ----
func registerTodo(r *Registry, opts Options) {
	ts := opts.Todo
	if ts == nil {
		ts = NewTodoStore()
	}
	r.Register(&Tool{
		Name: "todo",
		Description: "Manage your task list for multi-step work (3+ steps). Actions: " +
			"write (replace the whole list), update (merge changed items by id — advance a task without restating the list), " +
			"read (current list), complete (mark one id completed), clear (drop the list when the work is done). " +
			"Exactly one task may be in_progress. Mark tasks completed as you finish them and clear the list at the end of the job — " +
			"a task left in_progress is shown to the user as still running.",
		Parameters: obj(map[string]any{
			"action": map[string]any{"type": "string", "enum": []string{"write", "update", "read", "complete", "clear"}, "description": "Operation"},
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":       "object",
					"properties": map[string]any{"id": str("short id"), "content": str("task"), "status": map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed", "cancelled"}}},
				},
				"description": "Task items (for write/update)",
			},
			"id": str("task id (for complete)"),
		}, "action"),
		Exec: func(ctx context.Context, args map[string]any) Result {
			action, _ := args["action"].(string)
			switch action {
			case "write", "update":
				raw, _ := args["items"].([]any)
				var items []TodoItem
				for _, it := range raw {
					m, _ := it.(map[string]any)
					if m == nil {
						continue
					}
					id, _ := m["id"].(string)
					c, _ := m["content"].(string)
					s, _ := m["status"].(string)
					items = append(items, TodoItem{ID: id, Content: c, Status: s})
				}
				if len(items) == 0 {
					return Result{Output: "no items given — pass items:[{id,content,status}]", IsError: true}
				}
				if action == "update" {
					ts.Update(items)
				} else {
					ts.Save(items)
				}
				return Result{Output: ts.Render()}
			case "complete":
				id, _ := args["id"].(string)
				if id == "" {
					return Result{Output: `complete needs "id"`, IsError: true}
				}
				if !ts.Complete(id) {
					return Result{Output: "unknown task id: " + id + "\n" + ts.Render(), IsError: true}
				}
				return Result{Output: ts.Render()}
			case "clear":
				ts.Clear()
				return Result{Output: "task list cleared"}
			default:
				ts.Reload()
				return Result{Output: ts.Render()}
			}
		},
	})
}

// ---- skill_view ----
var skillNameRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

func registerSkillView(r *Registry, opts Options) {
	r.Register(&Tool{
		Name:        "skill_view",
		Description: "Load the full content of a skill by name. Use when a skill in the index matches the current task, BEFORE acting on it.",
		Parameters: obj(map[string]any{
			"name": str("Skill name from the index"),
		}, "name"),
		Exec: func(ctx context.Context, args map[string]any) Result {
			name, _ := args["name"].(string)
			if !skillNameRe.MatchString(name) {
				return Result{Output: "invalid skill name", IsError: true}
			}
			if opts.Skills == nil {
				return Result{Output: "skills not loaded", IsError: true}
			}
			path, ok := opts.Skills.Find(name)
			if !ok {
				return Result{Output: fmt.Sprintf("skill %q not found; available: %s", name, strings.Join(opts.Skills.Names(), ", ")), IsError: true}
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return Result{Output: err.Error(), IsError: true}
			}
			out := string(data)
			if len(out) > 40000 {
				out = out[:40000] + "\n[truncated]"
			}
			return Result{Output: out}
		},
	})
}

// JSONArgs parses tool-call arguments defensively.
func JSONArgs(s string) map[string]any {
	m := map[string]any{}
	if strings.TrimSpace(s) == "" {
		return m
	}
	_ = json.Unmarshal([]byte(s), &m)
	return m
}

// mustJSON unmarshals JSON into a map[string]any for tool Parameters (panics on invalid).
func mustJSON(s string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		panic("invalid JSON in tool definition: " + s)
	}
	return m
}

// registerWebFetchAlias adds web_fetch as an alias for web_extract.
func registerWebFetchAlias(r *Registry) {
	r.Register(&Tool{
		Name:        "web_fetch",
		Description: "Fetch a URL and return readable text content (alias for web_extract).",
		Parameters:  obj(map[string]any{"url": map[string]any{"type": "string", "description": "URL to fetch"}}, "url"),
		Exec: func(ctx context.Context, args map[string]any) Result {
			url, _ := args["url"].(string)
			if url == "" {
				return Result{Output: "missing url", IsError: true}
			}
			// Delegate to web_extract — look up the registered tool.
			t, ok := r.Get("web_extract")
			if !ok {
				return Result{Output: "web_extract not available", IsError: true}
			}
			return t.Exec(ctx, map[string]any{"urls": []any{url}})
		},
	})
}

// dangerousCmd checks for dangerous shell commands and returns a reason if blocked.
// The --force prefix bypasses the check.
func dangerousCmd(cmd string) string {
	// Strip --force prefix.
	if strings.HasPrefix(cmd, "--force ") {
		return ""
	}
	lower := strings.ToLower(cmd)
	// Destructive file operations on root/system paths.
	if strings.Contains(lower, "rm -rf /") || strings.Contains(lower, "rm -rf ~") ||
		strings.Contains(lower, "rm -rf /etc") || strings.Contains(lower, "rm -rf /usr") ||
		strings.Contains(lower, "rm -rf /var") || strings.Contains(lower, "rm -rf /home") {
		return "perintah menghapus direktori sistem secara rekursif"
	}
	// Force push to main/master.
	if strings.Contains(lower, "git push") && (strings.Contains(lower, "--force") || strings.Contains(lower, "-f")) &&
		(strings.Contains(lower, "main") || strings.Contains(lower, "master")) {
		return "git push --force ke branch main/master"
	}
	// Database drops.
	if strings.Contains(lower, "drop database") || strings.Contains(lower, "drop table") {
		return "perintah DROP pada database"
	}
	// Fork bombs and dangerous redirects.
	if strings.Contains(lower, ":(){ :|:& };:") || strings.Contains(lower, "fork bomb") {
		return "fork bomb terdeteksi"
	}
	// Overwrite critical system files.
	if strings.Contains(lower, "dd if=") && (strings.Contains(lower, "of=/dev/sd") || strings.Contains(lower, "of=/dev/nvme")) {
		return "dd overwrite ke disk device"
	}
	// chmod 777 on system dirs.
	if strings.Contains(lower, "chmod") && strings.Contains(lower, "777") &&
		(strings.Contains(lower, "/etc") || strings.Contains(lower, "/usr") || strings.Contains(lower, "/var") || strings.Contains(lower, "/")) {
		return "chmod 777 pada direktori sistem"
	}
	return ""
}
