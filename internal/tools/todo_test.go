package tools

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func status(ts *TodoStore, id string) string {
	for _, it := range ts.Snapshot() {
		if it.ID == id {
			return it.Status
		}
	}
	return "(missing)"
}

func newTestStore(t *testing.T) *TodoStore {
	t.Helper()
	return &TodoStore{Path: filepath.Join(t.TempDir(), "todo.json")}
}

// Update must merge by id — advancing a task's status without wiping the rest
// of the plan. This is the "plan nyangkut" regression: before, update was an
// advertised-but-unimplemented no-op, so progress never stuck.
func TestTodoUpdateMerges(t *testing.T) {
	ts := newTestStore(t)
	ts.Save([]TodoItem{
		{ID: "1", Content: "a", Status: StatusPending},
		{ID: "2", Content: "b", Status: StatusPending},
		{ID: "3", Content: "c", Status: StatusPending},
	})

	// Advance task 1 and 2 by id only — content omitted must be preserved.
	ts.Update([]TodoItem{
		{ID: "1", Status: StatusCompleted},
		{ID: "2", Status: StatusInProgress},
	})

	if got := len(ts.Snapshot()); got != 3 {
		t.Fatalf("expected 3 items after merge, got %d: %+v", got, ts.Snapshot())
	}
	if got := status(ts, "1"); got != StatusCompleted {
		t.Errorf("task 1 status = %q, want completed", got)
	}
	if got := status(ts, "2"); got != StatusInProgress {
		t.Errorf("task 2 status = %q, want in_progress", got)
	}
	if got := status(ts, "3"); got != StatusPending {
		t.Errorf("task 3 status = %q, want pending (untouched)", got)
	}
	// Omitted content must survive a status-only patch.
	for _, it := range ts.Snapshot() {
		if it.ID == "1" && it.Content != "a" {
			t.Errorf("task 1 content overwritten to %q, want a", it.Content)
		}
	}

	// Reload from disk to confirm the merge was persisted, not just in memory.
	fresh := &TodoStore{Path: ts.Path}
	fresh.Reload()
	if got := status(fresh, "1"); got != StatusCompleted {
		t.Errorf("persisted task 1 status = %q, want completed", got)
	}
}

// An unknown id appends rather than replaces.
func TestTodoUpdateAppendsNewID(t *testing.T) {
	ts := newTestStore(t)
	ts.Save([]TodoItem{{ID: "1", Content: "a", Status: StatusPending}})
	ts.Update([]TodoItem{{ID: "9", Content: "new", Status: StatusPending}})
	if got := len(ts.Snapshot()); got != 2 {
		t.Fatalf("expected 2 items after appending new id, got %d", got)
	}
	if got := status(ts, "9"); got != StatusPending {
		t.Errorf("appended task 9 status = %q, want pending", got)
	}
}

// Only one task may be in_progress: starting task 2 releases task 1, so the
// panel can't show two "running" rows at once.
func TestTodoSingleInProgress(t *testing.T) {
	ts := newTestStore(t)
	ts.Save([]TodoItem{
		{ID: "1", Content: "a", Status: StatusInProgress},
		{ID: "2", Content: "b", Status: StatusPending},
	})
	ts.Update([]TodoItem{{ID: "2", Status: StatusInProgress}})
	if got := status(ts, "1"); got != StatusPending {
		t.Errorf("task 1 status = %q, want pending (demoted)", got)
	}
	if got := status(ts, "2"); got != StatusInProgress {
		t.Errorf("task 2 status = %q, want in_progress", got)
	}

	// A full write with two in_progress rows keeps only the first.
	ts.Save([]TodoItem{
		{ID: "1", Content: "a", Status: StatusInProgress},
		{ID: "2", Content: "b", Status: StatusInProgress},
	})
	if got := status(ts, "2"); got != StatusPending {
		t.Errorf("second in_progress on write = %q, want pending", got)
	}
}

// Demote is the unstick path: after a cancelled turn nothing is running, so no
// task may stay in_progress.
func TestTodoDemote(t *testing.T) {
	ts := newTestStore(t)
	ts.Save([]TodoItem{
		{ID: "1", Content: "a", Status: StatusInProgress},
		{ID: "2", Content: "b", Status: StatusCompleted},
	})
	if n := ts.Demote(); n != 1 {
		t.Fatalf("Demote() = %d, want 1", n)
	}
	if got := status(ts, "1"); got != StatusPending {
		t.Errorf("task 1 status = %q, want pending", got)
	}
	if got := status(ts, "2"); got != StatusCompleted {
		t.Errorf("task 2 status = %q, want completed (untouched)", got)
	}
	if n := ts.Demote(); n != 0 {
		t.Errorf("second Demote() = %d, want 0", n)
	}
}

func TestTodoCompleteAndClear(t *testing.T) {
	ts := newTestStore(t)
	ts.Save([]TodoItem{{ID: "1", Content: "a", Status: StatusInProgress}})
	if !ts.Complete("1") {
		t.Fatal(`Complete("1") = false, want true`)
	}
	if got := status(ts, "1"); got != StatusCompleted {
		t.Errorf("status = %q, want completed", got)
	}
	if ts.Complete("nope") {
		t.Error(`Complete("nope") = true, want false`)
	}

	ts.Clear()
	if got := len(ts.Snapshot()); got != 0 {
		t.Errorf("after Clear: %d items, want 0", got)
	}
	// Clearing removes the file so a later run can't inherit the plan.
	if _, err := os.Stat(ts.Path); !os.IsNotExist(err) {
		t.Errorf("todo file still present after Clear: err=%v", err)
	}
}

// Each session owns its checklist: retargeting must not carry the plan over.
func TestTodoPerSessionIsolation(t *testing.T) {
	t.Setenv("HIROTO_HOME", t.TempDir())
	a := NewSessionTodoStore("sessA")
	a.Save([]TodoItem{{ID: "1", Content: "plan A", Status: StatusInProgress}})

	b := NewSessionTodoStore("sessB")
	if got := len(b.Snapshot()); got != 0 {
		t.Fatalf("new session inherited %d items, want 0", got)
	}
	b.Save([]TodoItem{{ID: "1", Content: "plan B", Status: StatusPending}})

	a.Reload()
	items := a.Snapshot()
	if len(items) != 1 || items[0].Content != "plan A" {
		t.Errorf("session A plan changed: %+v", items)
	}

	// Retarget follows the other session's file and exports the id for children.
	b.Retarget("sessA")
	items = b.Snapshot()
	if len(items) != 1 || items[0].Content != "plan A" {
		t.Errorf("after Retarget: %+v, want plan A", items)
	}
	if got := os.Getenv(EnvSessionID); got != "sessA" {
		t.Errorf("%s = %q, want sessA", EnvSessionID, got)
	}
}

// A session id that tries to escape the todos directory is sanitized.
func TestTodoPathTraversal(t *testing.T) {
	t.Setenv("HIROTO_HOME", t.TempDir())
	ts := NewSessionTodoStore("../../etc/passwd")
	if dir := filepath.Dir(ts.Path); dir != todosDir() {
		t.Errorf("path escaped todos dir: %s", ts.Path)
	}
}

// The TUI panel calls ReloadIfChanged on every frame: it must see a write made
// by another store instance (the child `hiroto tool todo` process) and skip
// re-reading when nothing moved.
func TestTodoReloadIfChanged(t *testing.T) {
	ts := newTestStore(t)
	ts.Save([]TodoItem{{ID: "1", Content: "a", Status: StatusPending}})

	panel := &TodoStore{Path: ts.Path}
	panel.ReloadIfChanged()
	if got := len(panel.Snapshot()); got != 1 {
		t.Fatalf("panel saw %d items, want 1", got)
	}

	// Same file, untouched: the cached copy stays.
	panel.ReloadIfChanged()
	if got := len(panel.Snapshot()); got != 1 {
		t.Fatalf("panel saw %d items after no-op reload, want 1", got)
	}

	// Another instance writes; mtime granularity can be coarse, so make the
	// size differ too (two items instead of one).
	other := &TodoStore{Path: ts.Path}
	other.Save([]TodoItem{
		{ID: "1", Content: "a", Status: StatusCompleted},
		{ID: "2", Content: "b", Status: StatusInProgress},
	})
	panel.ReloadIfChanged()
	if got := len(panel.Snapshot()); got != 2 {
		t.Fatalf("panel saw %d items after external write, want 2", got)
	}
	if got := status(panel, "2"); got != StatusInProgress {
		t.Errorf("panel task 2 = %q, want in_progress", got)
	}

	// File removed by another process: the panel must empty out.
	other.Clear()
	panel.ReloadIfChanged()
	if got := len(panel.Snapshot()); got != 0 {
		t.Errorf("panel saw %d items after clear, want 0", got)
	}
}

// Tool calls fan out concurrently (agent.executeToolCalls), so the store has to
// survive parallel writers without a data race or a corrupt file. Run with -race.
func TestTodoConcurrentUpdates(t *testing.T) {
	ts := newTestStore(t)
	ts.Save([]TodoItem{
		{ID: "1", Content: "a", Status: StatusPending},
		{ID: "2", Content: "b", Status: StatusPending},
		{ID: "3", Content: "c", Status: StatusPending},
	})
	var wg sync.WaitGroup
	for i, id := range []string{"1", "2", "3"} {
		wg.Add(2)
		go func(id string) { defer wg.Done(); ts.Update([]TodoItem{{ID: id, Status: StatusCompleted}}) }(id)
		go func(i int) { defer wg.Done(); ts.ReloadIfChanged(); _ = ts.Render() }(i)
	}
	wg.Wait()

	fresh := &TodoStore{Path: ts.Path}
	fresh.Reload()
	if got := len(fresh.Snapshot()); got != 3 {
		t.Fatalf("after concurrent updates: %d items, want 3 (%+v)", got, fresh.Snapshot())
	}
	for _, id := range []string{"1", "2", "3"} {
		if got := status(fresh, id); got != StatusCompleted {
			t.Errorf("task %s = %q, want completed", id, got)
		}
	}
}

// Blank/invalid statuses default to pending instead of rendering an empty icon.
func TestTodoNormalizeStatus(t *testing.T) {
	ts := newTestStore(t)
	ts.Save([]TodoItem{
		{ID: "1", Content: "a"},
		{ID: "2", Content: "b", Status: "bogus"},
		{ID: "3", Content: "   "}, // no content, no id survives? id present -> kept
	})
	for _, it := range ts.Snapshot() {
		if it.Status != StatusPending {
			t.Errorf("task %s status = %q, want pending", it.ID, it.Status)
		}
	}
}

func TestTodoCounts(t *testing.T) {
	ts := newTestStore(t)
	ts.Save([]TodoItem{
		{ID: "1", Content: "a", Status: StatusCompleted},
		{ID: "2", Content: "b", Status: StatusCancelled},
		{ID: "3", Content: "c", Status: StatusInProgress},
		{ID: "4", Content: "d", Status: StatusPending},
	})
	done, total := ts.Counts()
	if done != 2 || total != 4 {
		t.Errorf("Counts() = (%d, %d), want (2, 4)", done, total)
	}
}

// The legacy global todo.json must go: it was the root cause of a new session
// inheriting the previous run's plan.
func TestMigrateLegacyTodo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HIROTO_HOME", home)
	legacy := filepath.Join(home, "todo.json")
	if err := os.WriteFile(legacy, []byte(`[{"id":"1","content":"old","status":"in_progress"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !MigrateLegacyTodo() {
		t.Fatal("MigrateLegacyTodo() = false, want true")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy todo.json still exists: err=%v", err)
	}
	if MigrateLegacyTodo() {
		t.Error("second MigrateLegacyTodo() = true, want false (already gone)")
	}
}

func TestPruneTodos(t *testing.T) {
	t.Setenv("HIROTO_HOME", t.TempDir())
	old := NewSessionTodoStore("stale")
	old.Save([]TodoItem{{ID: "1", Content: "old", Status: StatusPending}})
	fresh := NewSessionTodoStore("recent")
	fresh.Save([]TodoItem{{ID: "1", Content: "new", Status: StatusPending}})

	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old.Path, past, past); err != nil {
		t.Fatal(err)
	}
	if n := PruneTodos(24 * time.Hour); n != 1 {
		t.Fatalf("PruneTodos() = %d, want 1", n)
	}
	if _, err := os.Stat(old.Path); !os.IsNotExist(err) {
		t.Error("stale checklist survived pruning")
	}
	if _, err := os.Stat(fresh.Path); err != nil {
		t.Errorf("recent checklist was pruned: %v", err)
	}
}

// Render is what the agent reads back after a write: it must carry the ids so
// the model can address tasks in a later update call.
func TestTodoRenderShowsIDs(t *testing.T) {
	ts := newTestStore(t)
	if got := ts.Render(); got != "(no tasks)" {
		t.Errorf("empty Render() = %q, want (no tasks)", got)
	}
	ts.Save([]TodoItem{
		{ID: "7", Content: "build it", Status: StatusInProgress},
		{ID: "8", Content: "verify", Status: StatusPending},
	})
	out := ts.Render()
	for _, want := range []string{"Tasks 0/2", "(7)", "(8)", "build it", "in_progress"} {
		if !contains(out, want) {
			t.Errorf("Render() missing %q:\n%s", want, out)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
