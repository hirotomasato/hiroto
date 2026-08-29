package gateway

import "testing"

// progressMode normalizes the config value to one of the three known modes.
func TestProgressMode(t *testing.T) {
	cases := map[string]string{
		"":        "all", // default
		"all":     "all",
		"new":     "new",
		"off":     "off",
		"bogus":   "all", // unknown falls back to all
		"verbose": "all",
	}
	for in, want := range cases {
		if got := (Options{ToolProgress: in}).progressMode(); got != want {
			t.Errorf("progressMode(%q) = %q, want %q", in, got, want)
		}
	}
}
