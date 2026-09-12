package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config mirrors Hiroto's design: settings in config.yaml, secrets in .env.
type Config struct {
	Model struct {
		BaseURL   string   `yaml:"base_url"`
		Name      string   `yaml:"model"`
		APIKeyEnv string   `yaml:"api_key"`   // literal key or ${ENV_NAME}
		Available []string `yaml:"available"` // cached model list from /v1/models
	} `yaml:"model"`
	Agent struct {
		MaxTurns          int    `yaml:"max_turns"`
		SystemExtra       string `yaml:"system_prompt_extra"`
		TermTimeout       int    `yaml:"terminal_timeout"`
		CompressBudget    int    `yaml:"compression_budget_tokens"` // 0 = off; default 25000
		CompressKeepTurns int    `yaml:"compression_keep_turns"`    // default 6
	} `yaml:"agent"`
	Skills struct {
		Dirs []string `yaml:"dirs"` // extra skill dirs; ~/.hiroto/skills always loaded
	} `yaml:"skills"`
	// Allow lists tools the agent may run WITHOUT interactive approval
	// (e.g. ["patch", "write_file", "terminal"]). Tools NOT in this list that
	// are classified risky are rejected in headless/API/one-shot mode. Empty =
	// prompt-mode default. Use --dangerously-skip-permissions to bypass all.
	Allow []string `yaml:"allow"`
	API   struct {
		Port int `yaml:"port"` // API server port (default 20129)
	} `yaml:"api"`
	Plugins struct {
		Dirs []string `yaml:"dirs"` // plugin directories
	} `yaml:"plugins"`
	MCP struct {
		Servers []MCPServerConfig `yaml:"servers"`
	} `yaml:"mcp"`
}

// MCPServerConfig describes one MCP server to connect to.
type MCPServerConfig struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

func HomeDir() string {
	if h := os.Getenv("HIROTO_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hiroto")
}

// Load reads ~/.hiroto/config.yaml, applying sensible defaults.
func Load() *Config {
	c := &Config{}
	c.Model.BaseURL = "http://localhost:20128/v1"
	c.Model.Name = ""
	c.Model.APIKeyEnv = ""
	c.Agent.MaxTurns = 40
	c.Agent.CompressBudget = 25000
	c.Agent.CompressKeepTurns = 6
	c.Agent.TermTimeout = 180

	path := filepath.Join(HomeDir(), "config.yaml")
	if data, err := os.ReadFile(path); err == nil {
		_ = yaml.Unmarshal(data, c)
	}
	if c.Agent.TermTimeout <= 0 {
		c.Agent.TermTimeout = 180
	}
	if c.Agent.MaxTurns <= 0 {
		c.Agent.MaxTurns = 40
	}
	return c
}

// ValidateModel errors when no model is configured. Called by every
// LLM-bound entry point so a fresh install fails with an actionable
// message instead of a cryptic provider error mid-session.
func (c *Config) ValidateModel() error {
	if strings.TrimSpace(c.Model.Name) == "" {
		return fmt.Errorf("no model configured — set model: in %s (or run: hiroto --set-model <name>)", filepath.Join(HomeDir(), "config.yaml"))
	}
	return nil
}

// APIKey resolves ${ENV} references in config against the environment and ~/.hiroto/.env.
func (c *Config) APIKey() string {
	return resolveEnv(c.Model.APIKeyEnv)
}

// resolveEnv expands a ${VAR} reference against the process env, then ~/.hiroto/.env.
// A non-${...} value (e.g. a literal key) is returned unchanged.
func resolveEnv(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
		envName := v[2 : len(v)-1]
		if val := os.Getenv(envName); val != "" {
			return val
		}
		return dotEnvValue(HomeDir(), envName)
	}
	return v
}

// dotEnvValue does a minimal KEY=VALUE lookup in <dir>/.env (no export, quotes stripped).
func dotEnvValue(dir, key string) string {
	f, err := os.Open(filepath.Join(dir, ".env"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, val, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == key {
			val = strings.TrimSpace(val)
			return strings.Trim(val, `"'`)
		}
	}
	return ""
}
