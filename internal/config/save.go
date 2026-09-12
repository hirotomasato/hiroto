package config

import (
	"os"
	"path/filepath"
	"regexp"
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
