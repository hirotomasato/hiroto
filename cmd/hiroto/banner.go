package main

// Banner & identity: big gradient HIROTO wordmark, HR chip, version, credits.

import (
	"fmt"
	"strings"

	"github.com/hirotomasato/hiroto/internal/config"

	"github.com/charmbracelet/lipgloss"
)

const version = "0.4.2"
const devCredit = "masantoid"

// stChip is the small "HR" logo chip used in the status bar and compact banner.
var stChip = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#161310")).
	Background(lipgloss.Color("#E8A33D")).
	Padding(0, 1)

// Tagline / rule — a step brighter than stMuted so the banner matches assets/hiroto.svg.
var (
	stTagline = lipgloss.NewStyle().Foreground(lipgloss.Color("#C4B49A"))
	stRule    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E8A33D"))
)

// ANSI-shadow glyphs (6 rows each, fixed width per letter).
var bannerGlyphs = map[rune][]string{
	'H': {"██╗  ██╗", "██║  ██║", "███████║", "██╔══██║", "██║  ██║", "╚═╝  ╚═╝"},
	'I': {" ██████╗", " ╚═════╝", "    ██║ ", "    ██║ ", "    ██║ ", "    ╚═╝ "},
	'R': {"██████╗ ", "██╔══██╗", "██████╔╝", "██╔══██╗", "██║  ██║", "╚═╝  ╚═╝"},
	'O': {" ██████╗ ", "██╔═══██╗", "██║   ██║", "██║   ██║", "╚██████╔╝", " ╚═════╝ "},
	'T': {"████████╗", "╚══██╔══╝", "   ██║   ", "   ██║   ", "   ██║   ", "   ╚═╝   "},
}

// bannerRows composes a word into 6 rows of block glyphs.
func bannerRows(word string) []string {
	var rows [][]string
	for _, r := range strings.ToUpper(word) {
		if g, ok := bannerGlyphs[r]; ok {
			rows = append(rows, g)
		}
	}
	var out []string
	for y := 0; y < 6; y++ {
		var b strings.Builder
		for x, g := range rows {
			b.WriteString(g[y])
			if x != len(rows)-1 {
				b.WriteString(" ")
			}
		}
		out = append(out, b.String())
	}
	return out
}

// gradient paints non-space runes with a horizontal gold→coral interpolation.
func gradient(lines []string) []string {
	from := [3]int{0xF2, 0xC1, 0x4E} // light gold
	to := [3]int{0xE0, 0x59, 0x2A}   // coral
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		rs := []rune(ln)
		var b strings.Builder
		for x, r := range rs {
			if r == ' ' {
				b.WriteRune(r)
				continue
			}
			t := 0.0
			if len(rs) > 1 {
				t = float64(x) / float64(len(rs)-1)
			}
			rgb := [3]int{}
			for c := 0; c < 3; c++ {
				rgb[c] = from[c] + int(float64(to[c]-from[c])*t)
			}
			hex := fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render(string(r)))
		}
		out = append(out, b.String())
	}
	return out
}

// bannerLines returns the full startup banner (big word + tagline + credits).
// Falls back to a compact one-liner on narrow terminals.
func bannerLines(width int, modelInfo string, skillCount int) []string {
	ruleW := width
	if ruleW > 56 {
		ruleW = 56
	}
	if ruleW < 24 {
		ruleW = 24
	}
	rule := stRule.Render(strings.Repeat("─", ruleW))

	var out []string
	out = append(out, rule) // top rule, mirrors the bottom one
	if width >= 60 {
		out = append(out, gradient(bannerRows("HIROTO"))...)
	} else {
		out = append(out, stChip.Render("HR")+" "+stBanner.Render("HIROTO"))
	}
	out = append(out,
		stTagline.Render("personal agent · go core · cyberteam"),
		rule,
		stMuted.Render("v"+version+" · "+devCredit+" · "+modelInfo+fmt.Sprintf(" · skills: %d", skillCount)),
		"",
	)
	return out
}

// setWindowTitle names the terminal tab "HR · hiroto" (OSC 9;22 with an
// OSC 2 fallback for terminals that don't support shell-integration marks).
func setWindowTitle() {
	fmt.Print("\x1b]9;22;HR · hiroto\x1b\\") // OSC 9;22 final
	fmt.Print("\x1b]2;HR · hiroto\x07")      // classic window title
}

// oneShotHeader prints a thin one-line identity header for `-q` runs.
func oneShotHeader(cfg *config.Config, skillCount int) {
	line := stChip.Render("HR") + stBanner.Render(" HIROTO") +
		stTagline.Render(" v"+version+" · "+devCredit+" · "+cfg.Model.Name+fmt.Sprintf(" · skills: %d", skillCount))
	fmt.Println(line)
	fmt.Println(stRule.Render(strings.Repeat("─", 56)))
}
