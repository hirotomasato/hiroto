// Package agent implements the Hiroto-style agent loop:
// system prompt (identity + tools + skills index + memory) -> LLM ->
// tool calls -> results -> loop until final answer.
package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	ToolArgs  string // raw JSON args (tool_start) — UI renders a short summary
	Duration  time.Duration
	Timestamp time.Time
}

const identityPrompt = `You are Hiroto, a skilled software engineer in the user's terminal. Write, review, debug, and deploy code using real tools. Persistent, thorough, self-verifying. 

## Core
- Keep working until done. Use tools instead of telling. Verify writes by reading back.
- Fix root causes; check sibling paths for the same flaw.
- Never guess — use tools when context is missing.
- Match project conventions; no drive-by refactors. Edit with patch, not full rewrites.

## Tools
- Use terminal for builds, tests, git. Batch independent calls together.
- Set the "activity" arg on every call: short present-tense ("Running tests", "Reading agent.go").
- Load relevant skills with skill_view. Patch outdated skills immediately.

## Todo
- 3+ steps → todo(action="write") once, then advance by id. One in_progress max. Clear when done.

## Safety
- Never read, print, or commit secrets (env, keys, tokens).
- Confirm scope before destructive commands.

Respond in the user's language. Be direct, concise, and verify with tools.`

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

	// Ask lists tools that require interactive approval before running. Empty
	// (default) = no prompts — tools run freely (OMP parity).
	Ask []string
	// SkipPermissions bypasses the approval gate entirely (--dangerously-skip).
	SkipPermissions bool
	// PlanMode restricts the agent to read-only tools (no writes/execution)
	// while a /plan investigation is running. Cleared when the plan is
	// approved and the real turn begins.
	PlanMode bool

	Messages  []llm.Message
	Emit      func(Event) // UI callback
	SteerCh   chan string // injected mid-turn: read after each tool call
	Goal      string      // standing goal across turns
	Reasoning string      // reasoning effort level
}

// SystemPrompt assembles the Hiroto-style system prompt.
func (a *Agent) SystemPrompt() string {
	var b strings.Builder
	b.WriteString(identityPrompt + "\n")

	if a.Workdir != "" {
		fmt.Fprintf(&b, "\nCurrent working directory: %s\n", a.Workdir)
		// Inject project context files (AGENTS.md, CLAUDE.md, .cursorrules, .hermes.md).
		for _, name := range []string{"AGENTS.md", "CLAUDE.md", ".cursorrules", ".hermes.md"} {
			path := filepath.Join(a.Workdir, name)
			if fi, err := os.Stat(path); err != nil || fi.Size() > 1<<20 {
				continue // skip missing or >1MB files
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			content := string(data)
			if len(content) > 4000 {
				content = content[:4000] + "\n... (truncated)"
			}
			fmt.Fprintf(&b, "\n## Project context (%s)\n%s\n", name, content)
		}
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
	if a.Goal != "" {
		fmt.Fprintf(&b, "\n## Standing goal\n%s\nWork toward this goal across turns. Do not lose sight of it.\n", a.Goal)
	}
	if a.Reasoning != "" {
		fmt.Fprintf(&b, "\n## Reasoning effort\nSet to %s. Use this level of reasoning depth.\n", a.Reasoning)
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
				assistant, streamErr = a.Client.Stream(ctx, a.Messages, toolDefs, onText, func(s string) {
					a.emit(Event{Type: "reasoning", Text: s})
				})
			} else {
				assistant, streamErr = a.Client.Chat(ctx, a.Messages, toolDefs)
			}
			if streamErr != nil {
				select {
				case <-ctx.Done():
					return final, ctx.Err()
				default:
				}
				// Stream failed; try Chat as a fallback (only when
				// we were streaming — the else branch already did Chat).
				if onText != nil {
					assistant, streamErr = a.Client.Chat(ctx, a.Messages, toolDefs)
				}
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

		// Check for steer injection before running the batch.
		select {
		case msg := <-a.SteerCh:
			a.Messages = append(a.Messages, llm.Message{Role: llm.RoleUser, Content: msg})
			a.emit(Event{Type: "error", Text: "steer: " + msg})
			return a.continueRun(ctx, onText)
		default:
		}

		// Run the tool calls (parallel when >1, order-preserving results).
		a.Messages = append(a.Messages, a.executeToolCalls(ctx, assistant.ToolCalls)...)
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

// riskyTools are tools that mutate state or run code and so require approval
// unless explicitly allowlisted or --dangerously-skip-permissions is set.
var riskyTools = map[string]bool{
	"patch": true, "write_file": true, "terminal": true,
	"execute_code": true, "execute_python": true, "ast_edit": true,
	"process": true, "delegate_task": true, "cronjob": true, "eval": true,
	"skill_manage": true, "smart_pipe": true,
}

// isRisky reports whether a tool needs approval before running.
func isRisky(name string) bool { return riskyTools[name] }

// approveTool decides whether a tool call may run without asking. Default is
// permissive (OMP parity): tools run freely. A tool requires approval only if
// the user listed it in the Ask config. SkipPermissions ignores Ask entirely.
// PlanMode refuses all risky tools (investigation stays read-only).
func (a *Agent) approveTool(name string) bool {
	if a.PlanMode {
		return !isRisky(name)
	}
	if a.SkipPermissions {
		return true
	}
	for _, t := range a.Ask {
		if t == name {
			return false
		}
	}
	return true
}

// executeToolCalls runs a batch of tool calls and returns their tool-result
// messages in the SAME order as the calls (required: each result must follow
// its tool_call_id). A single call runs inline; multiple independent calls run
// concurrently — mirroring how Hermes fans out independent tool calls in one
// turn. tool_start/tool_end events are still emitted per call so the UI shows
// every activity line.
func (a *Agent) executeToolCalls(ctx context.Context, calls []llm.ToolCall) []llm.Message {
	out := make([]llm.Message, len(calls))
	run := func(i int, tc llm.ToolCall) {
		name := tc.Function.Name
		a.emit(Event{Type: "tool_start", ToolName: name, ToolArgs: tc.Function.Arguments})
		start := time.Now()

		var result tools.Result
		if !a.approveTool(name) {
			// Risky tool not allowlisted (and permissions not skipped): refuse so
			// the model can adjust instead of mutating state without consent.
			result = tools.Result{
				Output:  fmt.Sprintf("tool %q butuh persetujuan — ketik /allow %s untuk mengizinkan di sesi ini, atau hapus dari `ask:` di config.yaml", name, name),
				IsError: true,
			}
		} else if tool, ok := a.Reg.Get(name); !ok {
			result = tools.Result{Output: fmt.Sprintf("unknown tool %q", name), IsError: true}
		} else {
			result = tool.Exec(ctx, tools.JSONArgs(tc.Function.Arguments))
		}
		a.emit(Event{Type: "tool_end", ToolName: name, ToolArgs: tc.Function.Arguments, Duration: time.Since(start), Text: result.Output})

		content := tools.RedactSecrets(result.Output)
		if result.IsError {
			content = "ERROR: " + content
		}
		out[i] = llm.Message{Role: llm.RoleTool, ToolCallID: tc.ID, Name: name, Content: content}
	}

	// Single call: run inline (no goroutine overhead, preserves simple path).
	if len(calls) <= 1 {
		for i, tc := range calls {
			run(i, tc)
		}
		return out
	}

	// Multiple calls: run concurrently. a.emit is guarded by the caller's
	// stream channel (chan send is safe from multiple goroutines) and each
	// goroutine writes only its own out[i] slot, so no shared-state races.
	var wg sync.WaitGroup
	for i, tc := range calls {
		wg.Add(1)
		go func(i int, tc llm.ToolCall) {
			defer wg.Done()
			run(i, tc)
		}(i, tc)
	}
	wg.Wait()
	return out
}

// continueRun continues the agent loop without adding a new user message.
// Used by steer injection: the steer message is already appended to Messages.
func (a *Agent) continueRun(ctx context.Context, onText func(string)) (string, error) {
	toolDefs := llmTools(a.Reg)
	var final string

	for turn := 0; turn < maxInt(a.MaxTurns, 1); turn++ {
		select {
		case <-ctx.Done():
			return final, ctx.Err()
		default:
		}

		var assistant llm.Message
		var streamErr error
		for attempt := 0; attempt <= a.RetryAttempts; attempt++ {
			if attempt > 0 {
				a.emit(Event{Type: "error", Text: fmt.Sprintf("retry %d/%d…", attempt, a.RetryAttempts)})
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			if onText != nil {
				assistant, streamErr = a.Client.Stream(ctx, a.Messages, toolDefs, onText, func(s string) {
					a.emit(Event{Type: "reasoning", Text: s})
				})
			} else {
				assistant, streamErr = a.Client.Chat(ctx, a.Messages, toolDefs)
			}
			if streamErr != nil {
				select {
				case <-ctx.Done():
					return final, ctx.Err()
				default:
				}
				// Stream failed; try Chat as a fallback.
				if onText != nil {
					assistant, streamErr = a.Client.Chat(ctx, a.Messages, toolDefs)
				}
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

		if len(assistant.ToolCalls) == 0 {
			if s, ok := assistant.Content.(string); ok {
				final = s
			}
			a.Messages = append(a.Messages, assistant)
			return final, nil
		}

		a.Messages = append(a.Messages, assistant)

		// Check for steer injection before running the batch.
		select {
		case msg := <-a.SteerCh:
			a.Messages = append(a.Messages, llm.Message{Role: llm.RoleUser, Content: msg})
			a.emit(Event{Type: "error", Text: "steer: " + msg})
			return a.continueRun(ctx, onText)
		default:
		}

		a.Messages = append(a.Messages, a.executeToolCalls(ctx, assistant.ToolCalls)...)
	}
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
