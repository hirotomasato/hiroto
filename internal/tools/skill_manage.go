package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func registerSkillManage(r *Registry) {
	r.Register(&Tool{
		Name:        "skill_manage",
		Description: "Create, edit, patch, or delete a skill (SKILL.md file). Actions: create, patch, delete. Skills are stored in ~/.hiroto/skills/<category>/<name>/SKILL.md. Use create for new skills, patch for targeted edits, delete to remove.",
		Parameters:  mustJSON(`{"type":"object","properties":{"action":{"type":"string","description":"One of: create, patch, delete"},"name":{"type":"string","description":"Skill name (lowercase, hyphens)"},"category":{"type":"string","description":"Optional category subdirectory"},"content":{"type":"string","description":"Full SKILL.md content (required for create)"},"old_string":{"type":"string","description":"Text to find (required for patch)"},"new_string":{"type":"string","description":"Replacement text (required for patch)"}},"required":["action","name"]}`),
		Exec:        skillManageExec,
	})
}

func skillManageExec(ctx context.Context, args map[string]any) Result {
	action, _ := args["action"].(string)
	name, _ := args["name"].(string)
	if name == "" {
		return Result{Output: "missing name", IsError: true}
	}
	home := os.Getenv("HIROTO_HOME")
	if home == "" {
		homeDir, _ := os.UserHomeDir()
		home = filepath.Join(homeDir, ".hiroto")
	}
	category, _ := args["category"].(string)
	dir := filepath.Join(home, "skills", category, name)
	path := filepath.Join(dir, "SKILL.md")

	switch action {
	case "create":
		content, _ := args["content"].(string)
		if content == "" {
			return Result{Output: "missing content for create", IsError: true}
		}
		os.MkdirAll(dir, 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return Result{Output: fmt.Sprintf("failed to create skill: %v", err), IsError: true}
		}
		return Result{Output: fmt.Sprintf("skill created: %s (%s)", name, path)}

	case "patch":
		oldStr, _ := args["old_string"].(string)
		newStr, _ := args["new_string"].(string)
		if oldStr == "" {
			return Result{Output: "missing old_string for patch", IsError: true}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return Result{Output: fmt.Sprintf("skill not found: %s (%v)", name, err), IsError: true}
		}
		content := string(data)
		count := strings.Count(content, oldStr)
		if count == 0 {
			return Result{Output: fmt.Sprintf("old_string not found in %s", name), IsError: true}
		}
		if count > 1 {
			return Result{Output: fmt.Sprintf("old_string appears %d times — use more context", count), IsError: true}
		}
		replaced := strings.Replace(content, oldStr, newStr, 1)
		if err := os.WriteFile(path, []byte(replaced), 0644); err != nil {
			return Result{Output: fmt.Sprintf("patch failed: %v", err), IsError: true}
		}
		return Result{Output: fmt.Sprintf("skill patched: %s (1 replacement)", name)}

	case "delete":
		if err := os.RemoveAll(dir); err != nil {
			return Result{Output: fmt.Sprintf("delete failed: %v", err), IsError: true}
		}
		return Result{Output: fmt.Sprintf("skill deleted: %s", name)}

	default:
		return Result{Output: fmt.Sprintf("unknown action: %s (use create/patch/delete)", action), IsError: true}
	}
}
