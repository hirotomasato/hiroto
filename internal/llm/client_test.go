package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing auth header")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "hello"}, "finish_reason": "stop"},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", "test-model")
	msg, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if msg.Content != "hello" {
		t.Errorf("Content = %q, want %q", msg.Content, "hello")
	}
}

func TestChatProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "bad request", "type": "invalid"},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "", "test-model")
	_, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestChatEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer srv.Close()

	c := New(srv.URL, "", "test-model")
	_, err := c.Chat(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "model-b"}, {"id": "model-a"}},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "", "test-model")
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0] != "model-a" || models[1] != "model-b" {
		t.Errorf("models not sorted: %v", models)
	}
}

func TestNewTrimsTrailingSlash(t *testing.T) {
	c := New("http://example.com/v1/", "key", "model")
	if c.BaseURL != "http://example.com/v1" {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, "http://example.com/v1")
	}
}

func TestThinkFilterWholeBlock(t *testing.T) {
	var f thinkFilter
	answer, reasoning := f.feed("<think>reasoning here</think>answer")
	if answer != "answer" {
		t.Errorf("answer = %q, want %q", answer, "answer")
	}
	if reasoning != "reasoning here" {
		t.Errorf("reasoning = %q, want %q", reasoning, "reasoning here")
	}
}

func TestThinkFilterSplitAcrossChunks(t *testing.T) {
	var f thinkFilter
	var out strings.Builder
	for _, chunk := range []string{"<thi", "nk>hidden ", "stuff</thi", "nk>ok", " then"} {
		a, _ := f.feed(chunk)
		out.WriteString(a)
	}
	a, _ := f.flush()
	out.WriteString(a)
	if out.String() != "ok then" {
		t.Errorf("got %q, want %q", out.String(), "ok then")
	}
}

func TestThinkFilterNoThink(t *testing.T) {
	var f thinkFilter
	answer, reasoning := f.feed("plain text <b>html</b> here")
	if answer != "plain text <b>html</b> here" {
		t.Errorf("answer = %q", answer)
	}
	if reasoning != "" {
		t.Errorf("reasoning should be empty, got %q", reasoning)
	}
}

func TestThinkFilterMultipleBlocks(t *testing.T) {
	var f thinkFilter
	answer, reasoning := f.feed("<think>a</think>x<think>b</think>y")
	if answer != "xy" {
		t.Errorf("answer = %q, want %q", answer, "xy")
	}
	if reasoning != "ab" {
		t.Errorf("reasoning = %q, want %q", reasoning, "ab")
	}
}

func TestThinkFilterTextBeforeTag(t *testing.T) {
	var f thinkFilter
	answer, reasoning := f.feed("hello <think>secret plan</think> world")
	if answer != "hello  world" {
		t.Errorf("answer = %q", answer)
	}
	if reasoning != "secret plan" {
		t.Errorf("reasoning = %q", reasoning)
	}
}
