package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Message types follow the OpenAI chat-completion schema (what Hiroto's providers speak).
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role       `json:"role"`
	Content    any        `json:"content,omitempty"` // string or []ContentPart
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ContentPart struct {
	Type     string `json:"type"` // "text" | "image_url"
	Text     string `json:"text,omitempty"`
	ImageURL any    `json:"image_url,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string
	} `json:"function"`
}

type Tool struct {
	Type     string         `json:"type"` // "function"
	Function ToolDefinition `json:"function"`
}

type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"` // JSON schema
}

type request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	Stream      bool      `json:"stream"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
}

type response struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Client is an OpenAI-compatible chat client (works with any provider Hiroto-style).
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

func New(baseURL, apiKey, model string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model, HTTP: &http.Client{}}
}

// Chat performs a non-streaming completion. Streaming fallback handled by Stream().
func (c *Client) Chat(ctx context.Context, messages []Message, tools []Tool) (Message, error) {
	reqBody, _ := json.Marshal(request{Model: c.Model, Messages: messages, Tools: tools, Temperature: 0.4})
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("provider unreachable: %w", err)
	}
	defer resp.Body.Close()
	var r response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return Message{}, fmt.Errorf("bad provider response (HTTP %d): %w", resp.StatusCode, err)
	}
	if r.Error != nil {
		return Message{}, fmt.Errorf("provider error: %s", r.Error.Message)
	}
	if len(r.Choices) == 0 {
		return Message{}, fmt.Errorf("provider returned no choices (HTTP %d)", resp.StatusCode)
	}
	return r.Choices[0].Message, nil
}

// StreamDelta is one incremental piece of an assistant reply.
type StreamDelta struct {
	Content  string    // text chunk
	ToolCall *ToolCall // complete tool call (assembled by caller)
	Done     bool
}

// Stream performs an SSE-streaming completion; on stream failure callers should fall back to Chat.
func (c *Client) Stream(ctx context.Context, messages []Message, tools []Tool, onText func(string)) (Message, error) {
	reqBody, _ := json.Marshal(request{Model: c.Model, Messages: messages, Tools: tools, Stream: true, Temperature: 0.4})
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()

	// Non-200 or non-SSE content type -> let caller fall back to non-streaming.
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return Message{}, fmt.Errorf("no stream (HTTP %d)", resp.StatusCode)
	}

	var content strings.Builder
	var toolCalls []ToolCall // assembled incrementally
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 256*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		d := chunk.Choices[0].Delta
		if d.Content != "" {
			content.WriteString(d.Content)
			if onText != nil {
				onText(d.Content)
			}
		}
		for _, tc := range d.ToolCalls {
			for len(toolCalls) <= tc.Index {
				toolCalls = append(toolCalls, ToolCall{Type: "function"})
			}
			if tc.ID != "" {
				toolCalls[tc.Index].ID = tc.ID
			}
			if tc.Function.Name != "" {
				toolCalls[tc.Index].Function.Name = tc.Function.Name
			}
			toolCalls[tc.Index].Function.Arguments += tc.Function.Arguments
		}
	}
	if err := sc.Err(); err != nil {
		return Message{}, err
	}
	msg := Message{Role: RoleAssistant}
	if s := content.String(); s != "" {
		msg.Content = s
	}
	msg.ToolCalls = toolCalls
	return msg, nil
}
