package main

// Exit summary (Hermes-style): after the TUI quits, print a short recap so
// the session can be resumed from the shell with a single command.

import (
	"fmt"
	"strings"
	"time"

	"github.com/hirotomasato/hiroto/internal/llm"
)

type exitInfo struct {
	set       bool
	id        string
	title     string
	dur       time.Duration
	total     int
	users     int
	toolCalls int
}

// exitSummary is filled by the TUI right before tea.Quit and printed by
// printExitSummary once the altscreen has closed.
var exitSummary exitInfo

// collectExitSummary captures what the quit screen needs from the model.
// Skips when nothing was asked (empty session) — no summary for a bare open/quit.
func collectExitSummary(m *model) {
	users, toolCalls := countStats(m.ag.Messages)
	if users == 0 {
		return
	}
	exitSummary = exitInfo{
		set:       true,
		id:        m.sessID,
		title:     firstUserText(m.ag.Messages, 60),
		dur:       time.Since(m.startedAt),
		total:     len(m.ag.Messages),
		users:     users,
		toolCalls: toolCalls,
	}
}

// countStats counts user messages and tool calls in a conversation.
func countStats(msgs []llm.Message) (users, toolCalls int) {
	for _, msg := range msgs {
		if msg.Role == llm.RoleUser {
			users++
		}
		toolCalls += len(msg.ToolCalls)
	}
	return
}

// formatDuration renders 18m 24s style ("1h 5m 3s" above an hour).
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	min := int(d.Minutes()) % 60
	sec := int(d.Seconds()) % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm %ds", h, min, sec)
	case min > 0:
		return fmt.Sprintf("%dm %ds", min, sec)
	default:
		return fmt.Sprintf("%ds", sec)
	}
}

// printExitSummary draws the quit recap (called after the altscreen closes,
// so colors render on the normal terminal).
func printExitSummary() {
	s := exitSummary
	if !s.set {
		return
	}
	fmt.Println(stBanner.Render("Resume this session with:"))
	fmt.Println("  " + stToolTag.Render("hiroto --resume "+s.id))
	fmt.Println("  " + stToolTag.Render("hiroto -c \""+strings.ReplaceAll(s.title, `"`, "'")+"\""))
	fmt.Println()
	label := func(name string) string { return stMuted.Render(fmt.Sprintf("%-16s", name)) }
	fmt.Println(label("Session:") + s.id)
	fmt.Println(label("Title:") + s.title)
	fmt.Println(label("Duration:") + formatDuration(s.dur))
	fmt.Printf("%s%d", label("Messages:"), s.total)
	fmt.Println(stMuted.Render(fmt.Sprintf(" (%d user, %d tool calls)", s.users, s.toolCalls)))
}
