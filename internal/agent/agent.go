// Package agent implements the Hiroto-style agent loop:
// system prompt (identity + tools + skills index + memory) -> LLM ->
// tool calls -> results -> loop until final answer.
package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hirotomasato/hiroto/internal/llm"
	"github.com/hirotomasato/hiroto/internal/memory"
	"github.com/hirotomasato/hiroto/internal/skills"
	"github.com/hirotomasato/hiroto/internal/tools"
)

// Event is streamed to the UI: text chunks, tool activity, and errors.
type Event struct {
	Type      string // "text", "tool_start", "tool_end", "turn", "error", "done"
	Text      string
	ToolName  string
	Duration  time.Duration
	Timestamp time.Time
}

const identityPrompt = `You are Hiroto, a personal AI agent running in the user's terminal.
You help with tasks by using tools: run shell commands, read/write files, search the web,
and manage persistent memory. Be direct, verify your work with tools, and never invent
tool output. When a relevant skill is listed in the index, load it with skill_view before
acting. Respond in the user's language.`

type Agent struct {
	Client   *llm.Client
	Reg      *tools.Registry
	Skills   []skills.Skill
	Memory   *memory.Store
	MaxTurns int
	Workdir  string

	CompressBudget    int // token budget before auto-compression triggers (0 = off)
	CompressKeepTurns int // recent turns to keep intact (default 6)
	RetryAttempts     int // auto-retry on provider errors (default 2)

	Messages []llm.Message
	Emit     func(Event) // UI callback
}

// SystemPrompt assembles the Hiroto-style system prompt.
func (a *Agent) SystemPrompt() string {
	var b strings.Builder
	b.WriteString(identityPrompt + "\n")

	if a.Workdir != "" {
		fmt.Fprintf(&b, "\nCurrent working directory: %s\n", a.Workdir)
	}

	if idx := a.skillIndexBlock(); idx != "" {
		b.WriteString("\n## Skills\n")
		b.WriteString("Before replying, check if a skill matches the task. If it does (or even partially), load it with skill_view and follow it.\n\n")
		b.WriteString("Available skills:\n\n")
		b.WriteString(idx)
		b.WriteString("\n")
	}

	if a.Memory != nil {
		if block := a.Memory.PromptBlock(); block != "" {
			b.WriteString("\n## Persistent memory\n")
			b.WriteString(block + "\n")
		}
	}
	if a.MaxTurns > 0 {
		fmt.Fprintf(&b, "\nExecution discipline: keep working until the task is complete. Use tools to verify results. Max %d tool turns per run.\n", a.MaxTurns)
	}
	return b.String()
}

func (a *Agent) skillIndexBlock() string {
	var b strings.Builder
	for _, s := range a.Skills {
		b.WriteString("- " + s.IndexLine() + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// skillAdapter adapts []skills.Skill to tools.SkillIndex.
type skillAdapter struct{ byName map[string]skills.Skill }

func newSkillAdapter(all []skills.Skill) skillAdapter {
	m := make(map[string]skills.Skill, len(all))
	for _, s := range all {
		m[s.Name] = s
	}
	return skillAdapter{byName: m}
}
func (a skillAdapter) Find(name string) (string, bool) {
	s, ok := a.byName[name]
	return s.Path, ok
}
func (a skillAdapter) Names() []string {
	out := make([]string, 0, len(a.byName))
	for n := range a.byName {
		out = append(out, n)
	}
	return out
}

// emit safely sends an event.
func (a *Agent) emit(e Event) {
	e.Timestamp = time.Now()
	if a.Emit != nil {
		a.Emit(e)
	}
}

// Run processes one user turn through the full tool loop.
// onText is called with incremental text when streaming is available.
func (a *Agent) Run(ctx context.Context, userText string, onText func(string)) (string, error) {
	if a.Messages == nil {
		a.Messages = []llm.Message{{Role: llm.RoleSystem, Content: a.SystemPrompt()}}
	}

	// Compress older messages if the conversation exceeds the token budget.
	if err := a.compress(ctx); err != nil {
		// Non-fatal: continue with uncompressed context.
		a.emit(Event{Type: "error", Text: "kompresi gagal: " + err.Error()})
	}

	a.Messages = append(a.Messages, llm.Message{Role: llm.RoleUser, Content: userText})

	toolDefs := llmTools(a.Reg)
	var final string

	for turn := 0; turn < maxInt(a.MaxTurns, 1); turn++ {
		// Try streaming first for visible typing; fall back to blocking call.
		// Auto-retry on transient provider errors.
		var assistant llm.Message
		var streamErr error
		for attempt := 0; attempt <= a.RetryAttempts; attempt++ {
			if attempt > 0 {
				a.emit(Event{Type: "error", Text: fmt.Sprintf("retry %d/%d…", attempt, a.RetryAttempts)})
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			if onText != nil {
				assistant, streamErr = a.Client.Stream(ctx, a.Messages, toolDefs, onText)
			}
			if streamErr != nil {
				select {
				case <-ctx.Done():
					return final, ctx.Err()
				default:
				}
				assistant, streamErr = a.Client.Chat(ctx, a.Messages, toolDefs)
				if streamErr != nil {
					if attempt < a.RetryAttempts {
						continue
					}
					return final, streamErr
				}
				if onText != nil {
					if s, ok := assistant.Content.(string); ok && s != "" {
						onText(s)
					}
				}
			}
			break
		}
		select {
		case <-ctx.Done():
			return final, ctx.Err()
		default:
		}

		// No tool calls -> final answer.
		if len(assistant.ToolCalls) == 0 {
			if s, ok := assistant.Content.(string); ok {
				final = s
			}
			a.Messages = append(a.Messages, assistant)
			return final, nil
		}

		// Preserve assistant message with tool calls, then execute each.
		a.Messages = append(a.Messages, assistant)
		for _, tc := range assistant.ToolCalls {
			name := tc.Function.Name
			a.emit(Event{Type: "tool_start", ToolName: name})
			start := time.Now()

			var result tools.Result
			tool, ok := a.Reg.Get(name)
			if !ok {
				result = tools.Result{Output: fmt.Sprintf("unknown tool %q", name), IsError: true}
			} else {
				result = tool.Exec(ctx, tools.JSONArgs(tc.Function.Arguments))
			}
			a.emit(Event{Type: "tool_end", ToolName: name, Duration: time.Since(start), Text: result.Output})

			content := result.Output
			if result.IsError {
				content = "ERROR: " + content
			}
			a.Messages = append(a.Messages, llm.Message{
				Role: llm.RoleTool, ToolCallID: tc.ID, Name: name, Content: content,
			})
		}
	}
	// Turn budget exhausted: tell the model to wrap up.
	wrap := llm.Message{Role: llm.RoleUser, Content: "Turn budget reached. Summarize what was accomplished and what remains, without further tool calls."}
	a.Messages = append(a.Messages, wrap)
	msg, err := a.Client.Chat(ctx, a.Messages, nil)
	if err != nil {
		return final, err
	}
	if s, ok := msg.Content.(string); ok {
		final = s
	}
	a.Messages = append(a.Messages, msg)
	return final, nil
}

func llmTools(r *tools.Registry) []llm.Tool {
	var out []llm.Tool
	for _, def := range r.LLMTools() {
		out = append(out, llm.Tool{
			Type: def.Type,
			Function: llm.ToolDefinition{
				Name:        def.Function.Name,
				Description: def.Function.Description,
				Parameters:  def.Function.Parameters,
			},
		})
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
