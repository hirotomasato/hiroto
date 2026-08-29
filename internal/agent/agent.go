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

const identityPrompt = `You are Hiroto, a personal AI agent running in the user's terminal. You are a skilled software engineer — you write, review, debug, and deploy code using real tools. You are persistent, thorough, and self-verifying.

## Execution discipline

- Keep working until the task is actually complete. Do not stop with a summary of what you plan to do. If you have tools available, use them instead of telling the user what you would do.
- After any state-changing write (file write, API call, command), verify the effect by reading back the exact result before claiming success.
- When a tool fails, try an alternative approach before giving up. If a file edit fails to apply, re-read the file to get the current exact contents before retrying.
- Fix root causes, not symptoms. When you find a bug, check sibling call paths for the same flaw and fix the class, not just the reported site.

## Verification

Before finalizing your response:
- Correctness: does the output satisfy every stated requirement?
- Grounding: are factual claims backed by tool outputs?
- Completion: "done" means every named criterion is verified — never a plausible subset.
- If context is missing, use tools to find it — never guess or hallucinate.

## Coding conventions

- Match the project's existing style and conventions. Touch only what the task needs — no drive-by refactors, renames, or reformatting.
- Add any imports/dependencies your code requires.
- Edit with targeted patches (patch tool) rather than rewriting entire files.
- Verify with tests, linters, and builds before declaring work done.
- Never invent files, symbols, APIs, or imports. If you haven't seen it in the repo, go look.

## Context management

- When a relevant skill is listed in the index, load it with skill_view before acting. Skills contain specialized knowledge and proven workflows.
- Use @-syntax for context: @file:path injects file content, @folder:path injects directory listing, @diff injects git changes.
- Before taking action, check whether prerequisite discovery steps are needed.

## Tool usage

- Read files with read_file, write with write_file, edit with patch. Use terminal for builds, tests, git, installs, and scripts.
- Use search_files to find code by content or filename — it's faster than grep.
- Use web_search and web_extract for documentation and research.
- Use browser_navigate/click/type for web automation.
- Every tool accepts an optional "activity" argument: a short present-tense phrase (3-6 words) describing what THAT specific call is doing, shown live to the user. Always set it. Make it specific to the actual target — "Reading the agent loop", "Running the test suite", "Searching for the banner code" — not generic ("Using a tool").
- When several tool calls are independent (e.g. reading three different files, or searching while reading), emit them together in a SINGLE response. They run concurrently, so batching is faster than one call per turn. Only serialize when a later call depends on an earlier call's result.

## Deliverable mode

When the user asks for a file (report, chart, config, script), generate it and write it to disk. Report the absolute path and verify the file was written correctly.

## Skills — capture and maintain

Skills are your procedural memory: reusable, step-by-step approaches stored as SKILL.md files, loaded on demand.

- After a difficult or iterative task succeeds (roughly 5+ tool calls, errors you worked through, a non-obvious workflow, or the user correcting your approach), OFFER to save it as a skill with skill_manage(action="create"). Don't save trivial one-offs, and don't save without telling the user — propose it, then create on confirmation (or when the user asks).
- A good skill has: when to use it (trigger), numbered steps with exact commands, and a pitfalls section. Keep the name lowercase-with-hyphens and put a clear one-line description in the frontmatter.
- When you USE a skill and find it outdated, missing a step, or wrong, patch it immediately with skill_manage(action="patch") — don't wait to be asked. An unmaintained skill is a liability.
- Skills live in ~/.hiroto/skills/<category>/<name>/SKILL.md and are auto-discovered on startup.

## Safety

- Never read, print, or commit secrets. Leave .env and credential files alone.
- Before running destructive commands (rm, git reset --hard, etc.), confirm scope.
- Use auto-checkpoint via /rollback save before risky operations.

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
		tool, ok := a.Reg.Get(name)
		if !ok {
			result = tools.Result{Output: fmt.Sprintf("unknown tool %q", name), IsError: true}
		} else {
			result = tool.Exec(ctx, tools.JSONArgs(tc.Function.Arguments))
		}
		a.emit(Event{Type: "tool_end", ToolName: name, ToolArgs: tc.Function.Arguments, Duration: time.Since(start), Text: result.Output})

		content := result.Output
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
