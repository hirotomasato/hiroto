package gateway

import (
	"strings"
	"testing"

	"github.com/hirotomasato/hiroto/internal/session"
	"github.com/hirotomasato/hiroto/internal/tools"
)

// Each chat's plan lives under its own session id: chat B must never see or
// advance chat A's checklist (the gateway serves every chat from one process).
func TestGatewayTodoPerChatIsolation(t *testing.T) {
	t.Setenv("HIROTO_HOME", t.TempDir())
	g := &gw{todos: tools.NewSessionTodoStore("boot"), chats: map[int64]*chat{}}

	g.bindTodos(100)
	g.todos.Save([]tools.TodoItem{{ID: "1", Content: "plan A", Status: tools.StatusInProgress}})

	g.bindTodos(200)
	if got := len(g.todos.Snapshot()); got != 0 {
		t.Fatalf("chat 200 inherited %d items from chat 100", got)
	}
	g.todos.Save([]tools.TodoItem{{ID: "1", Content: "plan B", Status: tools.StatusPending}})

	g.bindTodos(100)
	items := g.todos.Snapshot()
	if len(items) != 1 || items[0].Content != "plan A" {
		t.Errorf("chat 100 plan = %+v, want plan A", items)
	}
}

// A chat that resumed another session keeps writing that session's plan.
func TestGatewayBindTodosFollowsResumedSession(t *testing.T) {
	t.Setenv("HIROTO_HOME", t.TempDir())
	g := &gw{todos: tools.NewSessionTodoStore("boot"), chats: map[int64]*chat{}}
	g.chats[100] = &chat{id: "custom_session"}

	g.bindTodos(100)
	g.todos.Save([]tools.TodoItem{{ID: "1", Content: "resumed", Status: tools.StatusPending}})

	direct := tools.NewSessionTodoStore("custom_session")
	if got := len(direct.Snapshot()); got != 1 {
		t.Errorf("plan not written to the resumed session id: %+v", direct.Snapshot())
	}
}

// renderTodoList is what the user reads in chat: ids must be visible so
// /todo done <id> is usable.
func TestGatewayRenderTodoList(t *testing.T) {
	t.Setenv("HIROTO_HOME", t.TempDir())
	ts := tools.NewSessionTodoStore("render")
	if got := renderTodoList(ts); !strings.Contains(got, "belum ada task") {
		t.Errorf("empty list = %q", got)
	}
	ts.Save([]tools.TodoItem{
		{ID: "1", Content: "alpha", Status: tools.StatusCompleted},
		{ID: "2", Content: "beta", Status: tools.StatusInProgress},
	})
	out := renderTodoList(ts)
	for _, want := range []string{"Tasks 1/2", "✔", "▶", "alpha", "beta", "/todo done"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderTodoList missing %q:\n%s", want, out)
		}
	}
}

// The gateway persists and restores the plan with the session, never as running.
func TestGatewayTodoSessionRoundTrip(t *testing.T) {
	t.Setenv("HIROTO_HOME", t.TempDir())
	ts := tools.NewSessionTodoStore("tg123")
	ts.Save([]tools.TodoItem{
		{ID: "1", Content: "alpha", Status: tools.StatusCompleted},
		{ID: "2", Content: "beta", Status: tools.StatusInProgress},
	})
	stored := todosToStored(ts)
	if len(stored) != 2 {
		t.Fatalf("todosToStored = %+v", stored)
	}

	other := tools.NewSessionTodoStore("tg999")
	restoreTodos(other, &session.Session{ID: "tg123", Todos: stored})
	items := other.Snapshot()
	if len(items) != 2 {
		t.Fatalf("restored %d items, want 2", len(items))
	}
	if items[1].Status != tools.StatusPending {
		t.Errorf("resumed task 2 = %q, want pending", items[1].Status)
	}
}
