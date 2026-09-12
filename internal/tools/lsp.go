package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// lspServerFor maps a file extension to a language-server binary.
func lspServerFor(ext string) (bin string, args []string, ok bool) {
	switch ext {
	case ".go":
		return "gopls", nil, true
	case ".py", ".pyi":
		if _, err := exec.LookPath("pyright-langserver"); err == nil {
			return "pyright-langserver", []string{"--stdio"}, true
		}
		return "pylsp", nil, true
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return "typescript-language-server", []string{"--stdio"}, true
	case ".rs":
		return "rust-analyzer", nil, true
	default:
		return "", nil, false
	}
}

func languageID(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py", ".pyi":
		return "python"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	case ".rs":
		return "rust"
	default:
		return ""
	}
}

// workspaceRoot walks up from a file to the nearest project marker
// (go.mod, package.json, Cargo.toml, pyproject.toml, setup.py), falling back
// to the file's directory. LSP servers need this to resolve imports.
func workspaceRoot(file string) string {
	abs, _ := filepath.Abs(file)
	dir := filepath.Dir(abs)
	markers := []string{"go.mod", "package.json", "Cargo.toml", "pyproject.toml", "setup.py"}
	for {
		for _, m := range markers {
			if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(abs)
		}
		dir = parent
	}
}

func filepathToURI(path string) string {
	abs, _ := filepath.Abs(path)
	return "file://" + filepath.ToSlash(abs)
}

// lspPosition resolves a 1-indexed line (+ optional symbol substring) into an
// LSP 0-indexed position. If the symbol is given, the cursor lands on it.
func lspPosition(text, symbol string, line int) map[string]any {
	char := 0
	if symbol != "" {
		lines := strings.Split(text, "\n")
		if line-1 >= 0 && line-1 < len(lines) {
			if idx := strings.Index(lines[line-1], symbol); idx >= 0 {
				char = idx
			}
		}
	}
	return map[string]any{"line": line - 1, "character": char}
}

// lspPos is an LSP zero-indexed position.
type lspPos struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// lspLoc is a single location (definition/reference hit).
type lspLoc struct {
	URI   string `json:"uri"`
	Range struct {
		Start lspPos `json:"start"`
	} `json:"range"`
}

// lspEdit is one text edit inside a workspace edit.
type lspEdit struct {
	Range struct {
		Start struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"start"`
		End struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"end"`
	} `json:"range"`
	NewText string `json:"newText"`
}

// lspQuery starts a short-lived language server, opens the file, issues one
// request, and returns the raw JSON result. Errors hint at missing servers.
func lspQuery(ctx context.Context, file, symbol string, line int, method string, extra map[string]any) (json.RawMessage, error) {
	ext := filepath.Ext(file)
	bin, args, ok := lspServerFor(ext)
	if !ok {
		return nil, fmt.Errorf("no language server for %q — supported: .go/.py/.ts/.js/.rs", ext)
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("%s not installed — for Go run: go install golang.org/x/tools/gopls@latest; for Python: pip install pyright; for TS: npm i -g typescript-language-server typescript", bin)
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", bin, err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	text, _ := os.ReadFile(file)
	root := workspaceRoot(file)
	uri := filepathToURI(file)

	nextID := int64(0)
	send := func(method string, params any) int64 {
		nextID++
		body := map[string]any{"jsonrpc": "2.0", "id": nextID, "method": method}
		if params != nil {
			body["params"] = params
		}
		data, _ := json.Marshal(body)
		fmt.Fprintf(stdin, "Content-Length: %d\r\n\r\n%s", len(data), data)
		return nextID
	}
	notify := func(method string, params any) {
		body := map[string]any{"jsonrpc": "2.0", "method": method}
		if params != nil {
			body["params"] = params
		}
		data, _ := json.Marshal(body)
		fmt.Fprintf(stdin, "Content-Length: %d\r\n\r\n%s", len(data), data)
	}

	send("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      filepathToURI(root),
		"capabilities": map[string]any{},
	})
	notify("initialized", map[string]any{})
	notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": languageID(ext),
			"version":    1,
			"text":       string(text),
		},
	})

	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     lspPosition(string(text), symbol, line),
	}
	for k, v := range extra {
		params[k] = v
	}

	reqID := send(method, params)

	reader := bufio.NewReader(stdout)
	for {
		payload, err := readLSPMessage(reader)
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("%s closed unexpectedly (server error — try `%s --version` to confirm it runs)", bin, bin)
			}
			return nil, err
		}
		var env struct {
			ID    *int64          `json:"id"`
			Error *json.RawMessage `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(payload, &env); err != nil {
			continue
		}
		if env.ID != nil && *env.ID == reqID {
			if env.Error != nil {
				return nil, fmt.Errorf("lsp error: %s", string(*env.Error))
			}
			return env.Result, nil
		}
	}
}

// readLSPMessage reads one Content-Length-framed JSON message.
func readLSPMessage(r *bufio.Reader) ([]byte, error) {
	var length int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			length, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:")))
		}
	}
	if length <= 0 {
		return nil, io.EOF
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// formatLocs renders []lspLoc as "path:line:col" lines.
func formatLocs(raw json.RawMessage) (string, error) {
	var locs []lspLoc
	if err := json.Unmarshal(raw, &locs); err != nil {
		return "", err
	}
	if len(locs) == 0 {
		return "(no results)", nil
	}
	var b strings.Builder
	for _, l := range locs {
		p := strings.TrimPrefix(l.URI, "file://")
		b.WriteString(fmt.Sprintf("%s:%d:%d\n", p, l.Range.Start.Line+1, l.Range.Start.Character+1))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// applyRename applies a rename workspace edit in-process, then writes back each
// changed file. Returns a human summary of the changes.
func applyRename(raw json.RawMessage, newName string) (string, error) {
	var we struct {
		Changes map[string][]lspEdit `json:"changes"`
	}
	if err := json.Unmarshal(raw, &we); err != nil {
		return "", err
	}
	if len(we.Changes) == 0 {
		return "(no changes — symbol not found or rename produced nothing)", nil
	}
	var b strings.Builder
	files := 0
	edits := 0
	for uri, es := range we.Changes {
		p := strings.TrimPrefix(uri, "file://")
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := bytes.Split(data, []byte("\n"))
		// Apply edits bottom-up so earlier offsets stay valid.
		for i := len(es) - 1; i >= 0; i-- {
			e := es[i]
			sl, sc := e.Range.Start.Line, e.Range.Start.Character
			el, ec := e.Range.End.Line, e.Range.End.Character
			if sl >= len(lines) || sl != el {
				continue // single-line edits only for v1
			}
			line := lines[sl]
			if sc > len(line) || ec > len(line) {
				continue
			}
			lines[sl] = append(line[:sc], append([]byte(e.NewText), line[ec:]...)...)
			edits++
		}
		if err := os.WriteFile(p, bytes.Join(lines, []byte("\n")), 0o644); err == nil {
			files++
			b.WriteString(fmt.Sprintf("updated %s\n", p))
		}
	}
	if files == 0 {
		return "(rename produced no applicable edits)", nil
	}
	b.WriteString(fmt.Sprintf("renamed %d occurrence(s) across %d file(s) to %q", edits, files, newName))
	return strings.TrimRight(b.String(), "\n"), nil
}

// registerLSP adds the lsp tool (definition, references, rename, diagnostics)
// to the registry. Symbol-aware code intelligence, like OMP's lsp tool.
func registerLSP(r *Registry) {
	r.Register(&Tool{
		Name: "lsp",
		Description: "Symbol-aware code intelligence: find where a symbol is defined, who references it, rename it across all files, or get real diagnostics. Use this instead of text search for cross-file work — it follows imports and re-exports accurately.",
		Parameters: mustJSON(`{"type":"object","properties":{"action":{"type":"string","enum":["definition","references","rename","diagnostics"],"description":"Which query to run"},"file":{"type":"string","description":"File path containing the symbol"},"line":{"type":"integer","description":"Line number (1-indexed) of the symbol"},"symbol":{"type":"string","description":"Substring on that line to pinpoint the symbol"},"new_name":{"type":"string","description":"New name (rename only)"}},"required":["action","file","line"]}`),
		Exec: func(ctx context.Context, args map[string]any) Result {
			return lspExec(ctx, args)
		},
	})
}

func lspExec(ctx context.Context, args map[string]any) Result {
	action, _ := args["action"].(string)
	file, _ := args["file"].(string)
	symbol, _ := args["symbol"].(string)
	newName, _ := args["new_name"].(string)
	lineF, _ := args["line"].(float64)
	line := int(lineF)

	if file == "" || line <= 0 {
		return Result{Output: "need file path and valid line number", IsError: true}
	}

	switch action {
	case "definition":
		raw, err := lspQuery(ctx, file, symbol, line, "textDocument/definition", nil)
		if err != nil {
			return Result{Output: err.Error(), IsError: true}
		}
		out, err := formatLocs(raw)
		if err != nil {
			return Result{Output: err.Error(), IsError: true}
		}
		return Result{Output: out}
	case "references":
		raw, err := lspQuery(ctx, file, symbol, line, "textDocument/references", map[string]any{
			"context": map[string]any{"includeDeclaration": false},
		})
		if err != nil {
			return Result{Output: err.Error(), IsError: true}
		}
		out, err := formatLocs(raw)
		if err != nil {
			return Result{Output: err.Error(), IsError: true}
		}
		return Result{Output: out}
	case "rename":
		if newName == "" {
			return Result{Output: "rename requires new_name", IsError: true}
		}
		raw, err := lspQuery(ctx, file, symbol, line, "textDocument/rename", map[string]any{
			"newName": newName,
		})
		if err != nil {
			return Result{Output: err.Error(), IsError: true}
		}
		out, err := applyRename(raw, newName)
		if err != nil {
			return Result{Output: err.Error(), IsError: true}
		}
		return Result{Output: out}
	case "diagnostics":
		out, err := lspDiagnostics(ctx, file)
		if err != nil {
			return Result{Output: err.Error(), IsError: true}
		}
		return Result{Output: out}
	default:
		return Result{Output: fmt.Sprintf("unknown lsp action: %s — use definition/references/rename/diagnostics", action), IsError: true}
	}
}

// lspDiagnostics starts an LSP server, opens the file, and collects
// publishDiagnostics notifications for up to 10s.
func lspDiagnostics(ctx context.Context, file string) (string, error) {
	ext := filepath.Ext(file)
	bin, args, ok := lspServerFor(ext)
	if !ok {
		return "no language server for this file", nil
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Sprintf("%s not installed — diagnostics unavailable", bin), nil
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	defer func() { _ = cmd.Process.Kill() }()

	text, _ := os.ReadFile(file)
	root := workspaceRoot(file)
	uri := filepathToURI(file)

	write := func(method string, params any) {
		body := map[string]any{"jsonrpc": "2.0", "method": method}
		if params != nil {
			body["params"] = params
		}
		data, _ := json.Marshal(body)
		fmt.Fprintf(stdin, "Content-Length: %d\r\n\r\n%s", len(data), data)
	}

	write("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      filepathToURI(root),
		"capabilities": map[string]any{},
	})
	write("initialized", map[string]any{})
	write("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": languageID(ext),
			"version":    1,
			"text":       string(text),
		},
	})

	reader := bufio.NewReader(stdout)
	var diags []string

	// Read until the server goes quiet. `publishDiagnostics` arrives soon after
	// didOpen; there is no response to correlate, so poll with a short idle
	// timeout: stop once 1.5s passes with no new message.
	type msg struct {
		method string
		params json.RawMessage
	}
	msgCh := make(chan msg, 32)
	go func() {
		for {
			payload, err := readLSPMessage(reader)
			if err != nil {
				close(msgCh)
				return
			}
			var note struct {
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if json.Unmarshal(payload, &note) == nil && note.Method != "" {
				msgCh <- msg{note.Method, note.Params}
			}
		}
	}()

	idle := time.NewTimer(1500 * time.Millisecond)
	for {
		select {
		case m, ok := <-msgCh:
			if !ok {
				goto done
			}
			idle.Reset(1500 * time.Millisecond)
			if m.method == "textDocument/publishDiagnostics" {
				var pd struct {
					URI         string `json:"uri"`
					Diagnostics []struct {
						Range struct {
							Start lspPos `json:"start"`
							End   lspPos `json:"end"`
						} `json:"range"`
						Severity int    `json:"severity"`
						Message  string `json:"message"`
					} `json:"diagnostics"`
				}
				if json.Unmarshal(m.params, &pd) == nil && pd.URI == uri {
					for _, d := range pd.Diagnostics {
						sev := "WARN"
						if d.Severity == 1 {
							sev = "ERROR"
						} else if d.Severity == 3 {
							sev = "INFO"
						}
						diags = append(diags, fmt.Sprintf("%s:%d:%d: %s: %s",
							strings.TrimPrefix(pd.URI, "file://"),
							d.Range.Start.Line+1, d.Range.Start.Character+1,
							sev, d.Message))
					}
				}
			}
		case <-idle.C:
			goto done
		}
	}
done:
	if len(diags) == 0 {
		return "(no diagnostics)", nil
	}
	return strings.Join(diags, "\n"), nil
}
