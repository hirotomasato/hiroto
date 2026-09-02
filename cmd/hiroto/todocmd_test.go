package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hirotomasato/hiroto/internal/agent"
	"github.com/hirotomasato/hiroto/internal/config"
	"github.com/hirotomasato/hiroto/internal/llm"
	"github.com/hirotomasato/hiroto/internal/memory"
	"github.com/hirotomasato/hiroto/internal/session"
	"github.com/hirotomasato/hiroto/internal/tools"
)

// todoModel builds a TUI model with HIROTO_HOME isolated, so the checklist
// under test never touches the developer's real ~/.hiroto.
func todoModel(t *testing.T) model {
	t.Helper()
	t.Setenv("HIROTO_HOME", t.TempDir())
	cfg := &config.Config{}
	cfg.Model.Name = "test-model"
	m := initialModel(cfg, &agent.Agent{Client: &llm.Client{}}, memory.New(), session.NewAt(t.TempDir()))
	// initialModel binds the shared store; retarget onto this test's home.
	m.todos = tools.NewSessionTodoStore(m.sessID)
	m.width, m.height = 100, 30
	return m
}

// lastLines joins the transcript lines for substring assertions.
func lastLines(m model, n int) string {
	if len(m.lines) < n {
		n = len(m.lines)
	}
	var b strings.Builder
	for _, ln := range m.lines[len(m.lines)-n:] {
		b.WriteString(ln.text + "\n")
	}
	return b.String()
}

// The panel is invisible until the agent plans something — it must not steal a
// row from the viewport when there are no tasks.
func TestTodoPanelEmptyIsHidden(t *testing.T) {
	m := todoModel(t)
	if got := m.renderTodoPanel(); got != "" {
		t.Errorf("empty panel = %q, want \"\"", got)
	}
	m.recalcViewport()
	if m.vp.Height != 24 {
		t.Errorf("vp.Height with no tasks = %d, want 24", m.vp.Height)
	}
}

// With tasks, the panel renders and the viewport shrinks by exactly its height.
func TestTodoPanelShrinksViewport(t *testing.T) {
	m := todoModel(t)
	m.todos.Save([]tools.TodoItem{
		{ID: "1", Content: "alpha", Status: tools.StatusCompleted},
		{ID: "2", Content: "beta", Status: tools.StatusInProgress},
	})
	panel := m.renderTodoPanel()
	if panel == "" {
		t.Fatal("panel empty with 2 tasks")
	}
	if !strings.Contains(panel, "Tasks 1/2") || !strings.Contains(panel, "beta") {
		t.Errorf("panel missing header/content:\n%s", panel)
	}
	m.recalcViewport()
	if m.vp.Height >= 23 {
		t.Errorf("vp.Height = %d, want < 23 (panel takes rows)", m.vp.Height)
	}
}

// An in_progress task while nothing is running is flagged (idle) so the user
// isn't left wondering whether work is still happening.
func TestTodoPanelIdleMarker(t *testing.T) {
	m := todoModel(t)
	m.todos.Save([]tools.TodoItem{{ID: "1", Content: "alpha", Status: tools.StatusInProgress}})
	if got := m.renderTodoPanel(); !strings.Contains(got, "idle") {
		t.Errorf("idle marker missing while not busy:\n%s", got)
	}
	m.busy = true
	if got := m.renderTodoPanel(); strings.Contains(got, "idle") {
		t.Errorf("idle marker shown while busy:\n%s", got)
	}
}

// A long plan windows around the running task instead of always showing the
// first rows (the active task must stay on screen).
func TestTodoPanelWindowsAroundActive(t *testing.T) {
	m := todoModel(t)
	var items []tools.TodoItem
	for i := 1; i <= 20; i++ {
		st := tools.StatusPending
		if i <= 14 {
			st = tools.StatusCompleted
		}
		if i == 15 {
			st = tools.StatusInProgress
		}
		items = append(items, tools.TodoItem{ID: string(rune('a' + i - 1)), Content: "task" + string(rune('a'+i-1)), Status: st})
	}
	m.todos.Save(items)
	panel := m.renderTodoPanel()
	if !strings.Contains(panel, "task"+string(rune('a'+14))) {
		t.Errorf("active task (index 15) not visible:\n%s", panel)
	}
	if !strings.Contains(panel, "selesai di atas") {
		t.Errorf("expected hidden-above hint:\n%s", panel)
	}
}

// /todo add → done → unstick → clear, the manual escape hatch for a stuck plan.
func TestTodoCommandLifecycle(t *testing.T) {
	m := todoModel(t)

	m.handleTodoCmd([]string{"/todo"})
	if got := lastLines(m, 1); !strings.Contains(got, "belum ada task") {
		t.Errorf("empty /todo output = %q", got)
	}

	m.handleTodoCmd([]string{"/todo", "add", "bikin", "fitur"})
	items := m.todos.Snapshot()
	if len(items) != 1 || items[0].Content != "bikin fitur" {
		t.Fatalf("after add: %+v", items)
	}
	if items[0].ID != "1" {
		t.Errorf("first manual id = %q, want 1", items[0].ID)
	}

	m.handleTodoCmd([]string{"/todo", "add", "verifikasi"})
	if got := len(m.todos.Snapshot()); got != 2 {
		t.Fatalf("after second add: %d items, want 2", got)
	}

	m.handleTodoCmd([]string{"/todo", "done", "1"})
	if got := m.todos.Snapshot()[0].Status; got != tools.StatusCompleted {
		t.Errorf("task 1 = %q, want completed", got)
	}

	m.handleTodoCmd([]string{"/todo", "undo", "1"})
	if got := m.todos.Snapshot()[0].Status; got != tools.StatusPending {
		t.Errorf("after undo task 1 = %q, want pending", got)
	}

	// unstick releases a task the agent left running.
	m.todos.Update([]tools.TodoItem{{ID: "2", Status: tools.StatusInProgress}})
	m.handleTodoCmd([]string{"/todo", "unstick"})
	if got := status2(m.todos, "2"); got != tools.StatusPending {
		t.Errorf("after unstick task 2 = %q, want pending", got)
	}
	if got := lastLines(m, 2); !strings.Contains(got, "pending") {
		t.Errorf("unstick did not report the change: %q", got)
	}

	m.handleTodoCmd([]string{"/todo", "clear"})
	if got := len(m.todos.Snapshot()); got != 0 {
		t.Errorf("after clear: %d items, want 0", got)
	}

	m.handleTodoCmd([]string{"/todo", "bogus"})
	if m.flash == "" {
		t.Error("unknown subcommand did not flash usage")
	}
}

func status2(ts *tools.TodoStore, id string) string {
	for _, it := range ts.Snapshot() {
		if it.ID == id {
			return it.Status
		}
	}
	return "(missing)"
}

// A failed run must release the in_progress task — this is the exact "nyangkut"
// path: the turn dies, nobody is working, yet the panel claimed otherwise.
func TestAssistantDoneFailedDemotes(t *testing.T) {
	m := todoModel(t)
	m.todos.Save([]tools.TodoItem{{ID: "1", Content: "alpha", Status: tools.StatusInProgress}})
	m.busy = true

	updated, _ := m.Update(assistantDoneMsg{failed: true})
	mm := updated.(model)
	if mm.busy {
		t.Error("busy still true after done msg")
	}
	if got := status2(mm.todos, "1"); got != tools.StatusPending {
		t.Errorf("task 1 after failed run = %q, want pending", got)
	}
	if got := lastLines(mm, 4); !strings.Contains(got, "pending") {
		t.Errorf("no demote notice in transcript: %q", got)
	}
}

// A successful run that leaves a task in_progress gets called out, so the user
// knows the plan is stale rather than silently trusting the panel.
func TestAssistantDoneSuccessWarnsStuck(t *testing.T) {
	m := todoModel(t)
	m.todos.Save([]tools.TodoItem{{ID: "7", Content: "alpha", Status: tools.StatusInProgress}})
	m.busy = true

	updated, _ := m.Update(assistantDoneMsg{})
	mm := updated.(model)
	if got := status2(mm.todos, "7"); got != tools.StatusInProgress {
		t.Errorf("successful run changed status to %q, want in_progress (left as-is)", got)
	}
	if got := lastLines(mm, 4); !strings.Contains(got, "in_progress") || !strings.Contains(got, "7") {
		t.Errorf("no stuck-task hint in transcript: %q", got)
	}
}

// Ctrl+C during a run cancels and unsticks in one go.
func TestCancelDemotesTodos(t *testing.T) {
	m := todoModel(t)
	m.todos.Save([]tools.TodoItem{{ID: "1", Content: "alpha", Status: tools.StatusInProgress}})
	m.busy = true
	m.cancel = func() {}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	mm := updated.(model)
	if mm.busy {
		t.Error("busy still true after Ctrl+C")
	}
	if got := status2(mm.todos, "1"); got != tools.StatusPending {
		t.Errorf("task 1 after cancel = %q, want pending", got)
	}
	if got := lastLines(mm, 2); !strings.Contains(got, "dibatalkan") {
		t.Errorf("cancel notice missing: %q", got)
	}
}

// stuckTodos reports ids only while something claims to be running.
func TestStuckTodos(t *testing.T) {
	m := todoModel(t)
	if got := m.stuckTodos(); got != "" {
		t.Errorf("stuckTodos on empty = %q, want \"\"", got)
	}
	m.todos.Save([]tools.TodoItem{
		{ID: "1", Content: "a", Status: tools.StatusCompleted},
		{ID: "2", Content: "b", Status: tools.StatusInProgress},
	})
	if got := m.stuckTodos(); got != "2" {
		t.Errorf("stuckTodos = %q, want 2", got)
	}
}

// nextTodoID fills the first free slot so manual ids stay small and stable.
func TestNextTodoID(t *testing.T) {
	if got := nextTodoID(nil); got != "1" {
		t.Errorf("nextTodoID(nil) = %q, want 1", got)
	}
	items := []tools.TodoItem{{ID: "1"}, {ID: "3"}}
	if got := nextTodoID(items); got != "2" {
		t.Errorf("nextTodoID = %q, want 2 (gap filled)", got)
	}
}
