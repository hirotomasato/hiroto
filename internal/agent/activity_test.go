package agent

import "testing"

// ActivityLabel is shared by the TUI and the Telegram gateway, so both narrate
// tool activity with the same natural phrases.
func TestActivityLabel(t *testing.T) {
	cases := []struct {
		name, args, want string
	}{
		{"read_file", `{"path":"cmd/hiroto/main.go"}`, "Reading main.go"},
		{"write_file", `{"path":"/tmp/out.txt"}`, "Writing out.txt"},
		{"patch", `{"path":"internal/agent/agent.go"}`, "Editing agent.go"},
		{"terminal", `{"command":"go build ./..."}`, "Running go build ./..."},
		{"search_files", `{"pattern":"banner","target":"files"}`, "Finding files banner"},
		{"web_extract", `{"urls":["https://example.com"]}`, "Fetching https://example.com"},
		{"todo", `{}`, "Updating todos"},
		{"read_file", ``, "Reading"},
		{"mystery", `{"path":"x"}`, "mystery x"},
	}
	for _, c := range cases {
		if got := ActivityLabel(c.name, c.args); got != c.want {
			t.Errorf("ActivityLabel(%q,%q) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}

// A model-supplied "activity" arg wins over the derived label.
func TestActivityLabelModelNarration(t *testing.T) {
	if got := ActivityLabel("read_file", `{"path":"x.go","activity":"Checking the banner"}`); got != "Checking the banner" {
		t.Errorf("model narration lost: %q", got)
	}
	if got := ActivityLabel("read_file", `{"path":"x.go","activity":"  "}`); got != "Reading x.go" {
		t.Errorf("blank activity should fall back: %q", got)
	}
}

// Malformed JSON must not panic — fall back to the bare verb.
func TestActivityLabelBadJSON(t *testing.T) {
	if got := ActivityLabel("terminal", `{bad`); got != "Running" {
		t.Errorf("bad JSON: %q", got)
	}
}
