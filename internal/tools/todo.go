package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// TodoItem is one task in the agent's checklist.
type TodoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"` // pending | in_progress | completed | cancelled
}

// TodoStore persists the checklist to ~/.hiroto/todo.json.
type TodoStore struct {
	Path  string
	Items []TodoItem
}

func NewTodoStore() *TodoStore {
	home := homeDir()
	p := filepath.Join(home, "todo.json")
	ts := &TodoStore{Path: p}
	if data, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(data, &ts.Items)
	}
	return ts
}

func (ts *TodoStore) Save(items []TodoItem) {
	ts.Items = items
	if data, err := json.MarshalIndent(items, "", "  "); err == nil {
		_ = os.MkdirAll(filepath.Dir(ts.Path), 0o755)
		_ = os.WriteFile(ts.Path, data, 0o644)
	}
}

// Render draws the checklist, priority order preserved.
func (ts *TodoStore) Render() string {
	if len(ts.Items) == 0 {
		return "(no tasks)"
	}
	icon := map[string]string{
		"pending":     "○",
		"in_progress": "◐",
		"completed":   "●",
		"cancelled":   "×",
	}
	var b strings.Builder
	for _, it := range ts.Items {
		s := it.Status
		if s == "" {
			s = "pending"
		}
		b.WriteString(icon[s] + " [" + s + "] " + it.Content + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func homeDir() string {
	if h := os.Getenv("HIROTO_HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".hiroto")
}
