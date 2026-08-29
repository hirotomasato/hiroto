package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config mirrors Hiroto's design: settings in config.yaml, secrets in .env.
type Config struct {
	Model struct {
		BaseURL   string `yaml:"base_url"`
		Name      string `yaml:"model"`
		APIKeyEnv string `yaml:"api_key"` // literal key or ${ENV_NAME}
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
	Gateway struct {
		TelegramToken string `yaml:"telegram_token"` // Bot API token (empty = off)
	} `yaml:"gateway"`
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
	c.Model.Name = "teamo/glm-5.3-flash-free"
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

// APIKey resolves ${ENV} references in config against the environment and ~/.hiroto/.env.
func (c *Config) APIKey() string {
	v := strings.TrimSpace(c.Model.APIKeyEnv)
	if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
		envName := v[2 : len(v)-1]
		if val := os.Getenv(envName); val != "" {
			return val
		}
		return dotEnvValue(HomeDir(), envName)
	}
	return v // literal key
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
