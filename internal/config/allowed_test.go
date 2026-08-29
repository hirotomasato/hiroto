package config

import (
	"os"
	"testing"
)

// AllowedUsers merges config.yaml IDs with the env var, dedupes, and drops junk.
func TestAllowedUsers(t *testing.T) {
	c := &Config{}
	c.Gateway.AllowedUsers = []int64{111, 222}

	// No env → just the config IDs.
	os.Unsetenv("HIROTO_TELEGRAM_ALLOWED_USERS")
	// Point HERMES/HIROTO home at an empty temp dir so no stray .env leaks in.
	dir := t.TempDir()
	t.Setenv("HIROTO_HOME", dir)
	got := c.AllowedUsers()
	if len(got) != 2 {
		t.Fatalf("config-only: got %v, want [111 222]", got)
	}

	// Env adds more, with dedupe and whitespace/junk tolerance.
	t.Setenv("HIROTO_TELEGRAM_ALLOWED_USERS", "222, 333 , , notanumber, 444")
	got = c.AllowedUsers()
	set := map[int64]bool{}
	for _, id := range got {
		set[id] = true
	}
	for _, want := range []int64{111, 222, 333, 444} {
		if !set[want] {
			t.Errorf("missing %d in %v", want, got)
		}
	}
	if len(got) != 4 {
		t.Errorf("expected 4 unique IDs, got %v", got)
	}
}
