package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("HIROTO_HOME", t.TempDir())
	c := Load()
	if c.Agent.MaxTurns != 40 {
		t.Errorf("MaxTurns = %d, want 40", c.Agent.MaxTurns)
	}
	if c.Agent.TermTimeout != 180 {
		t.Errorf("TermTimeout = %d, want 180", c.Agent.TermTimeout)
	}
	if c.Model.BaseURL == "" {
		t.Error("BaseURL empty")
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIROTO_HOME", dir)
	yaml := `
model:
  base_url: https://api.example.com/v1
  model: test-model
  api_key: ${MY_KEY}
agent:
  max_turns: 10
`
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644)
	c := Load()
	if c.Model.Name != "test-model" {
		t.Errorf("Model.Name = %q, want %q", c.Model.Name, "test-model")
	}
	if c.Agent.MaxTurns != 10 {
		t.Errorf("MaxTurns = %d, want 10", c.Agent.MaxTurns)
	}
}

func TestResolveEnvFromProcess(t *testing.T) {
	t.Setenv("TEST_HIROTO_KEY", "sk-test-123")
	got := resolveEnv("${TEST_HIROTO_KEY}")
	if got != "sk-test-123" {
		t.Errorf("resolveEnv = %q, want %q", got, "sk-test-123")
	}
}

func TestResolveEnvFromDotEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIROTO_HOME", dir)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("MY_SECRET=dotenv-val\n"), 0o644)
	got := resolveEnv("${MY_SECRET}")
	if got != "dotenv-val" {
		t.Errorf("resolveEnv = %q, want %q", got, "dotenv-val")
	}
}

func TestResolveEnvLiteral(t *testing.T) {
	got := resolveEnv("sk-literal-key")
	if got != "sk-literal-key" {
		t.Errorf("resolveEnv = %q, want literal", got)
	}
}

func TestResolveEnvMissing(t *testing.T) {
	t.Setenv("HIROTO_HOME", t.TempDir())
	got := resolveEnv("${NONEXISTENT_VAR_XYZ}")
	if got != "" {
		t.Errorf("resolveEnv = %q, want empty", got)
	}
}

func TestDotEnvStripsQuotes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte(`KEY="quoted"`+"\n"), 0o644)
	got := dotEnvValue(dir, "KEY")
	if got != "quoted" {
		t.Errorf("dotEnvValue = %q, want %q", got, "quoted")
	}
}
