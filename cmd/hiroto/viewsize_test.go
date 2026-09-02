package main

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hirotomasato/hiroto/internal/agent"
	"github.com/hirotomasato/hiroto/internal/config"
	"github.com/hirotomasato/hiroto/internal/memory"
	"github.com/hirotomasato/hiroto/internal/session"
)

func tTempDir() string {
	d, _ := os.MkdirTemp("", "vs")
	return d
}

// tinyModel returns a model for viewport/sizing tests without a config file.
// HIROTO_HOME is redirected to a temp dir so the global todo/memory/session
// state on the developer's machine can't leak into the sizing math (a stale
// ~/.hiroto/todo.json would render the todo panel and shrink the viewport).
func tinyModel(t *testing.T) model {
	t.Setenv("HIROTO_HOME", t.TempDir())
	cfg := &config.Config{}
	cfg.Model.Name = "test-model"
	m := initialModel(cfg, &agent.Agent{}, memory.New(), session.NewAt(tTempDir()))
	return m
}

// A tiny terminal (height < 8) must not produce a negative viewport height,
// which previously panicked in viewport.GotoBottom (slice bounds out of range).
func TestWindowSizeTinyHeightNoPanic(t *testing.T) {
	m := tinyModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 3})
	mm := updated.(model)
	if mm.vp.Height < 1 {
		t.Fatalf("vp.Height = %d, want >= 1", mm.vp.Height)
	}
	// zero-size (e.g. size reported before PTY init) must also be safe
	updated, _ = mm.Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	mm = updated.(model)
	if mm.vp.Height < 1 {
		t.Fatalf("zero-size: vp.Height = %d, want >= 1", mm.vp.Height)
	}
}

// A normal terminal keeps the expected height: chrome is MEASURED from
// layoutParts(false) (input box 4 + status 1 + help 1 = 6), so the viewport
// gets 30-6 = 24 rows. The measurement and the render share one source of
// truth, so the frame always fits the terminal exactly and the banner's top
// rule can't scroll away.
func TestWindowSizeNormalHeight(t *testing.T) {
	m := tinyModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mm := updated.(model)
	if mm.vp.Height != 24 {
		t.Fatalf("vp.Height = %d, want 24", mm.vp.Height)
	}
	if mm.width != 100 || mm.height != 30 {
		t.Fatalf("model dims = %dx%d, want 100x30", mm.width, mm.height)
	}
	// The rendered frame must fit the terminal EXACTLY — any overflow scrolls
	// the top (banner rule) off-screen. This is the regression test for the
	// "missing top rule above HIROTO" bug.
	frame := lipgloss.Height(mm.View())
	if frame != 30 {
		t.Fatalf("frame height = %d, want 30 (terminal height)", frame)
	}
}
