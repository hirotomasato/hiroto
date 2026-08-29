// Package plugin loads external plugins and MCP servers.
// Plugins add tools via manifest + script; MCP servers expose tools via JSON-RPC.
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest describes a plugin directory.
type Manifest struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
	Tools       []struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Command     string `yaml:"command"` // e.g. "python3 script.py"
		Parameters  map[string]any `yaml:"parameters,omitempty"`
	} `yaml:"tools"`
}

// ToolDef is a plugin-provided tool ready for the agent registry.
type ToolDef struct {
	Name        string
	Description string
	Parameters  map[string]any
	Exec        func(ctx context.Context, args map[string]any) (output string, isError bool)
}

// LoadPlugins scans plugin directories for plugin.yaml manifests.
func LoadPlugins(dirs []string) []ToolDef {
	var tools []ToolDef
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			manifestPath := filepath.Join(dir, e.Name(), "plugin.yaml")
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				continue
			}
			var m Manifest
			if err := yaml.Unmarshal(data, &m); err != nil {
				continue
			}
			pluginDir := filepath.Join(dir, e.Name())
			for _, t := range m.Tools {
				cmd := t.Command
				params := t.Parameters
				if params == nil {
					params = map[string]any{
						"type":       "object",
						"properties": map[string]any{},
						"required":   []string{},
					}
				}
				tools = append(tools, ToolDef{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  params,
					Exec: func(ctx context.Context, args map[string]any) (string, bool) {
						return runPluginTool(ctx, pluginDir, cmd, args)
					},
				})
			}
		}
	}
	return tools
}

func runPluginTool(ctx context.Context, dir, command string, args map[string]any) (string, bool) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "plugin: empty command", true
	}
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	input, _ := json.Marshal(args)
	cmd.Stdin = strings.NewReader(string(input))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("plugin error: %v\n%s", err, string(out)), true
	}
	return strings.TrimSpace(string(out)), false
}