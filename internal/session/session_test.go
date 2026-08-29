package session

import (
	"testing"
	"time"
)

func TestSaveLoadList(t *testing.T) {
	s := NewAt(t.TempDir())
	sess := &Session{
		ID:      "20260829_120000_ab12cd34",
		Title:   "tes sesi",
		Model:   "teamo/glm-5.3-flash",
		Created: time.Now(),
		Updated: time.Now(),
		Messages: []StoredMessage{
			{Role: "user", Content: "halo"},
			{Role: "assistant", Content: "hai!", ToolCalls: []StoredToolCall{{ID: "c1", Name: "terminal", Args: `{"command":"ls"}`}}},
			{Role: "tool", ToolCallID: "c1", ToolName: "terminal", Content: "out"},
		},
	}
	if err := s.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.Load(sess.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Title != "tes sesi" || len(got.Messages) != 3 || got.Messages[1].ToolCalls[0].Name != "terminal" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	// path traversal must be rejected
	if _, err := s.Load("../evil"); err == nil {
		t.Fatal("expected invalid id to be rejected")
	}
	list := s.List()
	if len(list) != 1 || list[0].ID != sess.ID {
		t.Fatalf("list mismatch: %+v", list)
	}
}
