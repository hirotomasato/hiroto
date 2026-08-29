package main

// picker: reusable arrow-key list selection (model picker, session picker).
// Works as an in-TUI overlay and as a standalone mini-program for CLI flags.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	pkTitle = lipgloss.NewStyle().Bold(true).Foreground(cPrimary)
	pkBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBgFaint).Padding(0, 1)
	pkCur   = lipgloss.NewStyle().Bold(true).Foreground(cPrimary)
	pkItem  = lipgloss.NewStyle().Foreground(cMuted)
)

// pickerState is the in-TUI overlay state.
type pickerState struct {
	title  string
	items  []string // display lines
	values []string // picked values (defaults to items)
	cursor int
	onPick func(m *model, v string)
}

func newPicker(title string, items, values []string, onPick func(*model, string)) *pickerState {
	if values == nil {
		values = items
	}
	return &pickerState{title: title, items: items, values: values, onPick: onPick}
}

// updatePicker handles keys while the overlay is open. Returns true if consumed.
func (m *model) updatePicker(msg tea.KeyMsg) bool {
	p := m.picker
	if p == nil {
		return false
	}
	switch msg.String() {
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "j":
		if p.cursor < len(p.items)-1 {
			p.cursor++
		}
	case "home", "g":
		p.cursor = 0
	case "end", "G":
		p.cursor = len(p.items) - 1
	case "enter":
		v := p.values[p.cursor]
		fn := p.onPick
		m.picker = nil
		if fn != nil {
			fn(m, v)
		}
		m.refresh()
	case "esc", "q":
		m.picker = nil
		m.refresh()
	default:
		return true // swallow other keys while picking
	}
	return true
}

// renderPicker draws the overlay box (windowed to 12 visible rows).
func (m *model) renderPicker() string {
	p := m.picker
	if p == nil {
		return ""
	}
	const win = 12
	start, end := 0, len(p.items)
	if end > win {
		start = p.cursor - win/2
		if start < 0 {
			start = 0
		}
		if start+win > end {
			start = end - win
		}
		end = start + win
	}
	var b strings.Builder
	b.WriteString(pkTitle.Render(p.title) + "\n")
	for i := start; i < end; i++ {
		marker, style := "  ", pkItem
		if i == p.cursor {
			marker, style = "❯ ", pkCur
		}
		b.WriteString(style.Render(marker+p.items[i]) + "\n")
	}
	if len(p.items) > 12 {
		b.WriteString(pkItem.Render(fmt.Sprintf("  %d/%d", p.cursor+1, len(p.items))) + "\n")
	}
	b.WriteString(pkItem.Render("↑↓ pilih · Enter ok · esc batal"))
	return pkBox.Render(strings.TrimRight(b.String(), "\n"))
}

// ---------- standalone picker (CLI flags) ----------

type cliPickModel struct {
	title  string
	items  []string
	cursor int
	chosen int // -1 = cancelled
	done   bool
}

func (c cliPickModel) Init() tea.Cmd { return nil }

func (c cliPickModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			c.done, c.chosen = true, -1
		case "up", "k":
			if c.cursor > 0 {
				c.cursor--
			}
		case "down", "j":
			if c.cursor < len(c.items)-1 {
				c.cursor++
			}
		case "enter":
			c.done, c.chosen = true, c.cursor
		}
	}
	return c, nil
}

func (c cliPickModel) View() string {
	var b strings.Builder
	b.WriteString(pkTitle.Render(c.title) + "\n")
	for i, it := range c.items {
		if i == c.cursor {
			b.WriteString(pkCur.Render("❯ "+it) + "\n")
		} else {
			b.WriteString(pkItem.Render("  "+it) + "\n")
		}
	}
	b.WriteString(pkItem.Render("↑↓ pilih · Enter ok · esc batal"))
	return b.String()
}

// runCLIPicker shows a standalone selection list; returns index or -1.
func runCLIPicker(title string, items []string) int {
	pm := cliPickModel{title: title, items: items, chosen: -1}
	out, err := tea.NewProgram(pm).Run()
	if err != nil {
		return -1
	}
	if res, ok := out.(cliPickModel); ok {
		return res.chosen
	}
	return -1
}
