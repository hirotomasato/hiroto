package main

import (
	"testing"

	"github.com/hirotomasato/hiroto/internal/session"
	"github.com/hirotomasato/hiroto/internal/tools"
)

// A session file must carry its checklist, so quitting and resuming brings the
// plan back instead of showing an empty panel (or worse, another session's).
func TestSessionTodoRoundTrip(t *testing.T) {
	t.Setenv("HIROTO_HOME", t.TempDir())
	ts := tools.NewSessionTodoStore("sess1")
	ts.Save([]tools.TodoItem{
		{ID: "1", Content: "alpha", Status: tools.StatusCompleted},
		{ID: "2", Content: "beta", Status: tools.StatusInProgress},
	})

	stored := todosToStored(ts)
	if len(stored) != 2 || stored[1].Content != "beta" {
		t.Fatalf("todosToStored = %+v", stored)
	}

	// Resume into a different store instance.
	other := tools.NewSessionTodoStore("sess2")
	restoreTodos(other, &session.Session{ID: "sess1", Todos: stored})

	items := other.Snapshot()
	if len(items) != 2 {
		t.Fatalf("restored %d items, want 2: %+v", len(items), items)
	}
	if items[0].Status != tools.StatusCompleted {
		t.Errorf("item 1 status = %q, want completed", items[0].Status)
	}
	// Nothing is running on resume: the in_progress task comes back pending.
	if items[1].Status != tools.StatusPending {
		t.Errorf("item 2 status = %q, want pending (demoted on resume)", items[1].Status)
	}
}

// An empty checklist stays out of the session JSON (omitempty), so old sessions
// and no-plan sessions don't grow a null field.
func TestTodosToStoredEmpty(t *testing.T) {
	t.Setenv("HIROTO_HOME", t.TempDir())
	if got := todosToStored(nil); got != nil {
		t.Errorf("todosToStored(nil) = %+v, want nil", got)
	}
	ts := tools.NewSessionTodoStore("empty")
	if got := todosToStored(ts); got != nil {
		t.Errorf("todosToStored(empty) = %+v, want nil", got)
	}
}

// saveSession must persist the plan alongside the transcript.
func TestSaveSessionIncludesTodos(t *testing.T) {
	t.Setenv("HIROTO_HOME", t.TempDir())
	m := todoModel(t)
	store := session.NewAt(t.TempDir())
	m.sessStore = store
	m.sessID = "savetest"
	m.todos = tools.NewSessionTodoStore(m.sessID)
	m.todos.Save([]tools.TodoItem{{ID: "1", Content: "alpha", Status: tools.StatusPending}})
	m.ag.Messages = fromStored([]session.StoredMessage{{Role: "user", Content: "hi"}})

	m.saveSession()

	got, err := store.Load("savetest")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Todos) != 1 || got.Todos[0].Content != "alpha" {
		t.Errorf("session.Todos = %+v, want 1 item 'alpha'", got.Todos)
	}
}
