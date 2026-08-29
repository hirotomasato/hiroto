package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aymanbagabas/go-udiff"
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
			if d := unifiedDiff(path, content, replaced); d != "" {
				out += "\n" + d
			}
			if diag := lspCheck(path); diag != "" {
				out += "\n" + diag
			}
			return Result{Output: out}
		},
	})
}

// unifiedDiff returns a compact unified diff (3 lines of context) between two
// versions of a file, or "" when they're identical. The TUI colorizes the
// +/- lines; here we only produce the plain diff text.
func unifiedDiff(path, before, after string) string {
	if before == after {
		return ""
	}
	d := udiff.Unified(path, path, before, after)
	// Drop the redundant "--- path\n+++ path" header (the tool line already
	// names the file); keep the @@ hunks and +/- body.
	lines := strings.Split(strings.TrimRight(d, "\n"), "\n")
	var kept []string
	for _, ln := range lines {
		if strings.HasPrefix(ln, "--- ") || strings.HasPrefix(ln, "+++ ") {
			continue
		}
		kept = append(kept, ln)
	}
	// Cap very large diffs so a huge edit doesn't flood the transcript.
	const maxDiffLines = 60
	if len(kept) > maxDiffLines {
		kept = append(kept[:maxDiffLines], fmt.Sprintf("… (+%d more diff lines)", len(kept)-maxDiffLines))
	}
	return strings.Join(kept, "\n")
}
