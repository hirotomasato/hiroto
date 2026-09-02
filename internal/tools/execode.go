package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func registerExecuteCode(r *Registry) {
	r.Register(&Tool{
		Name:        "execute_code",
		Description: "Run a JavaScript (Node.js) script that can call Hiroto tools via the `hiroto` global. Use hiroto.terminal(cmd), hiroto.readFile(path), hiroto.writeFile(path, content), hiroto.webSearch(query), hiroto.webFetch(url), hiroto.searchFiles(pattern), hiroto.browserFetch(url). Returns stdout. Max 60s, 50KB output.",
		Parameters:  mustJSON("{\"type\":\"object\",\"properties\":{\"code\":{\"type\":\"string\",\"description\":\"JavaScript code to execute. Use hiroto.toolName(args) to call tools. Print final result to stdout.\"}},\"required\":[\"code\"]}"),
		Exec:        executeCode,
	})
}

const hirotoJSHelper = `
const { execSync } = require('child_process');

function tool(name, args) {
  const input = JSON.stringify(args || {});
  try {
    const stdout = execSync('hiroto tool ' + name, {
      input: input, encoding: 'utf-8', timeout: 55000, maxBuffer: 50 * 1024,
    });
    return JSON.parse(stdout);
  } catch (e) {
    return { output: 'tool error: ' + (e.stderr || e.message), isError: true };
  }
}

const hiroto = {
  terminal: function(cmd) { return tool('terminal', { command: cmd }); },
  readFile: function(p) { return tool('read_file', { path: p }); },
  writeFile: function(p, c) { return tool('write_file', { path: p, content: c }); },
  patch: function(p, o, n) { return tool('patch', { path: p, old_string: o, new_string: n }); },
  searchFiles: function(pat) { return tool('search_files', { pattern: pat }); },
  webSearch: function(q, n) { return tool('web_search', { query: q, limit: n || 5 }); },
  webFetch: function(url) { return tool('web_fetch', { url: url }); },
  browserStart: function() { return tool('browser_start', {}); },
  browserNavigate: function(url) { return tool('browser_navigate', { url: url }); },
  browserClick: function(sel) { return tool('browser_click', { selector: sel }); },
  browserType: function(sel, text) { return tool('browser_type', { selector: sel, text: text }); },
  browserExec: function(code) { return tool('browser_exec', { code: code }); },
  browserScreenshot: function(path) { return tool('browser_screenshot_cdp', { path: path }); },
  browserStop: function() { return tool('browser_stop', {}); },
  process: function(action, id, cmd) { return tool('process', { action: action, id: id, command: cmd }); },
  memory: function(action, target, content) { return tool('memory', { action: action, target: target, content: content }); },
  todo: function(action, items) { return tool('todo', { action: action, items: items }); },
  todoComplete: function(id) { return tool('todo', { action: 'complete', id: id }); },
  todoClear: function() { return tool('todo', { action: 'clear' }); },
  skillView: function(name) { return tool('skill_view', { name: name }); },
  sessionSearch: function(query) { return tool('session_search', { query: query }); },
  vision: function(path, question) { return tool('vision_analyze', { path: path, question: question }); },
};
globalThis.hiroto = hiroto;
`

func executeCode(ctx context.Context, args map[string]any) Result {
	code, _ := args["code"].(string)
	if code == "" {
		return Result{Output: "missing code", IsError: true}
	}
	dir, err := os.MkdirTemp("", "hiroto-exec-")
	if err != nil {
		return Result{Output: fmt.Sprintf("temp dir: %v", err), IsError: true}
	}
	defer os.RemoveAll(dir)

	helper := filepath.Join(dir, "hiroto_tools.js")
	script := filepath.Join(dir, "script.js")
	os.WriteFile(helper, []byte(hirotoJSHelper), 0644)
	os.WriteFile(script, []byte("require('./hiroto_tools.js');\n"+code), 0644)

	ctx2, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx2, "node", script)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		if result == "" {
			result = err.Error()
		}
		return Result{Output: result, IsError: true}
	}
	if len(result) > 50000 {
		result = result[:50000] + "\n... (truncated)"
	}
	return Result{Output: result}
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, _ := n.Int64()
		return int(i), true
	}
	return 0, false
}
