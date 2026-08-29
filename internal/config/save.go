package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SaveModel persists the model choice into config.yaml, preserving the rest
// of the file (comments included) via a targeted line replacement.
func SaveModel(name string) {
	path := filepath.Join(HomeDir(), "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		// No config file: write a minimal one.
		content := "model:\n  base_url: http://localhost:20128/v1\n  model: " + name + "\n  api_key: ${HIROTO_API_KEY}\n"
		_ = os.WriteFile(path, []byte(content), 0o644)
		return
	}
	src := string(data)

	// Case 1: an indented "model:" line exists — replace it in place
	// (even when the value is identical) so we never duplicate the line.
	re := regexp.MustCompile(`(?m)^([ \t]+model:[ \t]*).*$`)
	if re.MatchString(src) {
		_ = os.WriteFile(path, []byte(re.ReplaceAllString(src, "${1}"+name)), 0o644)
		return
	}

	// Case 2: only a bare top-level "model:" exists — insert under it.
	reTop := regexp.MustCompile(`(?m)^model:[ \t]*$`)
	if reTop.MatchString(src) {
		_ = os.WriteFile(path, []byte(reTop.ReplaceAllString(src, "model:\n  model: "+name)), 0o644)
		return
	}

	// Case 3: no model key at all — leave the rest untouched and append.
	_ = os.WriteFile(path, []byte(src+"\nmodel:\n  model: "+name+"\n"), 0o644)
}

// dotEnvSet upserts KEY=VALUE in <dir>/.env, creating the file if needed.
// Used by the gateway setup wizard to persist the Token secret out of config.yaml.
func dotEnvSet(dir, key, value string) error {
	path := filepath.Join(dir, ".env")
	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	}
	prefix := key + "="
	found := false
	for i, line := range lines {
		if strings.TrimSpace(line) == key || strings.HasPrefix(strings.TrimSpace(line), prefix) {
			lines[i] = key + "=" + value
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, key+"="+value)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

// SaveGatewayToken persists the Telegram token into ~/.hiroto/.env and points
// config.yaml's gateway.telegram_token at it via ${HIROTO_TELEGRAM_TOKEN}.
func SaveGatewayToken(token string) error {
	dir := HomeDir()
	if err := dotEnvSet(dir, "HIROTO_TELEGRAM_TOKEN", token); err != nil {
		return err
	}
	// Ensure config.yaml references the env var.
	path := filepath.Join(dir, "config.yaml")
	ref := "${HIROTO_TELEGRAM_TOKEN}"
	data, err := os.ReadFile(path)
	if err != nil {
		// No config yet: write a minimal gateway block.
		content := "gateway:\n  telegram_token: " + ref + "\n"
		return os.WriteFile(path, []byte(content), 0o644)
	}
	src := string(data)
	// replace an existing telegram_token line (comment-preserving)
	re := regexp.MustCompile(`(?m)^([ 	]+telegram_token:[ 	]*).*$`)
	if re.MatchString(src) {
		return os.WriteFile(path, []byte(re.ReplaceAllString(src, "${1}"+ref)), 0o644)
	}
	// gateway: block exists but no token line -> insert under it
	reTop := regexp.MustCompile(`(?m)^gateway:[ 	]*$`)
	if reTop.MatchString(src) {
		return os.WriteFile(path, []byte(reTop.ReplaceAllString(src, "gateway:\n  telegram_token: "+ref)), 0o644)
	}
	// no gateway block -> append
	return os.WriteFile(path, []byte(src+"\ngateway:\n  telegram_token: "+ref+"\n"), 0o644)
}
