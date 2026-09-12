package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ---- kernel scripts ----

const pyKernel = `
import sys, json, traceback, io, os, subprocess

_globals = {
    "display": lambda *a: print(" ".join(str(x) for x in a)),
    "read":   lambda path: open(path).read(),
    "write":  lambda path, content: open(path, "w").write(content),
    "hiroto": lambda name, args=None: json.loads(
        subprocess.run(
            ["hiroto", "tool", name],
            input=json.dumps(args or {}),
            capture_output=True, text=True, timeout=55,
        ).stdout
    ),
    "env":    lambda key=None: os.environ.get(key) if key else dict(os.environ),
}
_globals["print"] = _globals["display"]

for line in sys.stdin:
    line = line.strip()
    if not line: continue
    req = json.loads(line)
    code = req.get("code", "")
    buf = io.StringIO()
    old = sys.stdout
    sys.stdout = buf
    try:
        exec(code, _globals)
        out = buf.getvalue()
        sys.stdout = old
        print(json.dumps({"output": out, "ok": True}), flush=True)
    except Exception as e:
        sys.stdout = old
        print(json.dumps({"output": traceback.format_exc(), "ok": False}), flush=True)
`

const jsKernel = `
const vm = require("vm");
const { execSync } = require("child_process");
const fs = require("fs");

const ctx = vm.createContext({
    display: (...a) => { process.stdout.write(a.map(String).join(" ")); },
    read: (path) => fs.readFileSync(path, "utf8"),
    write: (path, content) => fs.writeFileSync(path, content),
    hiroto: (name, args) => {
        const stdout = execSync("hiroto tool " + name, {
            input: JSON.stringify(args || {}), encoding: "utf-8",
            timeout: 55000, maxBuffer: 50 * 1024,
        });
        return JSON.parse(stdout);
    },
    env: (key) => key ? process.env[key] : process.env,
});
ctx.print = ctx.display;

let buf = "";
process.stdin.on("data", (chunk) => {
    buf += chunk;
    while (true) {
        const nl = buf.indexOf("\n");
        if (nl === -1) break;
        const line = buf.slice(0, nl);
        buf = buf.slice(nl + 1);
        let req;
        try { req = JSON.parse(line); } catch(e) { continue; }
        let out = "";
        const orig = process.stdout.write.bind(process.stdout);
        process.stdout.write = (s) => { out += String(s); return true; };
        try {
            vm.runInContext(req.code, ctx, { timeout: 55000 });
            process.stdout.write = orig;
            console.log(JSON.stringify({output: out, ok: true}));
        } catch(e) {
            process.stdout.write = orig;
            console.log(JSON.stringify({output: String(e.stack || e), ok: false}));
        }
    }
});
`

// ---- kernel manager ----

type evalKernel struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	mu     sync.Mutex
	lang   string
	alive  bool
}

var (
	evalMu     sync.Mutex
	evalKernels = map[string]*evalKernel{}
)

// getKernel returns a running kernel for the language, starting one if needed.
// A dead kernel (process exited) is restarted transparently.
func getKernel(ctx context.Context, lang string) (*evalKernel, error) {
	evalMu.Lock()
	defer evalMu.Unlock()
	k, ok := evalKernels[lang]
	if ok {
		if k.alive && k.cmd.ProcessState == nil {
			return k, nil
		}
		_ = k.cmd.Process.Kill()
		delete(evalKernels, lang)
	}
	k, err := startKernel(ctx, lang)
	if err != nil {
		return nil, err
	}
	evalKernels[lang] = k
	return k, nil
}

func startKernel(ctx context.Context, lang string) (*evalKernel, error) {
	var bin string
	var flag string
	var script string
	switch lang {
	case "python":
		bin = "python3"
		flag = "-c"
		script = pyKernel
	case "javascript":
		bin = "node"
		flag = "-e"
		script = jsKernel
	default:
		return nil, fmt.Errorf("unknown eval language: %s", lang)
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("%s not installed", bin)
	}

	// Kernel lives for the entire hiroto process — not tied to a caller context.
	cmd := exec.Command(bin, flag, script)
	cmd.Env = sanitizeEnv()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s kernel: %w", lang, err)
	}

	k := &evalKernel{
		cmd:    cmd,
		stdin:  stdin,
		reader: bufio.NewReader(stdout),
		lang:   lang,
		alive:  true,
	}

	go func() {
		cmd.Wait()
		k.alive = false
	}()

	return k, nil
}

// send sends code to the kernel and returns the result. Thread-safe.
// A 55s deadline guards against runaway loops.
func (k *evalKernel) send(code string) evalResult {
	k.mu.Lock()
	defer k.mu.Unlock()

	req, _ := json.Marshal(map[string]string{"code": code})
	_, err := fmt.Fprintf(k.stdin, "%s\n", req)
	if err != nil {
		return evalResult{Output: fmt.Sprintf("kernel write error: %v", err)}
	}

	resultCh := make(chan evalResult, 1)
	go func() { resultCh <- k.readResult() }()
	select {
	case r := <-resultCh:
		return r
	case <-time.After(55 * time.Second):
		k.Kill()
		return evalResult{Output: "eval timed out after 55s"}
	}
}

func (k *evalKernel) readResult() evalResult {
	// The stdout pipe is merged with stderr. We need to read ONE line of JSON.
	// But if code produces stdout, that comes AFTER the JSON response.
	// The kernel design: code stdout is captured into the JSON response.
	// So the JSON line is the ONLY output from the kernel per execution.
	line, err := k.reader.ReadString('\n')
	if err != nil {
		return evalResult{Output: fmt.Sprintf("kernel read error: %v", err)}
	}
	line = strings.TrimSpace(line)
	var r evalResult
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		for i := 0; i < 3; i++ {
			line, err = k.reader.ReadString('\n')
			if err != nil {
				return evalResult{Output: fmt.Sprintf("kernel response garbled: %s", line)}
			}
			line = strings.TrimSpace(line)
			if err := json.Unmarshal([]byte(line), &r); err == nil {
				break
			}
		}
		if r.Output == "" && r.OK {
			return evalResult{Output: fmt.Sprintf("kernel output lost (non-JSON line: %s)", line)}
		}
	}
	if !r.OK {
		r.Output = "ERROR: " + r.Output
	}
	if len(r.Output) > 50000 {
		r.Output = r.Output[:50000] + "\n... (truncated)"
	}
	return r
}

// Kill terminates the kernel process.
func (k *evalKernel) Kill() {
	k.alive = false
	if k.cmd.Process != nil {
		_ = k.cmd.Process.Kill()
	}
}

type evalResult struct {
	Output string `json:"output"`
	OK     bool   `json:"ok"`
}

// KillAllKernels terminates every running eval kernel.
func KillAllKernels() {
	evalMu.Lock()
	defer evalMu.Unlock()
	for _, k := range evalKernels {
		k.Kill()
	}
	evalKernels = map[string]*evalKernel{}
}

// ---- tool registration ----

func registerEval(r *Registry) {
	r.Register(&Tool{
		Name:        "eval",
		Description: "Execute Python or JavaScript code with persistent state across calls — imports, variables, and functions survive between invocations. Use for exploration, testing, data analysis, and tool scripting. The kernel stays alive for the duration of the session. language: 'python' or 'javascript'.",
		Parameters: mustJSON(`{"type":"object","properties":{"language":{"type":"string","enum":["python","javascript"],"description":"Runtime language"},"code":{"type":"string","description":"Code to execute"}},"required":["language","code"]}`),
		Exec: func(ctx context.Context, args map[string]any) Result {
			return evalExec(ctx, args)
		},
	})
}

func evalExec(ctx context.Context, args map[string]any) Result {
	lang, _ := args["language"].(string)
	code, _ := args["code"].(string)
	if lang == "" || code == "" {
		return Result{Output: "need language and code", IsError: true}
	}
	if lang != "python" && lang != "javascript" {
		return Result{Output: "language must be 'python' or 'javascript'", IsError: true}
	}

	k, err := getKernel(ctx, lang)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}
	}

	r := k.send(code)
	return Result{Output: r.Output, IsError: !r.OK}
}