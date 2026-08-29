package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func registerNativeTools(r *Registry) {
	// secret_scan — native secret scanner
	r.Register(&Tool{
		Name:        "secret_scan",
		Description: "Scan files and directories for secrets, API keys, tokens, passwords, and credentials using regex patterns. Use for security auditing.",
		Parameters:  mustJSON(`{"type":"object","properties":{"path":{"type":"string","description":"File or directory path to scan"},"pattern":{"type":"string","description":"Optional custom regex pattern"}},"required":["path"]}`),
		Exec:        secretScanExec,
	})

	// search_knowledge — search the skills/knowledge base
	r.Register(&Tool{
		Name:        "search_knowledge",
		Description: "Search Hiroto's skill and knowledge base for relevant techniques, payloads, or methodologies. Returns matching skill names and snippets.",
		Parameters:  mustJSON(`{"type":"object","properties":{"query":{"type":"string","description":"Search query (keyword, technique, vulnerability type)"},"limit":{"type":"integer","description":"Max results (default 10)"}},"required":["query"]}`),
		Exec:        searchKnowledgeExec,
	})

	// aggregate_reports — merge and deduplicate findings
	r.Register(&Tool{
		Name:        "aggregate_reports",
		Description: "Merge, deduplicate, and summarize multiple report files into a single consolidated report. Handles JSON, text, and markdown formats.",
		Parameters:  mustJSON(`{"type":"object","properties":{"paths":{"type":"array","items":{"type":"string"},"description":"List of report file paths to aggregate"},"output":{"type":"string","description":"Output file path for the consolidated report"}},"required":["paths","output"]}`),
		Exec:        aggregateReportsExec,
	})

	// smart_pipe — chain commands with data transformation
	r.Register(&Tool{
		Name:        "smart_pipe",
		Description: "Chain multiple shell commands with smart data transformation between them. Each command's stdout becomes the next command's stdin. Use for complex data processing pipelines.",
		Parameters:  mustJSON(`{"type":"object","properties":{"commands":{"type":"array","items":{"type":"string"},"description":"Ordered list of shell commands to pipe together"}},"required":["commands"]}`),
		Exec:        smartPipeExec,
	})
}

// ---- secret_scan ----
var secretPatterns = []string{
	`(?i)(api[_-]?key|apikey|api[_-]?secret|secret[_-]?key)\s*[:=]\s*['"]?[a-zA-Z0-9_\-]{20,}['"]?`,
	`(?i)(password|passwd|pwd)\s*[:=]\s*['"][^'"]+['"]`,
	`(?i)(token|auth[_-]?token|bearer)\s*[:=]\s*['"]?[a-zA-Z0-9_\-\.]{20,}['"]?`,
	`[a-zA-Z0-9+/]{40,}={0,2}`, // base64 potential secrets
	`(?i)(private[_-]?key|privkey|ssh[_-]?key)`,
	`(?i)(aws[_-]?access[_-]?key|aws[_-]?secret|AKIA[0-9A-Z]{16})`,
	`(?i)(github[_-]?token|gh[_-]?token|ghp_[0-9a-zA-Z]{36})`,
	`(?i)(jwt|json[_-]?web[_-]?token)\s*[:=]\s*['"]?eyJ`,
}

func secretScanExec(ctx context.Context, args map[string]any) Result {
	path, _ := args["path"].(string)
	customPattern, _ := args["pattern"].(string)
	if path == "" {
		return Result{Output: "missing path", IsError: true}
	}
	patterns := secretPatterns
	if customPattern != "" {
		patterns = append(patterns, customPattern)
	}
	var findings []string
	for _, pat := range patterns {
		cmd := exec.CommandContext(ctx, "grep", "-rnI", "--include=*", "-E", pat, path)
		out, _ := cmd.CombinedOutput()
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line != "" {
				// Redact the actual secret value
				parts := strings.SplitN(line, ":", 3)
				if len(parts) >= 3 {
					line = parts[0] + ":" + parts[1] + ": [REDACTED]"
				}
				findings = append(findings, line)
			}
		}
	}
	if len(findings) == 0 {
		return Result{Output: fmt.Sprintf("no secrets found in %s", path)}
	}
	if len(findings) > 50 {
		findings = findings[:50]
		findings = append(findings, fmt.Sprintf("... and %d more", len(findings)-50))
	}
	return Result{Output: fmt.Sprintf("found %d potential secrets:\n%s", len(findings), strings.Join(findings, "\n"))}
}

// ---- search_knowledge ----
func searchKnowledgeExec(ctx context.Context, args map[string]any) Result {
	query, _ := args["query"].(string)
	if query == "" {
		return Result{Output: "missing query", IsError: true}
	}
	limit := 10
	if l, ok := toInt(args["limit"]); ok && l > 0 {
		limit = l
	}
	home := os.Getenv("HIROTO_HOME")
	if home == "" {
		homeDir, _ := os.UserHomeDir()
		home = filepath.Join(homeDir, ".hiroto")
	}
	skillsDir := filepath.Join(home, "skills")
	// Search SKILL.md files for the query
	cmd := exec.CommandContext(ctx, "grep", "-rli", "--include=SKILL.md", query, skillsDir)
	out, _ := cmd.CombinedOutput()
	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	var results []string
	for i, f := range files {
		if f == "" || i >= limit {
			break
		}
		// Extract skill name from path
		rel, _ := filepath.Rel(skillsDir, f)
		name := filepath.Dir(rel)
		// Get description
		descCmd := exec.CommandContext(ctx, "grep", "-m1", "^description:", f)
		descOut, _ := descCmd.CombinedOutput()
		desc := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(descOut)), "description:"))
		results = append(results, fmt.Sprintf("- %s: %s", name, desc))
	}
	if len(results) == 0 {
		return Result{Output: fmt.Sprintf("no knowledge found for: %s", query)}
	}
	return Result{Output: fmt.Sprintf("knowledge results for \"%s\":\n%s", query, strings.Join(results, "\n"))}
}

// ---- aggregate_reports ----
func aggregateReportsExec(ctx context.Context, args map[string]any) Result {
	pathsRaw, _ := args["paths"].([]any)
	output, _ := args["output"].(string)
	if len(pathsRaw) == 0 || output == "" {
		return Result{Output: "missing paths or output", IsError: true}
	}
	var allLines []string
	seen := make(map[string]bool)
	for _, p := range pathsRaw {
		path, ok := p.(string)
		if !ok {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || seen[line] {
				continue
			}
			seen[line] = true
			allLines = append(allLines, line)
		}
	}
	content := fmt.Sprintf("# Aggregated Report\n\n%d unique findings from %d sources\n\n%s",
		len(allLines), len(pathsRaw), strings.Join(allLines, "\n"))
	os.MkdirAll(filepath.Dir(output), 0755)
	if err := os.WriteFile(output, []byte(content), 0644); err != nil {
		return Result{Output: fmt.Sprintf("write error: %v", err), IsError: true}
	}
	return Result{Output: fmt.Sprintf("aggregated %d findings from %d reports → %s", len(allLines), len(pathsRaw), output)}
}

// ---- smart_pipe ----
func smartPipeExec(ctx context.Context, args map[string]any) Result {
	cmdsRaw, _ := args["commands"].([]any)
	if len(cmdsRaw) == 0 {
		return Result{Output: "missing commands", IsError: true}
	}
	var cmds []string
	for _, c := range cmdsRaw {
		if s, ok := c.(string); ok {
			cmds = append(cmds, s)
		}
	}
	if len(cmds) == 0 {
		return Result{Output: "no valid commands", IsError: true}
	}
	// Chain commands with pipes
	pipeline := strings.Join(cmds, " | ")
	cmd := exec.CommandContext(ctx, shellCmd(), shellFlag(), pipeline)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{Output: fmt.Sprintf("pipeline error: %v\n%s", err, string(out)), IsError: true}
	}
	result := strings.TrimSpace(string(out))
	if len(result) > 10000 {
		result = result[:10000] + "\n... (truncated)"
	}
	return Result{Output: result}
}
