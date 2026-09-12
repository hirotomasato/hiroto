package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPluginsEmptyDir(t *testing.T) {
	if got := LoadPlugins([]string{t.TempDir()}); len(got) != 0 {
		t.Errorf("expected 0 tools, got %d", len(got))
	}
}

func TestLoadPluginsMissingManifest(t *testing.T) {
	dir := t.TempDir()
	// Directory exists but no plugin.yaml
	os.MkdirAll(filepath.Join(dir, "myplugin"), 0o755)
	if got := LoadPlugins([]string{dir}); len(got) != 0 {
		t.Errorf("expected 0 tools, got %d", len(got))
	}
}

func TestLoadPluginsValidManifest(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "myplugin")
	os.MkdirAll(pluginDir, 0o755)

	manifest := `
name: myplugin
version: "1.0"
tools:
  - name: greet
    description: say hello
    command: echo hello
`
	os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644)

	tools := LoadPlugins([]string{dir})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "greet" {
		t.Errorf("Name = %q, want %q", tools[0].Name, "greet")
	}
}

func TestLoadPluginsBadYAML(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "bad")
	os.MkdirAll(pluginDir, 0o755)
	os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte("{{{"), 0o644)
	if got := LoadPlugins([]string{dir}); len(got) != 0 {
		t.Errorf("expected 0 tools from bad YAML, got %d", len(got))
	}
}

func TestRunPluginToolEmptyCommand(t *testing.T) {
	out, isErr := runPluginTool(context.Background(), t.TempDir(), "", nil)
	if !isErr {
		t.Error("expected error for empty command")
	}
	if !strings.Contains(out, "empty command") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestSanitizeEnv(t *testing.T) {
	t.Setenv("MY_API_KEY", "sk-secret")
	t.Setenv("MY_TOKEN", "tok-secret")
	t.Setenv("NORMAL_VAR", "visible")
	t.Setenv("PATH", "/usr/bin")

	env := sanitizeEnv()
	joined := strings.Join(env, "\n")

	for _, secret := range []string{"sk-secret", "tok-secret"} {
		if strings.Contains(joined, secret) {
			t.Errorf("secret %q leaked in env", secret)
		}
	}
	if !strings.Contains(joined, "visible") {
		t.Error("NORMAL_VAR missing from env")
	}
	if !strings.Contains(joined, "/usr/bin") {
		t.Error("PATH missing from env")
	}
}
