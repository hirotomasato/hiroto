package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// astgrepBin returns the first available ast-grep binary, preferring the
// newer "ast-grep" over the old "sg" (which shadows shadow-utils on Linux).
func astgrepBin() string {
	for _, b := range []string{"ast-grep", "sg"} {
		if p, err := exec.LookPath(b); err == nil {
			// On Linux, /usr/bin/sg is shadow-utils, not ast-grep.
			// Quick heuristic: ast-grep's "sg" responds to --help with "ast-grep".
			if b == "sg" && strings.Contains(p, "/usr/bin/sg") {
				continue
			}
			return b
		}
	}
	return ""
}

func registerASTEdit(r *Registry) {
	r.Register(&Tool{
		Name:        "ast_edit",
		Description: "Structural AST-aware rewrite via ast-grep. Use for codemods where text patch is unsafe. Patterns match AST structure, not text. Metavariables $A/$B/$_ substitute into the rewrite. Safe for cross-file refactors. Requires ast-grep installed (cargo install ast-grep).",
		Parameters: mustJSON(`{"type":"object","properties":{"pattern":{"type":"string","description":"AST pattern with $metavariables (e.g. 'console.log($$$ARGS)')"},"rewrite":{"type":"string","description":"Replacement template (e.g. '' to delete)"},"files":{"type":"string","description":"File path or glob to rewrite"},"lang":{"type":"string","description":"Language override (auto-detected from extension if omitted)"}},"required":["pattern","rewrite","files"]}`),
		Exec: func(ctx context.Context, args map[string]any) Result {
			return astEditExec(ctx, args)
		},
	})
}

var safePathRe = regexp.MustCompile(`^[a-zA-Z0-9_./*-]+$`)

func astEditExec(ctx context.Context, args map[string]any) Result {
	pattern, _ := args["pattern"].(string)
	rewrite, _ := args["rewrite"].(string)
	files, _ := args["files"].(string)
	lang, _ := args["lang"].(string)

	bin := astgrepBin()
	if bin == "" {
		return Result{Output: "ast-grep not installed — run: cargo install ast-grep", IsError: true}
	}
	if pattern == "" || files == "" {
		return Result{Output: "need pattern and files", IsError: true}
	}
	if !safePathRe.MatchString(files) && !strings.Contains(files, "**") {
		return Result{Output: "files path contains unsafe characters", IsError: true}
	}

	// Auto-detect language from the first matched file extension if not given.
	if lang == "" {
		lang = langFromExt(files)
		if lang == "" {
			return Result{Output: "could not detect language — pass lang explicitly", IsError: true}
		}
	}

	cmdArgs := []string{"run", "--pattern", pattern, "--rewrite", rewrite, "--lang", lang, "--update-all", files}
	if rewrite == "" {
		// Empty rewrite = delete the matched node. ast-grep treats "" as "replace with nothing".
	}
	cmd := exec.CommandContext(ctx, bin, cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		return Result{Output: fmt.Sprintf("ast-grep error: %v\n%s", err, stderr.String()), IsError: true}
	}
	if out == "" {
		out = "(no changes — pattern did not match)"
	}
	return Result{Output: out}
}

func langFromExt(path string) string {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	switch ext {
	case "go":
		return "go"
	case "py", "pyi":
		return "python"
	case "ts":
		return "typescript"
	case "tsx":
		return "tsx"
	case "js":
		return "javascript"
	case "jsx":
		return "javascript"
	case "rs":
		return "rust"
	case "c", "h":
		return "c"
	case "cpp", "cc", "cxx", "hpp":
		return "cpp"
	default:
		return ""
	}
}