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
	pkTitle  = lipgloss.NewStyle().Bold(true).Foreground(cPrimary)
	pkBox    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBgFaint).Padding(0, 1)
	pkCur    = lipgloss.NewStyle().Bold(true).Foreground(cPrimary)
	pkItem   = lipgloss.NewStyle().Foreground(cMuted)
	pkHeader = lipgloss.NewStyle().Foreground(cPrimary).Faint(true)
)

// pickerState is the in-TUI overlay state.
type pickerState struct {
	title    string
	allItems []string // full list, unfiltered
	allVals  []string // picked values, parallel to allItems
	items    []string // filtered display lines
	values   []string // filtered values
	isHeader []bool   // parallel to items; true = group header (non-selectable)
	cursor   int
	filter   string
	onPick   func(m *model, v string)
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

// setHeaders marks certain indices in allItems as group headers (non-selectable).
// Must be called after newPicker and before the picker is displayed.
func (p *pickerState) setHeaders(headers []bool) {
	p.isHeader = headers
	// Re-apply filter to sync isHeader with items/values.
	p.applyFilter()
}

// applyFilter rebuilds the visible items/values from the current filter.
// Empty query shows everything; otherwise items are fuzzy-matched against the
// query and ranked so the best hits sort first. Group headers are always kept
// when their group has at least one visible child, but hidden when the group
// is empty after filtering.
func (p *pickerState) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(p.filter))
	p.items = nil
	p.values = nil
	// Build a new isHeader slice for the filtered view.
	var filtHeaders []bool

	type scored struct {
		i     int
		score int
	}
	var ranked []scored
	for i, it := range p.allItems {
		// Headers are always included in unfiltered view; they don't get scored.
		if len(p.isHeader) > i && p.isHeader[i] {
			if q == "" {
				p.items = append(p.items, it)
				p.values = append(p.values, p.allVals[i])
				filtHeaders = append(filtHeaders, true)
			}
			// When filtering, headers are added later if their group has matches.
			continue
		}
		if q == "" {
			ranked = append(ranked, scored{i, 0})
			continue
		}
		if s, ok := fuzzyScore(q, strings.ToLower(it)); ok {
			ranked = append(ranked, scored{i, s})
		}
	}
	// stable sort by score desc (ties keep original order)
	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0 && ranked[j].score > ranked[j-1].score; j-- {
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}
	// When filtering, re-insert headers before their group's items.
	if q != "" && len(p.isHeader) > 0 {
		lastHeader := -1
		for _, r := range ranked {
			// Find the header that precedes this item in the original order.
			hdr := p.findGroupHeader(r.i)
			if hdr != lastHeader {
				// Insert the header.
				if hdr >= 0 {
					p.items = append(p.items, p.allItems[hdr])
					p.values = append(p.values, p.allVals[hdr])
					filtHeaders = append(filtHeaders, true)
				}
				lastHeader = hdr
			}
			p.items = append(p.items, p.allItems[r.i])
			p.values = append(p.values, p.allVals[r.i])
			filtHeaders = append(filtHeaders, false)
		}
	} else {
		for _, r := range ranked {
			p.items = append(p.items, p.allItems[r.i])
			p.values = append(p.values, p.allVals[r.i])
			filtHeaders = append(filtHeaders, false)
		}
	}

	p.isHeader = filtHeaders

	if len(p.items) == 0 {
		p.cursor = -1
	} else if p.cursor < 0 || p.cursor >= len(p.items) {
		p.cursor = 0
	}
	// If cursor landed on a header, move to first non-header.
	if p.cursor >= 0 && p.cursor < len(p.isHeader) && p.isHeader[p.cursor] {
		p.cursor = p.nextNonHeader(p.cursor)
	}
}

// findGroupHeader returns the index in allItems of the group header that
// precedes the given item index, or -1 if none.
func (p *pickerState) findGroupHeader(itemIdx int) int {
	for i := itemIdx; i >= 0; i-- {
		if len(p.isHeader) > i && p.isHeader[i] {
			return i
		}
	}
	return -1
}

// nextNonHeader returns the next index that is not a header, or current if none.
func (p *pickerState) nextNonHeader(from int) int {
	for i := from; i < len(p.items); i++ {
		if !p.isHeader[i] {
			return i
		}
	}
	// Search backward.
	for i := from - 1; i >= 0; i-- {
		if !p.isHeader[i] {
			return i
		}
	}
	return from
}

// fuzzyScore reports whether q matches text as a fuzzy subsequence and, if so,
// a relevance score (higher = better). It rewards word-boundary hits, runs of
// consecutive matches, and matching at the very start — fzf-style.
func fuzzyScore(q, text string) (int, bool) {
	if q == "" {
		return 0, true
	}
	qrunes := []rune(q)
	trunes := []rune(text)
	qi := 0
	prev := -1 // rune index of the previous matched rune
	score := 0
	runLen := 0
	totalGap := 0

	for ti, r := range trunes {
		if qi >= len(qrunes) {
			break
		}
		if r != qrunes[qi] {
			continue
		}
		// word boundary: start, or preceded by a separator
		if ti == 0 || isSepRune(trunes[ti-1]) {
			score += 8
		}
		if prev >= 0 && ti == prev+1 {
			// consecutive run
			runLen++
			score += 4 + runLen
		} else {
			runLen = 1
			if prev >= 0 {
				totalGap += ti - prev - 1
			}
		}
		prev = ti
		qi++
	}
	if qi != len(qrunes) {
		return 0, false
	}
	// prefer earlier matches and fewer gaps; small penalty for late start
	score -= totalGap
	score -= prev / 3
	if score < 0 {
		score = 0
	}
	return score, true
}

func isSepRune(r rune) bool {
	switch r {
	case ' ', '-', '_', '.', '/', ':', '(', ')', '[', ']', ',', ';', '|':
		return true
	}
	return false
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
			// Skip headers backward.
			for p.cursor >= 0 && p.cursor < len(p.isHeader) && p.isHeader[p.cursor] {
				p.cursor--
			}
			if p.cursor < 0 {
				p.cursor = 0
			}
		}
	case "down", "j":
		if p.cursor < len(p.items)-1 {
			p.cursor++
			// Skip headers forward.
			for p.cursor < len(p.items) && p.cursor < len(p.isHeader) && p.isHeader[p.cursor] {
				p.cursor++
			}
			if p.cursor >= len(p.items) {
				p.cursor = len(p.items) - 1
			}
		}
	case "home", "g":
		if len(p.items) > 0 {
			p.cursor = p.nextNonHeader(0)
		}
	case "end", "G":
		if len(p.items) > 0 {
			p.cursor = len(p.items) - 1
			for p.cursor >= 0 && p.cursor < len(p.isHeader) && p.isHeader[p.cursor] {
				p.cursor--
			}
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
	b.WriteString(pkTitle.Render(p.title) + "\n")

	// search input line (always visible, shows query + caret)
	search := p.filter
	if search == "" {
		search = "ketik untuk cari…"
	}
	b.WriteString("  " + pkItem.Render(search) + "▌\n")

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
			isHdr := i < len(p.isHeader) && p.isHeader[i]
			if isHdr {
				marker, style = "  ", pkHeader
			}
			if i == p.cursor {
				marker, style = "❯ ", pkCur
			}
			line := marker + p.items[i]
			if isHdr {
				line = pkHeader.Render(line)
			} else {
				line = style.Render(line)
			}
			b.WriteString(line + "\n")
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
		display[i] = s.Name
	}
	p := newPicker(fmt.Sprintf("pilih skill (%d)", n), display, values, func(mm *model, name string) {
		for _, s := range mm.ag.Skills {
			if s.Name == name {
				mm.lines = append(mm.lines, line{lineInfo, stMuted.Render("skill: " + s.Name + " — " + s.Description)})
				break
			}
		}
	})
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
