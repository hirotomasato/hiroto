package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func registerExecutePython(r *Registry) {
	r.Register(&Tool{
		Name:        "execute_python",
		Description: "Run a Python script that can call Hiroto tools via subprocess. Use `import subprocess, json; def tool(name, args): return json.loads(subprocess.run(['hiroto','tool',name], input=json.dumps(args), capture_output=True, text=True).stdout)`. Max 60s, 50KB output.",
		Parameters:  mustJSON(`{"type":"object","properties":{"code":{"type":"string","description":"Python code to execute. Print final result to stdout."}},"required":["code"]}`),
		Exec:        executePython,
	})
}

const pythonHelper = `
import subprocess, json, sys, os

def tool(name, args=None):
    """Call a Hiroto tool via subprocess."""
    if args is None:
        args = {}
    inp = json.dumps(args)
    try:
        r = subprocess.run(['hiroto', 'tool', name],
            input=inp, capture_output=True, text=True, timeout=55)
        return json.loads(r.stdout)
    except Exception as e:
        return {"Output": "tool error: " + str(e), "IsError": True}

# Convenience wrappers
def terminal(cmd):
    return tool('terminal', {'command': cmd})

def read_file(path):
    return tool('read_file', {'path': path})

def write_file(path, content):
    return tool('write_file', {'path': path, 'content': content})

def web_search(query, limit=5):
    return tool('web_search', {'query': query, 'limit': limit})

def web_fetch(url):
    return tool('web_fetch', {'url': url})

def browser_start():
    return tool('browser_start', {})

def browser_navigate(url):
    return tool('browser_navigate', {'url': url})

def browser_click(selector):
    return tool('browser_click', {'selector': selector})

def browser_exec(code):
    return tool('browser_exec', {'code': code})

def browser_stop():
    return tool('browser_stop', {})
`

func executePython(ctx context.Context, args map[string]any) Result {
	code, _ := args["code"].(string)
	if code == "" {
		return Result{Output: "missing code", IsError: true}
	}
	dir, err := os.MkdirTemp("", "hiroto-py-")
	if err != nil {
		return Result{Output: fmt.Sprintf("temp dir: %v", err), IsError: true}
	}
	defer os.RemoveAll(dir)

	helper := filepath.Join(dir, "hiroto_tools.py")
	script := filepath.Join(dir, "script.py")
	os.WriteFile(helper, []byte(pythonHelper), 0644)
	fullCode := fmt.Sprintf("import sys\nsys.path.insert(0, '.')\nfrom hiroto_tools import *\n%s", code)
	os.WriteFile(script, []byte(fullCode), 0644)

	ctx2, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx2, "python3", script)
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
