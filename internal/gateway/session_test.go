package gateway

import (
	"context"
	"fmt"
	"testing"

	"github.com/hirotomasato/hiroto/internal/agent"
	"github.com/hirotomasato/hiroto/internal/llm"
	"github.com/hirotomasato/hiroto/internal/session"
	"github.com/hirotomasato/hiroto/internal/tools"
)

func testGateway(t *testing.T) *gw {
	t.Helper()
	dir := t.TempDir()
	ss := session.NewAt(dir)
	ag := &agent.Agent{
		Client:   &llm.Client{Model: "test"},
		Reg:      tools.NewRegistry(),
		MaxTurns: 1,
	}
	return &gw{
		ag:       ag,
		sess:     ss,
		chats:    make(map[int64]*chat),
		cancels:  make(map[int64]context.CancelFunc),
		steerChs: make(map[int64]chan string),
		todos:    tools.NewSessionTodoStore("_test"),
		allowed:  map[int64]bool{123: true},
	}
}

func TestGatewaySessionSaveListSearchResume(t *testing.T) {
	g := testGateway(t)

	// Step 1: create a chat with messages and save it.
	chatID := int64(456)
	st := &chat{id: sessionIDFor(chatID)}
	st.messages = []llm.Message{
		{Role: llm.RoleUser, Content: "buat hello world"},
		{Role: llm.RoleAssistant, Content: "ok, file dibuat"},
	}
	g.chats[chatID] = st
	g.saveChat(st)

	// Step 2: list should show the session.
	list := g.sess.List()
	if len(list) != 1 {
		t.Fatalf("List() = %d sessions, want 1", len(list))
	}
	if list[0].ID != sessionIDFor(chatID) {
		t.Errorf("session ID = %s, want %s", list[0].ID, sessionIDFor(chatID))
	}

	// Step 3: search by title.
	results := g.sess.Search("hello")
	if len(results) != 1 {
		t.Fatalf("Search(hello) = %d results, want 1", len(results))
	}

	// Step 4: search by message content.
	results = g.sess.Search("file dibuat")
	if len(results) != 1 {
		t.Fatalf("Search(file dibuat) = %d results, want 1", len(results))
	}

	// Step 5: search with no match.
	results = g.sess.Search("zzznomatch")
	if len(results) != 0 {
		t.Fatalf("Search(zzznomatch) = %d results, want 0", len(results))
	}

	// Step 6: resume the session into a new chat.
	newChatID := int64(789)
	sess, err := g.sess.Load(sessionIDFor(chatID))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	newSt := &chat{id: sess.ID}
	newSt.messages = fromStored(sess.Messages)
	g.chats[newChatID] = newSt

	if len(newSt.messages) != 2 {
		t.Fatalf("resumed messages = %d, want 2", len(newSt.messages))
	}
	userMsg, _ := newSt.messages[0].Content.(string)
	if userMsg != "buat hello world" {
		t.Errorf("resumed user message = %q", userMsg)
	}
}

func TestGatewaySessionSaveTodoRoundTrip(t *testing.T) {
	g := testGateway(t)

	// Save a session with todo items.
	chatID := int64(456)
	st := &chat{id: sessionIDFor(chatID)}
	st.messages = []llm.Message{
		{Role: llm.RoleUser, Content: "bikin todo"},
		{Role: llm.RoleAssistant, Content: "ok"},
	}

	// Add todos via the store.
	g.bindTodos(chatID)
	g.todos.Update([]tools.TodoItem{
		{ID: "1", Content: "task 1", Status: "completed"},
		{ID: "2", Content: "task 2", Status: "in_progress"},
	})

	g.chats[chatID] = st
	g.saveChat(st)

	// Load the session and verify todos are restored.
	sess, err := g.sess.Load(sessionIDFor(chatID))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(sess.Todos) != 2 {
		t.Fatalf("saved todos = %d, want 2", len(sess.Todos))
	}
	if sess.Todos[0].Status != "completed" {
		t.Errorf("todo[0] status = %s, want completed", sess.Todos[0].Status)
	}
}

func TestGatewaySessionSaveEmpty(t *testing.T) {
	g := testGateway(t)

	// Saving a chat with no user messages should be a no-op.
	chatID := int64(456)
	st := &chat{id: sessionIDFor(chatID)}
	st.messages = []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
	}
	g.chats[chatID] = st
	g.saveChat(st)

	list := g.sess.List()
	if len(list) != 0 {
		t.Fatalf("empty save should not create session, got %d", len(list))
	}
}

func TestGatewaySessionInvalidID(t *testing.T) {
	g := testGateway(t)

	// Loading an invalid session ID should fail.
	_, err := g.sess.Load("../../../etc/passwd")
	if err == nil {
		t.Fatal("Load with invalid ID should fail")
	}
}

func TestGatewaySessionListEmpty(t *testing.T) {
	dir := t.TempDir()
	ss := session.NewAt(dir)
	list := ss.List()
	if len(list) != 0 {
		t.Fatalf("empty store should return 0 sessions, got %d", len(list))
	}
}

func TestGatewaySessionMultipleSessions(t *testing.T) {
	g := testGateway(t)

	// Save multiple sessions and verify list order (newest first).
	for i := range 3 {
		chatID := int64(100 + i)
		st := &chat{id: sessionIDFor(chatID)}
		st.messages = []llm.Message{
			{Role: llm.RoleUser, Content: fmt.Sprintf("msg %d", i)},
		}
		g.chats[chatID] = st
		g.saveChat(st)
	}

	list := g.sess.List()
	if len(list) != 3 {
		t.Fatalf("List() = %d sessions, want 3", len(list))
	}
	// Newest first by ID.
	if list[0].ID < list[1].ID {
		t.Error("sessions should be sorted newest first")
	}
}

func TestGatewaySessionResumeNonexistent(t *testing.T) {
	g := testGateway(t)

	_, err := g.sess.Load("nonexistent")
	if err == nil {
		t.Fatal("Load(nonexistent) should fail")
	}
}

func TestGatewaySessionSearchEmptyStore(t *testing.T) {
	dir := t.TempDir()
	ss := session.NewAt(dir)
	results := ss.Search("anything")
	if len(results) != 0 {
		t.Fatalf("Search on empty store should return 0, got %d", len(results))
	}
}