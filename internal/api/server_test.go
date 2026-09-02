package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hirotomasato/hiroto/internal/agent"
	"github.com/hirotomasato/hiroto/internal/llm"
	"github.com/hirotomasato/hiroto/internal/tools"
)

func testAgent() *agent.Agent {
	return &agent.Agent{
		Client: &llm.Client{
			BaseURL: "http://localhost:9999/v1",
			Model:   "test-model",
			HTTP:    &http.Client{Timeout: 100 * time.Millisecond},
		},
		Reg:      tools.NewRegistry(),
		MaxTurns: 1,
	}
}

func TestHandleHealth(t *testing.T) {
	s := New(testAgent(), 0)
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	s.handleHealth(w, req)

	if w.Code != 200 {
		t.Errorf("health: expected 200, got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("health: expected status=ok, got %s", body["status"])
	}
}

func TestHandleModels(t *testing.T) {
	ag := testAgent()
	s := New(ag, 0)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.handleModels(w, req)

	if w.Code != 200 {
		t.Errorf("models: expected 200, got %d", w.Code)
	}
	var resp modelsResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Object != "list" {
		t.Errorf("models: expected object=list, got %s", resp.Object)
	}
	if len(resp.Data) == 0 {
		t.Error("models: expected at least 1 model (fallback to Client.Model)")
	}
}

func TestHandleChatInvalidMethod(t *testing.T) {
	s := New(testAgent(), 0)
	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	w := httptest.NewRecorder()
	s.handleChat(w, req)

	if w.Code != 405 {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleChatInvalidJSON(t *testing.T) {
	s := New(testAgent(), 0)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	s.handleChat(w, req)

	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleChatNoUserMessage(t *testing.T) {
	s := New(testAgent(), 0)
	body := `{"model":"test","messages":[{"role":"system","content":"sys"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleChat(w, req)

	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleChatBlockingError(t *testing.T) {
	// Agent with a client that will fail (invalid URL).
	ag := testAgent()
	ag.Client.BaseURL = "http://0.0.0.0:1/v1" // connection refused
	ag.Client.HTTP = &http.Client{Timeout: 100 * time.Millisecond}
	s := New(ag, 0)

	body := `{"model":"test","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleChat(w, req)

	if w.Code != 500 {
		t.Errorf("expected 500 on agent error, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestHandleChatConcurrentClientCopy(t *testing.T) {
	// Verify that the client copy in handleChat isolates the model name.
	ag := testAgent()
	originalModel := ag.Client.Model
	s := New(ag, 0)

	// Send a request with a different model. The client copy should get the
	// new model, but the original shared agent should keep its model.
	body := `{"model":"other-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	// We can't fully test the agent run (it would try to connect), but we
	// can verify the shared agent's model is unchanged after the request
	// handler runs. The handler will fail on ag.Run(), but the copy was
	// already made.
	s.handleChat(w, req)

	if ag.Client.Model != originalModel {
		t.Errorf("shared agent model changed: %s → %s", originalModel, ag.Client.Model)
	}
}

func TestHandleStreamContentType(t *testing.T) {
	ag := testAgent()
	s := New(ag, 0)

	body := `{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleChat(w, req)

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("stream: expected text/event-stream, got %s", ct)
	}
}

func TestHandleStreamSSEFormat(t *testing.T) {
	// Use a client that returns a fast error so we can check the SSE format.
	ag := testAgent()
	ag.Client.BaseURL = "http://0.0.0.0:1/v1"
	ag.Client.HTTP = &http.Client{Timeout: 100 * time.Millisecond}
	s := New(ag, 0)

	body := `{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleChat(w, req)

	scanner := bufio.NewScanner(w.Body)
	lines := 0
	hasDone := false
	hasData := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "data: [DONE]" {
			hasDone = true
		}
		if strings.HasPrefix(line, "data: ") {
			hasData = true
		}
		lines++
	}
	if lines == 0 {
		t.Error("stream: expected SSE output, got empty body")
	}
	if !hasData {
		t.Error("stream: expected data: lines, got none")
	}
	if !hasDone {
		t.Error("stream: expected [DONE] marker")
	}
}

func TestChatResponseFormat(t *testing.T) {
	// Verify the response struct marshals correctly.
	resp := chatResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "test-model",
		Choices: []chatChoice{{
			Index: 0,
			Message: chatMessage{
				Role:    "assistant",
				Content: "Hello!",
			},
			FinishReason: "stop",
		}},
	}
	data, _ := json.Marshal(resp)
	var back chatResponse
	json.Unmarshal(data, &back)
	if back.Choices[0].Message.Content != "Hello!" {
		t.Errorf("round-trip failed: got %s", back.Choices[0].Message.Content)
	}
}