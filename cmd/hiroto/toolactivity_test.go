package main

import "testing"

// toolActivity turns a tool call + raw JSON args into a natural, Hermes-style
// activity phrase. These cases lock in the label each tool produces.
func TestToolActivity(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"read_file", `{"path":"cmd/hiroto/main.go"}`, "Reading main.go"},
		{"write_file", `{"path":"/tmp/out.txt"}`, "Writing out.txt"},
		{"patch", `{"path":"internal/agent/agent.go"}`, "Editing agent.go"},
		{"terminal", `{"command":"go build ./..."}`, "Running go build ./..."},
		{"search_files", `{"pattern":"banner","target":"files"}`, "Finding files banner"},
		{"search_files", `{"pattern":"TODO"}`, "Searching TODO"},
		{"skill_view", `{"name":"hiroto-development"}`, "Opening skill hiroto-development"},
		{"web_search", `{"query":"bubbletea mouse"}`, "Searching web bubbletea mouse"},
		{"web_extract", `{"urls":["https://example.com"]}`, "Fetching https://example.com"},
		{"todo", `{}`, "Updating todos"},
		{"read_file", ``, "Reading"},                       // no args → verb only
		{"mystery_tool", `{"path":"x"}`, "mystery_tool x"}, // unknown tool → name + arg
		{"mystery_tool", `{}`, "mystery_tool"},             // unknown, no arg → bare name
	}
	for _, c := range cases {
		got := toolActivity(c.name, c.args)
		if got != c.want {
			t.Errorf("toolActivity(%q, %q) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}

// Malformed JSON must not panic — fall back to the bare verb.
func TestToolActivityBadJSON(t *testing.T) {
	if got := toolActivity("read_file", `{not json`); got != "Reading" {
		t.Errorf("bad JSON: got %q, want %q", got, "Reading")
	}
}

// A model-supplied "activity" arg overrides the derived label (Hermes-style).
func TestToolActivityModelNarration(t *testing.T) {
	got := toolActivity("read_file", `{"path":"main.go","activity":"Checking the banner layout"}`)
	if got != "Checking the banner layout" {
		t.Errorf("model narration: got %q, want %q", got, "Checking the banner layout")
	}
	// Empty/whitespace activity falls back to the derived label.
	if got := toolActivity("read_file", `{"path":"main.go","activity":"   "}`); got != "Reading main.go" {
		t.Errorf("blank activity: got %q, want %q", got, "Reading main.go")
	}
}
