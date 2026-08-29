package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func registerVision(r *Registry, opts Options) {
	r.Register(&Tool{
		Name:        "vision_analyze",
		Description: "Read and analyze an image file (PNG, JPEG, GIF, WebP). Sends the image to the LLM for description. Requires a vision-capable model.",
		Parameters:  mustJSON(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the image file"},"question":{"type":"string","description":"Optional: what to look for in the image"}},"required":["path"]}`),
		Exec: func(ctx context.Context, args map[string]any) Result {
			return visionExec(ctx, args, opts)
		},
	})
}

func visionExec(ctx context.Context, args map[string]any, opts Options) Result {
	path, _ := args["path"].(string)
	question, _ := args["question"].(string)
	if path == "" {
		return Result{Output: "missing path", IsError: true}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Output: fmt.Sprintf("cannot read image: %v", err), IsError: true}
	}
	mime := "image/png"
	switch {
	case strings.HasSuffix(strings.ToLower(path), ".jpg"), strings.HasSuffix(strings.ToLower(path), ".jpeg"):
		mime = "image/jpeg"
	case strings.HasSuffix(strings.ToLower(path), ".gif"):
		mime = "image/gif"
	case strings.HasSuffix(strings.ToLower(path), ".webp"):
		mime = "image/webp"
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	url := fmt.Sprintf("data:%s;base64,%s", mime, b64)

	if opts.LLMClient == nil {
		return Result{Output: fmt.Sprintf("IMAGE_READY\nmime: %s\nsize: %d bytes\n%s", mime, len(data), url), IsError: false}
	}

	prompt := question
	if prompt == "" {
		prompt = "Describe this image in detail. What do you see?"
	}
	// Call LLM with vision
	content := fmt.Sprintf(`[IMAGE: %s]\n%s`, url, prompt)
	answer, err := opts.LLMClient.Chat(ctx, []LLMMessage{
		{Role: "user", Content: content},
	})
	if err != nil {
		return Result{Output: fmt.Sprintf("vision error: %v", err), IsError: true}
	}
	return Result{Output: answer}
}