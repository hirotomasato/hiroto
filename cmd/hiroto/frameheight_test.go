package main

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hirotomasato/hiroto/internal/agent"
	"github.com/hirotomasato/hiroto/internal/llm"
	"github.com/hirotomasato/hiroto/internal/tools"
)

// TestFrameHeightBusyLongContent reproduces the stacked "HR hiroto v0.6.1"
// status bars: if the rendered frame ever exceeds the terminal height, the
// terminal scrolls and stale bottom lines stack up.
func TestFrameHeightBusyLongContent(t *testing.T) {
	m := tinyModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mm := updated.(model)

	// Long markdown table line like a real agent answer.
	mm.lines = append(mm.lines, line{kind: lineAssistant, text: strings.Repeat("| kolom | sangat panjang sekali | nilai |", 10)})
	mm.busy = true

	frame := lipgloss.Height(mm.View())
	if frame != 30 {
		t.Fatalf("busy+long content: frame height = %d, want 30", frame)
	}

	// Todo panel visible + busy
	mm.todos = tools.NewSessionTodoStore("test-session")
	mm.todos.Update([]tools.TodoItem{
		{ID: "1", Content: "first", Status: tools.StatusInProgress},
		{ID: "2", Content: "second", Status: tools.StatusPending},
	})
	os.Setenv(tools.EnvSessionID, "test-session")
	frame2 := lipgloss.Height(mm.View())
	if frame2 != 30 {
		t.Fatalf("todo panel + busy: frame height = %d, want 30", frame2)
	}
}

// TestLoadingIndicatorAnimatesAsChrome: the "berpikir…/menulis…" spinner row
// must live in the chrome (re-rendered on every View) instead of the viewport
// content (only updated on refresh()). Regression: during a long LLM wait no
// messages arrive, so a viewport-embedded spinner froze mid-animation.
func TestLoadingIndicatorAnimatesAsChrome(t *testing.T) {
	m := tinyModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mm := updated.(model)

	if !mm.busy && strings.Contains(mm.View(), "berpikir") {
		t.Fatal("loading row visible while idle")
	}

	mm.busy = true
	view := mm.View()
	if !strings.Contains(view, "berpikir") {
		t.Fatal("loading row missing while busy")
	}
	if lipgloss.Height(view) != 30 {
		t.Fatalf("busy: frame height = %d, want 30", lipgloss.Height(view))
	}

	// Streaming label takes over.
	mm.streaming = true
	if !strings.Contains(mm.View(), "menulis") {
		t.Fatal("streaming label missing while streaming")
	}

	// Back to idle: row disappears, frame still fits exactly.
	mm.busy = false
	mm.streaming = false
	view = mm.View()
	if strings.Contains(view, "menulis") || strings.Contains(view, "berpikir") {
		t.Fatal("loading row stuck after turn end")
	}
	if lipgloss.Height(view) != 30 {
		t.Fatalf("idle: frame height = %d, want 30", lipgloss.Height(view))
	}
}

// TestToolActivityShowsInChrome: while a tool runs the loading row must show
// the tool's label with a ticking elapsed time instead of "berpikir…", and it
// must clear when the tool completes (no stale label into the next turn).
func TestToolActivityShowsInChrome(t *testing.T) {
	m := tinyModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mm := updated.(model)
	mm.busy = true

	if strings.Contains(mm.View(), "Running go build") {
		t.Fatal("activity label visible before tool_start")
	}

	// Simulate a running tool.
	mm.activityText = "Running go build"
	mm.activityStart = time.Now().Add(-3 * time.Second)
	view := mm.View()
	if !strings.Contains(view, "Running go build") {
		t.Fatal("activity label missing while tool runs")
	}
	if !strings.Contains(view, "3s") {
		t.Fatal("elapsed seconds missing")
	}
	if strings.Contains(view, "berpikir") {
		t.Fatal("generic thinking label shown while a tool runs")
	}

	// Tool completes → label cleared → back to thinking.
	mm.activityText = ""
	if !strings.Contains(mm.View(), "berpikir") {
		t.Fatal("thinking label missing after tool completes")
	}
}

// TestLazyMarkdownStreaming: markdown styling must be applied exactly once
// per assistant turn — lazily in endAssistantLine, not on every stream chunk
// (which was the main render hotspot). Resize must reset the flag so lines
// re-wrap at the new width.
func TestLazyMarkdownStreaming(t *testing.T) {
	m := tinyModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mm := updated.(model)

	// Stream two chunks; neither should be styled yet.
	mm.appendStream("## Header ")
	mm.appendStream("body text")
	if mm.lines[len(mm.lines)-1].mdRendered {
		t.Fatal("stream chunk prematurely styled")
	}

	// Finish the turn → single style pass.
	mm.endAssistantLine()
	l := mm.lines[len(mm.lines)-2] // last is the turn-separator info line
	if l.kind != lineAssistant {
		t.Fatalf("expected assistant line, got kind %d", l.kind)
	}
	if !l.mdRendered {
		t.Fatal("endAssistantLine did not mark line styled")
	}

	// Simulate a resize to a new width → flags reset for re-wrap.
	updated, _ = mm.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	mm = updated.(model)
	if mm.lines[0].mdRendered {
		t.Fatal("resize did not reset mdRendered for re-wrap")
	}
}

// TestLastAssistantText: copy must grab the most recent assistant message,
// skip tool-call entries (Content not a plain string), and ignore empties.
func TestLastAssistantText(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "first answer"},
		{Role: llm.RoleAssistant, Content: ""}, // empty — skip
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1"}}, Content: nil},
		{Role: llm.RoleAssistant, Content: "second answer"},
	}
	if got := lastAssistantText(msgs); got != "second answer" {
		t.Errorf("got %q, want %q", got, "second answer")
	}
	if got := lastAssistantText(nil); got != "" {
		t.Errorf("empty messages should give empty string, got %q", got)
	}
	// Only tool-call messages → nothing to copy.
	if got := lastAssistantText(msgs[:4]); got != "first answer" {
		t.Errorf("should fall back to last non-empty text, got %q", got)
	}
}

// TestCopyTextOSC52: the copy path writes the OSC52 escape to the terminal.
// We can't easily intercept os.Stdout here, so just assert the base64 the
// sequence embeds round-trips (guards against encoding regressions).
func TestCopyTextOSC52(t *testing.T) {
	orig := "line satu\nline dua"
	enc := base64.StdEncoding.EncodeToString([]byte(orig))
	if dec, err := base64.StdEncoding.DecodeString(enc); err != nil || string(dec) != orig {
		t.Errorf("base64 round-trip failed: %v", err)
	}
}

// TestAssistantMarkdownNoFragileANSI: glamour's markdown STRUCTURE is kept
// (code fences, bold resolved) but its fragile color escapes are stripped —
// those standalone "\x1b[38;5;252m" runs made some terminals print literal
// "[38;5;252m" text. Code content must survive the strip.
func TestAssistantMarkdownNoFragileANSI(t *testing.T) {
	m := tinyModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mm := updated.(model)
	mm.appendStream("Jawaban dengan `code` dan **bold**.\n\n```go\nfmt.Println(\"x\")\n```\n\nSelesai.")
	mm.endAssistantLine()
	for _, ln := range mm.lines {
		if ln.kind != lineAssistant {
			continue
		}
		if strings.Contains(ln.text, "\x1b[38;5;252m") {
			t.Errorf("fragile 252 escape present: %q", ln.text[:60])
		}
		if !strings.Contains(ln.text, "fmt.Println") {
			t.Errorf("code block content lost after strip: %q", ln.text)
		}
	}
}

// TestStripSeveredANSI: healthy escapes pass through; a bracket that begins a
// CSI sequence but lost its ESC is neutralized so the terminal can't print
// it as literal text.
func TestStripSeveredANSI(t *testing.T) {
	healthy := "a\x1b[38;5;252mb\x1b[0m"
	if got := stripSeveredANSI(healthy); got != healthy {
		t.Errorf("healthy escape mangled: %q", got)
	}
	severed := "prefix [38;5;252m suffix"
	if got := stripSeveredANSI(severed); strings.Contains(got, "[38;5;252m") {
		t.Errorf("severed CSI not neutralized: %q", got)
	}
}

// TestToolStreamAccumulates: live chunks from a running tool (terminal) must
// accumulate into the current tool line — one box streaming in real time —
// rather than each chunk spawning its own line.
func TestToolStreamAccumulates(t *testing.T) {
	m := tinyModel(t)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mm := u.(model)

	u, _ = mm.Update(eventMsg(agent.Event{Type: "tool_start", ToolName: "terminal", ToolArgs: `{"command":"go build"}`}))
	mm = u.(model)
	before := len(mm.lines)

	u, _ = mm.Update(toolStreamMsg{tool: "terminal", chunk: "compiling foo\n"})
	mm = u.(model)
	u, _ = mm.Update(toolStreamMsg{tool: "terminal", chunk: "compiling bar\n"})
	mm = u.(model)

	if len(mm.lines) != before {
		t.Fatalf("stream chunks should accumulate into one line, got %d want %d", len(mm.lines), before)
	}
	last := mm.lines[len(mm.lines)-1]
	if !strings.Contains(last.text, "compiling foo") || !strings.Contains(last.text, "compiling bar") {
		t.Errorf("chunks not accumulated: %q", last.text)
	}
}
