package main

// /todo command: inspect and unstick the live checklist without going through
// the agent. The panel is fed by ~/.hiroto/todos/<session>.json, so a plan left
// behind by a crashed or cancelled turn used to sit there with no way out —
// these subcommands are that way out.

import (
	"fmt"
	"strings"

	"github.com/hirotomasato/hiroto/internal/tools"
)

const todoUsage = "pakai: /todo · /todo add <teks> · /todo done <id> · /todo undo <id> · /todo unstick · /todo clear"

// handleTodoCmd runs a /todo subcommand. fields[0] is "/todo".
func (m *model) handleTodoCmd(fields []string) {
	if m.todos == nil {
		m.flashMsg("todo store tidak aktif", "error")
		return
	}
	sub := ""
	if len(fields) > 1 {
		sub = strings.ToLower(fields[1])
	}
	// Guard the slice: a bare "/todo" has no index 2 to slice from.
	arg := ""
	if len(fields) > 2 {
		arg = strings.TrimSpace(strings.Join(fields[2:], " "))
	}

	switch sub {
	case "":
		m.todos.Reload()
		m.showTodos()

	case "add":
		if arg == "" {
			m.flashMsg("pakai: /todo add <teks>", "error")
			return
		}
		m.todos.Update([]tools.TodoItem{{ID: nextTodoID(m.todos.Snapshot()), Content: arg, Status: tools.StatusPending}})
		m.showTodos()

	case "done", "complete":
		if arg == "" {
			m.flashMsg("pakai: /todo done <id>", "error")
			return
		}
		if !m.todos.Complete(arg) {
			m.flashMsg("id tidak ada: "+arg, "error")
			return
		}
		m.showTodos()

	case "undo", "reopen":
		if arg == "" {
			m.flashMsg("pakai: /todo undo <id>", "error")
			return
		}
		found := false
		for _, it := range m.todos.Snapshot() {
			if it.ID == arg {
				found = true
				break
			}
		}
		if !found {
			m.flashMsg("id tidak ada: "+arg, "error")
			return
		}
		m.todos.Update([]tools.TodoItem{{ID: arg, Status: tools.StatusPending}})
		m.showTodos()

	case "unstick", "reset":
		n := m.todos.Demote()
		if n == 0 {
			m.lines = append(m.lines, line{lineInfo, stMuted.Render("tidak ada task yang nyangkut")})
			return
		}
		m.lines = append(m.lines, line{lineInfo, stMuted.Render(fmt.Sprintf("%d task in_progress → pending", n))})
		m.showTodos()

	case "clear":
		done, total := m.todos.Counts()
		m.todos.Clear()
		m.lines = append(m.lines, line{lineInfo, stMuted.Render(fmt.Sprintf("task list dihapus (%d/%d selesai)", done, total))})

	default:
		m.flashMsg(todoUsage, "error")
	}
}

// showTodos prints the current list into the transcript (the panel above the
// input only shows a window of it).
func (m *model) showTodos() {
	items := m.todos.Snapshot()
	if len(items) == 0 {
		m.lines = append(m.lines, line{lineInfo, stMuted.Render("belum ada task — " + todoUsage)})
		return
	}
	var b strings.Builder
	done, total := m.todos.Counts()
	b.WriteString(stDiffHunk.Render(fmt.Sprintf("Tasks %d/%d", done, total)))
	for _, it := range items {
		var mark string
		switch it.Status {
		case tools.StatusCompleted:
			mark = stDiffAdd.Render("✔")
		case tools.StatusInProgress:
			mark = stBanner.Render("▶")
		case tools.StatusCancelled:
			mark = stErr.Render("✗")
		default:
			mark = stMuted.Render("○")
		}
		b.WriteString("\n  " + mark + " " + stHelp.Render(it.ID) + " " + it.Content)
	}
	m.lines = append(m.lines, line{lineInfo, b.String()})
}

// nextTodoID returns the smallest unused positive integer id as a string, so
// manually added tasks keep the "1,2,3" shape the agent writes.
func nextTodoID(items []tools.TodoItem) string {
	used := make(map[string]bool, len(items))
	for _, it := range items {
		used[it.ID] = true
	}
	for i := 1; ; i++ {
		id := fmt.Sprintf("%d", i)
		if !used[id] {
			return id
		}
	}
}

// stuckTodos lists ids still marked in_progress. After a turn ends nothing is
// running, so a non-empty result means the agent left its plan half-updated.
func (m model) stuckTodos() string {
	if m.todos == nil {
		return ""
	}
	var ids []string
	for _, it := range m.todos.Snapshot() {
		if it.Status == tools.StatusInProgress {
			id := it.ID
			if id == "" {
				id = truncateStr(it.Content, 24)
			}
			ids = append(ids, id)
		}
	}
	return strings.Join(ids, ", ")
}
