package main

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
func tinyModel() model {
	cfg := &config.Config{}
	cfg.Model.Name = "test-model"
	m := initialModel(cfg, &agent.Agent{}, memory.New(), session.NewAt(tTempDir()))
	return m
}

// A tiny terminal (height < 8) must not produce a negative viewport height,
// which previously panicked in viewport.GotoBottom (slice bounds out of range).
func TestWindowSizeTinyHeightNoPanic(t *testing.T) {
	m := tinyModel()
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

// A normal terminal keeps the expected height (height - 7 for layout chrome).
func TestWindowSizeNormalHeight(t *testing.T) {
	m := tinyModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mm := updated.(model)
	if mm.vp.Height != 23 {
		t.Fatalf("vp.Height = %d, want 23", mm.vp.Height)
	}
	if mm.width != 100 || mm.height != 30 {
		t.Fatalf("model dims = %dx%d, want 100x30", mm.width, mm.height)
	}
}
