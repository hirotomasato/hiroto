// Package api implements an OpenAI-compatible HTTP API server so Hiroto
// can be used as a backend for tools like VS Code (Continue, Cline), Aider,
// Open WebUI, and any script that speaks the OpenAI chat-completions format.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/hirotomasato/hiroto/internal/agent"
	"github.com/hirotomasato/hiroto/internal/llm"
)

// Server wraps a Hiroto agent behind an OpenAI-compatible HTTP API.
type Server struct {
	ag   *agent.Agent
	port int
	srv  *http.Server
}

// New creates an API server bound to the given port.
func New(ag *agent.Agent, port int) *Server {
	return &Server{ag: ag, port: port}
}

// Start begins listening. Blocks until the server is stopped.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.handleChat)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/health", s.handleHealth)

	s.srv = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 10 * time.Minute, // long for streaming
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("[api] OpenAI-compatible server on http://localhost:%d", s.port)
	log.Printf("[api] endpoints: /v1/chat/completions, /v1/models, /health")
	return s.srv.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv != nil {
		return s.srv.Shutdown(ctx)
	}
	return nil
}

// ---- OpenAI-compatible request/response types ----

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []chatChoice   `json:"choices"`
	Usage   *chatUsage     `json:"usage,omitempty"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message,omitempty"`
	Delta        chatMessage `json:"delta,omitempty"`
	FinishReason string      `json:"finish_reason"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type modelsResponse struct {
	Object string        `json:"object"`
	Data   []modelEntry  `json:"data"`
}

type modelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ---- handlers ----

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	models, _ := s.ag.Client.ListModels(ctx)
	if len(models) == 0 {
		models = []string{s.ag.Client.Model}
	}

	var entries []modelEntry
	for _, m := range models {
		entries = append(entries, modelEntry{
			ID: m, Object: "model", Created: 0, OwnedBy: "hiroto",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(modelsResponse{Object: "list", Data: entries})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":{"message":"invalid request: %v"}}`, err), http.StatusBadRequest)
		return
	}

	// Convert external messages to internal llm.Messages.
	messages := []llm.Message{{Role: llm.RoleSystem, Content: s.ag.SystemPrompt()}}
	for _, m := range req.Messages {
		role := llm.RoleUser
		switch m.Role {
		case "system":
			role = llm.RoleSystem
		case "assistant":
			role = llm.RoleAssistant
		}
		messages = append(messages, llm.Message{Role: role, Content: m.Content})
	}

	// Use the last user message as the prompt.
	var userText string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser {
			userText, _ = messages[i].Content.(string)
			break
		}
	}
	if userText == "" {
		http.Error(w, `{"error":{"message":"no user message found"}}`, http.StatusBadRequest)
		return
	}

	// Create a shallow agent copy for this request, including the client
	// so concurrent requests don't race on the shared model name.
	ag := *s.ag
	clientCopy := *s.ag.Client
	ag.Client = &clientCopy
	ag.Messages = messages

	if req.Stream {
		s.handleStream(w, r, &ag, userText, req.Model)
	} else {
		s.handleBlocking(w, r, &ag, userText, req.Model)
	}
}

func (s *Server) handleBlocking(w http.ResponseWriter, r *http.Request, ag *agent.Agent, userText, model string) {
	if model != "" {
		ag.Client.Model = model
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	answer, err := ag.Run(ctx, userText, nil)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": err.Error()},
		})
		return
	}

	resp := chatResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   ag.Client.Model,
		Choices: []chatChoice{{
			Index: 0,
			Message: chatMessage{
				Role:    "assistant",
				Content: answer,
			},
			FinishReason: "stop",
		}},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request, ag *agent.Agent, userText, model string) {
	if model != "" {
		ag.Client.Model = model
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	chunkID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()

	// Stream chunks as they arrive.
	_, err := ag.Run(ctx, userText, func(chunk string) {
		data, _ := json.Marshal(map[string]any{
			"id":      chunkID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   ag.Client.Model,
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]string{"content": chunk},
			}},
		})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	})

	// Send [DONE].
	if err != nil {
		data, _ := json.Marshal(map[string]any{
			"id":      chunkID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   ag.Client.Model,
			"choices": []map[string]any{{
				"index":         0,
				"delta":         map[string]string{},
				"finish_reason": "error",
			}},
		})
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// ---- one-shot helpers for CLI mode ----

// PrintBanner prints the API server startup banner.
func PrintBanner(port int, model string) {
	fmt.Printf("◆ Hiroto API Server\n")
	fmt.Printf("   endpoint: http://localhost:%d/v1\n", port)
	fmt.Printf("   model:    %s\n", model)
	fmt.Printf("\n")
	fmt.Printf("   curl http://localhost:%d/v1/chat/completions \\\n", port)
	fmt.Printf("     -H \"Content-Type: application/json\" \\\n")
	fmt.Printf("     -d '{\"model\":\"%s\",\"messages\":[{\"role\":\"user\",\"content\":\"halo\"}]}'\n", model)
	fmt.Printf("\n")
}