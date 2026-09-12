package main

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	mm.lines = append(mm.lines, line{lineAssistant, strings.Repeat("| kolom | sangat panjang sekali | nilai |", 10)})
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
