package agent

import (
	"strings"
	"testing"

	"github.com/hirotomasato/hiroto/internal/llm"
)

func TestCompressBudgetOff(t *testing.T) {
	a := &Agent{CompressBudget: 0, CompressKeepTurns: 6}
	if err := a.compress(nil); err != nil {
		t.Fatalf("compress with budget=0 should be no-op: %v", err)
	}
}

func TestCompressNotEnoughMessages(t *testing.T) {
	a := &Agent{CompressBudget: 100, CompressKeepTurns: 6}
	// 6 turns * 2 = 12 keep messages, so < 16 messages = no compress
	a.Messages = []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "hi"},
	}
	if err := a.compress(nil); err != nil {
		t.Fatalf("compress with too few messages should be no-op: %v", err)
	}
}

func TestCompressTokenBudget(t *testing.T) {
	a := &Agent{CompressBudget: 100, CompressKeepTurns: 1} // keep 2 msgs
	// Build enough messages to trigger the token check, but under budget
	a.Messages = []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "short"},
		{Role: llm.RoleAssistant, Content: "ok"},
		{Role: llm.RoleUser, Content: "another"},
		{Role: llm.RoleAssistant, Content: "done"},
	}
	if err := a.compress(nil); err != nil {
		t.Fatalf("under budget should be no-op: %v", err)
	}
}

func TestEstimateTokens(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "hello world"},
		{Role: llm.RoleUser, Content: "test message"},
	}
	tok := estimateTokens(msgs)
	if tok <= 0 {
		t.Errorf("estimateTokens returned %d, expected >0", tok)
	}
	// "hello world" = 11 chars + "test message" = 12 chars = 23 / 3 ≈ 7
	if tok < 5 || tok > 10 {
		t.Errorf("estimateTokens = %d, expected ~7-8 for 23 chars", tok)
	}
}

func TestBuildSummaryBlock(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "buat hello world"},
		{Role: llm.RoleAssistant, Content: "ok", ToolCalls: []llm.ToolCall{
			{Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "write_file", Arguments: `{"path":"hello.go"}`}},
		}},
		{Role: llm.RoleTool, Name: "write_file", Content: "file written"},
	}
	block := buildSummaryBlock(msgs)
	if block == "" {
		t.Fatal("buildSummaryBlock returned empty")
	}
	if !strings.Contains(block, "[USER]") || !strings.Contains(block, "[TOOL CALL]") || !strings.Contains(block, "[TOOL RESULT:") {
		t.Errorf("summary block missing expected sections: %s", block)
	}
}

func TestCompressNowFailsOnShortConversation(t *testing.T) {
	a := &Agent{CompressKeepTurns: 6}
	a.Messages = []llm.Message{
		{Role: llm.RoleSystem, Content: "s"},
		{Role: llm.RoleUser, Content: "hi"},
	}
	if err := a.CompressNow(nil); err == nil {
		t.Fatal("CompressNow on short conversation should fail")
	}
}