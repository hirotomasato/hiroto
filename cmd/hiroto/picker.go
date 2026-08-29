package main

// picker: reusable arrow-key list selection (model picker, session picker,
// skills picker). Works as an in-TUI overlay and as a standalone mini-program
// for CLI flags. Supports type-to-search filtering and an optional preview
// line for the highlighted item.

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
	title    string
	allItems []string // full list, unfiltered
	allVals  []string // picked values, parallel to allItems
	items    []string // filtered display lines
	values   []string // filtered values
	cursor   int
	filter   string
	onPick   func(m *model, v string)
	hover    func(v string) string // optional preview for the highlighted value
}

func newPicker(title string, items, values []string, onPick func(*model, string)) *pickerState {
	if values == nil {
		values = items
	}
	p := &pickerState{
		title:    title,
		allItems: append([]string(nil), items...),
		allVals:  append([]string(nil), values...),
		onPick:   onPick,
	}
	p.applyFilter()
	return p
}

// applyFilter rebuilds the visible items/values from the current filter.
func (p *pickerState) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(p.filter))
	p.items = nil
	p.values = nil
	for i, it := range p.allItems {
		if q == "" || strings.Contains(strings.ToLower(it), q) {
			p.items = append(p.items, it)
			p.values = append(p.values, p.allVals[i])
		}
	}
	if len(p.items) == 0 {
		p.cursor = -1
	} else if p.cursor < 0 || p.cursor >= len(p.items) {
		p.cursor = 0
	}
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
		if len(p.items) > 0 {
			p.cursor = 0
		}
	case "end", "G":
		if len(p.items) > 0 {
			p.cursor = len(p.items) - 1
		}
	case "backspace":
		if p.filter != "" {
			p.filter = p.filter[:len(p.filter)-1]
			p.applyFilter()
		}
	case "enter":
		if p.cursor >= 0 && p.cursor < len(p.values) {
			v := p.values[p.cursor]
			fn := p.onPick
			m.picker = nil
			if fn != nil {
				fn(m, v)
			}
			m.refresh()
		}
	case "esc", "q":
		m.picker = nil
		m.refresh()
	default:
		// type-to-search: append printable runes to the filter
		if rs := msg.Runes; len(rs) == 1 && rs[0] >= 0x20 && rs[0] != 0x7f {
			p.filter += string(rs[0])
			p.applyFilter()
		}
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
	var b strings.Builder
	title := p.title
	if p.filter != "" {
		title += "  (cari: " + p.filter + ")"
	}
	b.WriteString(pkTitle.Render(title) + "\n")

	if len(p.items) == 0 {
		b.WriteString(pkItem.Render("  (tidak ada hasil)") + "\n")
	} else {
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
		for i := start; i < end; i++ {
			marker, style := "  ", pkItem
			if i == p.cursor {
				marker, style = "❯ ", pkCur
			}
			b.WriteString(style.Render(marker+p.items[i]) + "\n")
		}
	}

	// preview of the highlighted item (e.g. skill description + path)
	if p.hover != nil && p.cursor >= 0 && p.cursor < len(p.values) {
		if d := p.hover(p.values[p.cursor]); d != "" {
			d = strings.TrimSpace(d)
			if len(d) > 240 {
				d = d[:240] + "…"
			}
			b.WriteString(pkItem.Render("  ─ "+strings.ReplaceAll(d, "\n", "\n    ")) + "\n")
		}
	}

	if len(p.items) > win {
		b.WriteString(pkItem.Render(fmt.Sprintf("  %d/%d", p.cursor+1, len(p.items))) + "\n")
	}
	b.WriteString(pkItem.Render("↑↓ pilih · ketik untuk cari · Enter ok · esc batal"))
	return pkBox.Render(strings.TrimRight(b.String(), "\n"))
}

// openSkillsPicker lists indexed skills; picking shows name + description.
// Hover shows the full description and SKILL.md path.
func (m *model) openSkillsPicker() {
	n := len(m.ag.Skills)
	if n == 0 {
		m.lines = append(m.lines, line{lineInfo, stMuted.Render("tidak ada skill terindex")})
		m.refresh()
		return
	}
	display := make([]string, n)
	values := make([]string, n)
	for i, s := range m.ag.Skills {
		values[i] = s.Name
		d := s.Description
		if d == "" {
			d = "(tanpa deskripsi)"
		}
		if len(d) > 80 {
			d = d[:80] + "…"
		}
		display[i] = fmt.Sprintf("%s  ·  %s", s.Name, d)
	}
	p := newPicker(fmt.Sprintf("pilih skill (%d)", n), display, values, func(mm *model, name string) {
		for _, s := range mm.ag.Skills {
			if s.Name == name {
				mm.lines = append(mm.lines, line{lineInfo, stMuted.Render("skill: " + s.Name + " — " + s.Description)})
				break
			}
		}
	})
	p.hover = func(name string) string {
		for _, s := range m.ag.Skills {
			if s.Name == name {
				d := s.Description
				if d == "" {
					d = "(tanpa deskripsi)"
				}
				return d + "\n" + s.Path
			}
		}
		return ""
	}
	m.picker = p
	m.refresh()
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
