package tools

import (
	"context"
)

// ClarifyChan is set by the TUI to enable interactive user questions.
// When nil (headless/one-shot), clarify returns an error instead.
var ClarifyChan chan ClarifyRequest

type ClarifyRequest struct {
	Question string
	Response chan string
}

func registerClarify(r *Registry) {
	r.Register(&Tool{
		Name:        "clarify",
		Description: "Ask the user a question when you need clarification or a decision. Use when the task is ambiguous and you need user input. Supports multiple-choice or open-ended questions.",
		Parameters: mustJSON(`{"type":"object","properties":{"question":{"type":"string","description":"The question to ask the user"},"choices":{"type":"array","items":{"type":"string"},"description":"Optional list of choices for the user"}},"required":["question"]}`),
		Exec: clarifyExec,
	})
}

func clarifyExec(ctx context.Context, args map[string]any) Result {
	question, _ := args["question"].(string)
	if question == "" {
		return Result{Output: "missing question", IsError: true}
	}
	if ClarifyChan == nil {
		return Result{Output: "CLARIFY: " + question + "\n[headless mode — cannot ask user]", IsError: true}
	}
	respCh := make(chan string, 1)
	select {
	case ClarifyChan <- ClarifyRequest{Question: question, Response: respCh}:
	case <-ctx.Done():
		return Result{Output: "clarify cancelled", IsError: true}
	}
	select {
	case answer := <-respCh:
		return Result{Output: "USER ANSWER: " + answer}
	case <-ctx.Done():
		return Result{Output: "clarify timed out", IsError: true}
	}
}