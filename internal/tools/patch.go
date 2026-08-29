package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func registerPatch(r *Registry, opts Options) {
	r.Register(&Tool{
		Name:        "patch",
		Description: "Edit a file by replacing old_string with new_string (Hiroto-style targeted patch). Finds the first exact match and replaces it. Use for precise edits without overwriting the whole file. After patching, runs LSP diagnostics on the file.",
		Parameters:  mustJSON(`{"type":"object","properties":{"path":{"type":"string","description":"File path to edit"},"old_string":{"type":"string","description":"Exact text to find and replace"},"new_string":{"type":"string","description":"Replacement text (use empty string to delete)"}},"required":["path","old_string","new_string"]}`),
		Exec: func(ctx context.Context, args map[string]any) Result {
			path, _ := args["path"].(string)
			oldStr, _ := args["old_string"].(string)
			newStr, _ := args["new_string"].(string)
			if path == "" || oldStr == "" {
				return Result{Output: "missing path or old_string", IsError: true}
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return Result{Output: fmt.Sprintf("cannot read %s: %v", path, err), IsError: true}
			}
			content := string(data)
			count := strings.Count(content, oldStr)
			if count == 0 {
				return Result{Output: fmt.Sprintf("old_string not found in %s", path), IsError: true}
			}
			if count > 1 {
				return Result{Output: fmt.Sprintf("old_string appears %d times in %s — use a more specific match with surrounding context", count, path), IsError: true}
			}
			autoCheckpoint(opts.Workdir)
			replaced := strings.Replace(content, oldStr, newStr, 1)
			if err := os.WriteFile(path, []byte(replaced), 0644); err != nil {
				return Result{Output: fmt.Sprintf("write error: %v", err), IsError: true}
			}
			lines := strings.Count(replaced, "\n") + 1
			out := fmt.Sprintf("patched %s (%d lines, 1 replacement)", path, lines)
			if diag := lspCheck(path); diag != "" {
				out += "\n" + diag
			}
			return Result{Output: out}
		},
	})
}