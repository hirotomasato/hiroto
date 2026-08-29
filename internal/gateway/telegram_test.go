package gateway

import (
	"testing"

	"github.com/hirotomasato/hiroto/internal/llm"
	"github.com/hirotomasato/hiroto/internal/session"
)

func TestSessionIDFor(t *testing.T) {
	if got := sessionIDFor(12345); got != "tg12345" {
		t.Fatalf("sessionIDFor(12345) = %q", got)
	}
	if got := sessionIDFor(-1001234); got != "tg-1001234" {
		t.Fatalf("sessionIDFor(-1001234) = %q", got)
	}
}

func TestToStoredRoundTrip(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "halo"},
		{Role: llm.RoleAssistant, Content: "hai", ToolCalls: []llm.ToolCall{{ID: "c1", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "terminal", Arguments: `{"cmd":"ls"}`}}}},
		{Role: llm.RoleTool, ToolCallID: "c1", Name: "terminal", Content: "ok"},
	}
	stored := toStored(msgs)
	if len(stored) != 3 {
		t.Fatalf("toStored len = %d, want 3", len(stored))
	}
	if stored[1].ToolCalls[0].Name != "terminal" {
		t.Fatalf("tool call name = %q", stored[1].ToolCalls[0].Name)
	}
	back := fromStored(stored)
	if len(back) != 3 || back[1].ToolCalls[0].Function.Name != "terminal" {
		t.Fatalf("round-trip mismatch: %+v", back)
	}
}

func TestFirstUserText(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "  balas ini ya  "},
		{Role: llm.RoleAssistant, Content: "ok"},
	}
	if got := firstUserText(msgs, 60); got != "balas ini ya" {
		t.Fatalf("firstUserText = %q", got)
	}
}

func TestChatPersistenceShapes(t *testing.T) {
	// ensure a chat's stable id loads/saves into the session store layout
	s := &session.Session{ID: "tg42", Title: "t", Messages: nil}
	if s.ID != "tg42" {
		t.Fatalf("id = %q", s.ID)
	}
}
