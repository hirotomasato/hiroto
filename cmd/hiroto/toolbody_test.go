package main

import (
	"strings"
	"testing"
)

// renderToolBody truncates by line and char budget and appends a hidden-count
// footer at the default compact verbose level.
func TestRenderToolBodyCompact(t *testing.T) {
	m := &model{verbose: 0}
	// 20 short lines → capped at 6, footer mentions hidden lines.
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, "line")
	}
	out := m.renderToolBody(strings.Join(lines, "\n"))
	if !strings.Contains(out, "hidden") {
		t.Errorf("expected hidden-count footer, got:\n%s", out)
	}
	if !strings.Contains(out, "+14 lines") {
		t.Errorf("expected '+14 lines', got:\n%s", out)
	}
}

// Short output has no footer.
func TestRenderToolBodyShort(t *testing.T) {
	m := &model{verbose: 0}
	out := m.renderToolBody("one\ntwo")
	if strings.Contains(out, "hidden") {
		t.Errorf("short output should not have footer, got:\n%s", out)
	}
}

// Log level (verbose=2) shows everything, no footer.
func TestRenderToolBodyLogLevel(t *testing.T) {
	m := &model{verbose: 2}
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "x")
	}
	out := m.renderToolBody(strings.Join(lines, "\n"))
	if strings.Contains(out, "hidden") {
		t.Errorf("log level should show all, got footer:\n%s", out)
	}
}

// hasDiff detects unified-diff hunks; colorizeDiff must not lose content.
func TestHasDiffAndColorize(t *testing.T) {
	diff := "patched a.go (2 lines, 1 replacement)\n@@ -1,2 +1,2 @@\n foo\n-bar\n+baz"
	if !hasDiff(diff) {
		t.Fatal("hasDiff should detect the @@ hunk")
	}
	col := colorizeDiff(diff)
	for _, want := range []string{"foo", "bar", "baz", "@@"} {
		if !strings.Contains(col, want) {
			t.Errorf("colorizeDiff dropped %q:\n%s", want, col)
		}
	}
	if hasDiff("wrote foo.go (12 bytes)") {
		t.Error("plain output must not be treated as a diff")
	}
}
