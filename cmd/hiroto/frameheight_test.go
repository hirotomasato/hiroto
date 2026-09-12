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
