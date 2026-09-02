package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TodoItem is one task in the agent's checklist.
type TodoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"` // pending | in_progress | completed | cancelled
}

// Task statuses.
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusCancelled  = "cancelled"
)

// EnvSessionID names the session that owns the checklist. Child processes
// (`hiroto tool todo`, spawned by execute_code) inherit it, so they write to
// the same per-session file as the TUI instead of a global one.
const EnvSessionID = "HIROTO_SESSION"

// TodoStore persists a checklist to ~/.hiroto/todos/<session>.json.
//
// The file is per-session on purpose. A single global todo.json meant every
// launch inherited the previous run's plan, so a task left "in_progress"
// stayed pinned in the panel of a brand-new session forever.
//
// One instance is shared by the TUI panel and the todo tool (Options.Todo),
// and tool calls can run concurrently (agent.executeToolCalls fans out), so
// every method takes the mutex.
type TodoStore struct {
	mu    sync.Mutex
	Path  string
	Items []TodoItem

	// File identity at last read, so the render path can skip re-parsing
	// when nothing changed (renderTodoPanel runs on every frame).
	lastSize int64
	lastMod  time.Time
}

// todosDir holds one checklist file per session.
func todosDir() string { return filepath.Join(homeDir(), "todos") }

// todoPathFor maps a session id to its checklist file. Ids are sanitized so a
// crafted id can't escape the todos directory.
func todoPathFor(sessID string) string {
	id := sanitizeID(sessID)
	if id == "" {
		id = "_default"
	}
	return filepath.Join(todosDir(), id+".json")
}

// sanitizeID keeps [A-Za-z0-9_-] and drops everything else (path separators
// included), so ids coming from /title or a chat id can't traverse.
func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

// NewTodoStore returns a store for the session named by HIROTO_SESSION.
func NewTodoStore() *TodoStore { return NewSessionTodoStore(os.Getenv(EnvSessionID)) }

var (
	sharedTodoOnce sync.Once
	sharedTodo     *TodoStore
)

// SharedTodo returns the process-wide store. The TUI panel and the todo tool
// must observe the same instance, otherwise one holds a stale copy of the list
// and the panel disagrees with what the agent thinks the plan is.
func SharedTodo() *TodoStore {
	sharedTodoOnce.Do(func() { sharedTodo = NewTodoStore() })
	return sharedTodo
}

// NewSessionTodoStore returns a store bound to one session id.
func NewSessionTodoStore(sessID string) *TodoStore {
	ts := &TodoStore{Path: todoPathFor(sessID)}
	ts.mu.Lock()
	ts.readLocked()
	ts.mu.Unlock()
	return ts
}

// Retarget points the store at another session's checklist and loads it. Used
// when the session id changes (/new, /branch, /title, resume). It also exports
// HIROTO_SESSION so child tool processes follow the same file.
func (ts *TodoStore) Retarget(sessID string) {
	_ = os.Setenv(EnvSessionID, sessID)
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.Path = todoPathFor(sessID)
	ts.lastSize, ts.lastMod = 0, time.Time{}
	ts.readLocked()
}

// readLocked re-reads the checklist file. Caller holds the mutex.
func (ts *TodoStore) readLocked() {
	ts.Items = nil
	data, err := os.ReadFile(ts.Path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &ts.Items)
	if fi, err := os.Stat(ts.Path); err == nil {
		ts.lastSize, ts.lastMod = fi.Size(), fi.ModTime()
	}
}

// writeLocked persists the current items atomically (temp + rename) so a
// concurrent reader never sees a half-written file. Caller holds the mutex.
func (ts *TodoStore) writeLocked() {
	_ = os.MkdirAll(filepath.Dir(ts.Path), 0o755)
	if len(ts.Items) == 0 {
		// An empty plan is the absence of a file: nothing to inherit later.
		_ = os.Remove(ts.Path)
		ts.lastSize, ts.lastMod = 0, time.Time{}
		return
	}
	data, err := json.MarshalIndent(ts.Items, "", "  ")
	if err != nil {
		return
	}
	tmp := ts.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, ts.Path); err != nil {
		_ = os.Remove(tmp)
		return
	}
	if fi, err := os.Stat(ts.Path); err == nil {
		ts.lastSize, ts.lastMod = fi.Size(), fi.ModTime()
	}
}

// Save replaces the whole list.
func (ts *TodoStore) Save(items []TodoItem) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.Items = normalize(items)
	keepFirstInProgress(ts.Items)
	ts.writeLocked()
}

// Reload re-reads the checklist from disk unconditionally.
func (ts *TodoStore) Reload() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.readLocked()
}

// ReloadIfChanged re-reads only when the file's size or mtime moved. The TUI
// panel calls this every frame, so the common case must be a single stat.
func (ts *TodoStore) ReloadIfChanged() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	fi, err := os.Stat(ts.Path)
	if err != nil {
		if len(ts.Items) > 0 {
			ts.Items = nil // file removed (cleared by another process)
			ts.lastSize, ts.lastMod = 0, time.Time{}
		}
		return
	}
	if fi.Size() == ts.lastSize && fi.ModTime().Equal(ts.lastMod) {
		return
	}
	ts.readLocked()
}

// Update merges items into the existing list by id: an incoming item whose id
// matches an existing one patches it in place (a non-empty content or status
// overwrites), and an item with a new (or empty) id is appended in order.
// This is the "advance the plan" path — marking a task in_progress/completed
// without restating the whole list. Reloads first so it merges against the
// latest on-disk state, since a child `hiroto tool todo` process writes the
// same file.
func (ts *TodoStore) Update(items []TodoItem) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.readLocked()
	byID := make(map[string]int, len(ts.Items))
	for i, it := range ts.Items {
		if it.ID != "" {
			byID[it.ID] = i
		}
	}
	lastInProgress := -1
	for _, in := range normalize(items) {
		idx := -1
		if in.ID != "" {
			if found, ok := byID[in.ID]; ok {
				idx = found
				if in.Content != "" {
					ts.Items[idx].Content = in.Content
				}
				if in.Status != "" {
					ts.Items[idx].Status = in.Status
				}
			}
		}
		if idx < 0 {
			ts.Items = append(ts.Items, in)
			idx = len(ts.Items) - 1
			if in.ID != "" {
				byID[in.ID] = idx
			}
		}
		if ts.Items[idx].Status == StatusInProgress {
			lastInProgress = idx
		}
	}
	if lastInProgress >= 0 {
		// The just-touched task wins: only one item may be in_progress.
		for i := range ts.Items {
			if i != lastInProgress && ts.Items[i].Status == StatusInProgress {
				ts.Items[i].Status = StatusPending
			}
		}
	} else {
		keepFirstInProgress(ts.Items)
	}
	ts.writeLocked()
}

// Clear drops the plan entirely (new session, or the user unsticking it).
func (ts *TodoStore) Clear() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.Items = nil
	ts.writeLocked()
}

// Complete marks one task completed by id. Reports false when the id is unknown.
func (ts *TodoStore) Complete(id string) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.readLocked()
	for i := range ts.Items {
		if ts.Items[i].ID == id {
			ts.Items[i].Status = StatusCompleted
			ts.writeLocked()
			return true
		}
	}
	return false
}

// Demote turns every in_progress task back to pending and reports how many it
// moved. Called when a turn is cancelled or fails: nothing is running any more,
// so leaving a task marked in_progress would strand it in the panel — the
// "task nyangkut" symptom.
func (ts *TodoStore) Demote() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.readLocked()
	n := 0
	for i := range ts.Items {
		if ts.Items[i].Status == StatusInProgress {
			ts.Items[i].Status = StatusPending
			n++
		}
	}
	if n > 0 {
		ts.writeLocked()
	}
	return n
}

// Snapshot returns a copy of the items, safe to read while tools mutate.
func (ts *TodoStore) Snapshot() []TodoItem {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make([]TodoItem, len(ts.Items))
	copy(out, ts.Items)
	return out
}

// Counts returns (settled, total) where settled covers completed + cancelled.
func (ts *TodoStore) Counts() (int, int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	done := 0
	for _, it := range ts.Items {
		if it.Status == StatusCompleted || it.Status == StatusCancelled {
			done++
		}
	}
	return done, len(ts.Items)
}

// normalize copies items, trims ids/content and defaults a blank status.
func normalize(items []TodoItem) []TodoItem {
	out := make([]TodoItem, 0, len(items))
	for _, it := range items {
		it.ID = strings.TrimSpace(it.ID)
		it.Content = strings.TrimSpace(it.Content)
		it.Status = strings.TrimSpace(it.Status)
		switch it.Status {
		case StatusPending, StatusInProgress, StatusCompleted, StatusCancelled:
		default:
			it.Status = StatusPending
		}
		if it.Content == "" && it.ID == "" {
			continue
		}
		out = append(out, it)
	}
	return out
}

// keepFirstInProgress enforces the one-active-task rule on a full list write.
func keepFirstInProgress(items []TodoItem) {
	seen := false
	for i := range items {
		if items[i].Status != StatusInProgress {
			continue
		}
		if seen {
			items[i].Status = StatusPending
			continue
		}
		seen = true
	}
}

// Render draws the checklist for tool output and /todo, priority order preserved.
func (ts *TodoStore) Render() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.Items) == 0 {
		return "(no tasks)"
	}
	icon := map[string]string{
		StatusPending:    "○",
		StatusInProgress: "◐",
		StatusCompleted:  "●",
		StatusCancelled:  "×",
	}
	done := 0
	for _, it := range ts.Items {
		if it.Status == StatusCompleted || it.Status == StatusCancelled {
			done++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Tasks %d/%d\n", done, len(ts.Items))
	for _, it := range ts.Items {
		s := it.Status
		if s == "" {
			s = StatusPending
		}
		id := it.ID
		if id != "" {
			id = " (" + id + ")"
		}
		b.WriteString(icon[s] + " [" + s + "] " + it.Content + id + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// PruneTodos deletes checklist files older than maxAge so abandoned sessions
// don't pile up in ~/.hiroto/todos. Best-effort.
func PruneTodos(maxAge time.Duration) int {
	entries, err := os.ReadDir(todosDir())
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-maxAge)
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if os.Remove(filepath.Join(todosDir(), e.Name())) == nil {
			n++
		}
	}
	return n
}

// MigrateLegacyTodo removes the pre-per-session ~/.hiroto/todo.json. Its
// contents belonged to whichever session ran last, and keeping it around only
// resurrects that stale plan.
func MigrateLegacyTodo() bool {
	return os.Remove(filepath.Join(homeDir(), "todo.json")) == nil
}

func homeDir() string {
	if h := os.Getenv("HIROTO_HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".hiroto")
}
