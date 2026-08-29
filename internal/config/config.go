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
	Gateway struct {
		TelegramToken string `yaml:"telegram_token"` // Bot API token (empty = off)
		// AllowedUsers is the list of Telegram numeric user IDs permitted to
		// use the bot. Empty = deny everyone (safe default for a bot with
		// terminal access). Can also be set via HIROTO_TELEGRAM_ALLOWED_USERS
		// (comma-separated) in ~/.hiroto/.env.
		AllowedUsers []int64 `yaml:"allowed_users"`
		// ToolProgress controls how much tool activity is streamed to chat:
		//   "all" (default) — every tool start/end as a breadcrumb line
		//   "new"           — only tool_start lines (quieter)
		//   "off"           — no breadcrumbs; only the final answer
		ToolProgress string `yaml:"tool_progress"`
		// CleanupProgress deletes the progress bubble once the final answer
		// lands (Hermes-style). Failed turns keep the bubble as a breadcrumb.
		CleanupProgress bool `yaml:"cleanup_progress"`
		// TypingIndicator shows the "typing…" action while the agent works.
		// Defaults to true when unset.
		TypingIndicator *bool `yaml:"typing_indicator"`
	} `yaml:"gateway"`
	API struct {
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
	return resolveEnv(c.Model.APIKeyEnv)
}

// GatewayToken resolves ${ENV} references for the Telegram bot token, mirroring
// APIKey so the secret can live in ~/.hiroto/.env instead of plaintext config.yaml.
func (c *Config) GatewayToken() string {
	return resolveEnv(c.Gateway.TelegramToken)
}

// AllowedUsers returns the Telegram user-ID allowlist, merging config.yaml's
// gateway.allowed_users with HIROTO_TELEGRAM_ALLOWED_USERS (comma-separated) in
// the environment / ~/.hiroto/.env. Duplicates are removed. An empty result
// means "deny everyone" — the safe default for a terminal-capable bot.
func (c *Config) AllowedUsers() []int64 {
	seen := map[int64]bool{}
	var out []int64
	add := func(id int64) {
		if id != 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range c.Gateway.AllowedUsers {
		add(id)
	}
	raw := os.Getenv("HIROTO_TELEGRAM_ALLOWED_USERS")
	if raw == "" {
		raw = dotEnvValue(HomeDir(), "HIROTO_TELEGRAM_ALLOWED_USERS")
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var id int64
		if _, err := fmt.Sscan(part, &id); err == nil {
			add(id)
		}
	}
	return out
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
