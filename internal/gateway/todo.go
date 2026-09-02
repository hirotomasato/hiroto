package gateway

// /todo for the Telegram gateway: same checklist the TUI panel shows, same
// per-session file. Without this the command was a stub that told the user to
// "just type the task", so a plan left in_progress by a timed-out turn had no
// way to be inspected or cleared from chat.

import (
	"fmt"
	"strings"

	"github.com/hirotomasato/hiroto/internal/tools"
)

const tgTodoUsage = "/todo · /todo done <id> · /todo unstick · /todo clear"

// handleTodo renders or edits the checklist for this chat's session.
func (g *gw) handleTodo(chatID int64, arg string, replyTo int) {
	if g.todos == nil {
		send(g.bot, chatID, "todo store tidak aktif", replyTo)
		return
	}
	// Each chat owns a session id, so point the store at this chat first.
	g.bindTodos(chatID)

	fields := strings.Fields(arg)
	sub := ""
	if len(fields) > 0 {
		sub = strings.ToLower(fields[0])
	}
	// Guard the slice: a bare "/todo" leaves no element to slice from.
	rest := ""
	if len(fields) > 1 {
		rest = strings.TrimSpace(strings.Join(fields[1:], " "))
	}

	switch sub {
	case "":
		send(g.bot, chatID, renderTodoList(g.todos), replyTo)

	case "done", "complete":
		if rest == "" {
			send(g.bot, chatID, "pakai: /todo done <id>", replyTo)
			return
		}
		if !g.todos.Complete(rest) {
			send(g.bot, chatID, "✗ id tidak ada: "+rest+"\n\n"+renderTodoList(g.todos), replyTo)
			return
		}
		send(g.bot, chatID, renderTodoList(g.todos), replyTo)

	case "unstick", "reset":
		n := g.todos.Demote()
		if n == 0 {
			send(g.bot, chatID, "tidak ada task yang nyangkut", replyTo)
			return
		}
		send(g.bot, chatID, fmt.Sprintf("✓ %d task in_progress → pending\n\n%s", n, renderTodoList(g.todos)), replyTo)

	case "clear":
		done, total := g.todos.Counts()
		g.todos.Clear()
		send(g.bot, chatID, fmt.Sprintf("✓ task list dihapus (%d/%d selesai)", done, total), replyTo)

	default:
		send(g.bot, chatID, "❓ "+tgTodoUsage, replyTo)
	}
}

// bindTodos points the shared store at this chat's session checklist. The
// gateway serves many chats from one process, so the binding must be refreshed
// on every turn — otherwise chat B would advance chat A's plan. A chat that
// resumed another session keeps that session's id, so prefer it over the
// chat-derived default.
func (g *gw) bindTodos(chatID int64) {
	if g.todos == nil {
		return
	}
	id := sessionIDFor(chatID)
	if st, ok := g.chats[chatID]; ok && st.id != "" {
		id = st.id
	}
	g.todos.Retarget(id)
}

// renderTodoList formats the checklist for a Telegram message.
func renderTodoList(ts *tools.TodoStore) string {
	items := ts.Snapshot()
	if len(items) == 0 {
		return "belum ada task — " + tgTodoUsage
	}
	done, total := ts.Counts()
	var b strings.Builder
	fmt.Fprintf(&b, "◆ Tasks %d/%d\n\n", done, total)
	for _, it := range items {
		mark := "○"
		switch it.Status {
		case tools.StatusCompleted:
			mark = "✔"
		case tools.StatusInProgress:
			mark = "▶"
		case tools.StatusCancelled:
			mark = "✗"
		}
		fmt.Fprintf(&b, "%s %s · %s\n", mark, it.ID, it.Content)
	}
	b.WriteString("\n" + tgTodoUsage)
	return b.String()
}
