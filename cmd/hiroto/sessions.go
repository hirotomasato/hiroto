package main

// Session glue: ids, persistence, session picker, resume loading, model picker.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hirotomasato/hiroto/internal/config"
	"github.com/hirotomasato/hiroto/internal/llm"
	"github.com/hirotomasato/hiroto/internal/memory"
	"github.com/hirotomasato/hiroto/internal/session"
	"github.com/hirotomasato/hiroto/internal/tools"

	tea "github.com/charmbracelet/bubbletea"
)

func newSessionID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return time.Now().Format("20060102_150405") + "_" + hex.EncodeToString(b)
}

// toStored converts agent messages for persistence.
func toStored(msgs []llm.Message) []session.StoredMessage {
	out := make([]session.StoredMessage, 0, len(msgs))
	for _, m := range msgs {
		sm := session.StoredMessage{Role: string(m.Role), ToolCallID: m.ToolCallID, ToolName: m.Name}
		if s, ok := m.Content.(string); ok {
			sm.Content = s
		}
		for _, tc := range m.ToolCalls {
			sm.ToolCalls = append(sm.ToolCalls, session.StoredToolCall{ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments})
		}
		out = append(out, sm)
	}
	return out
}

// fromStored rebuilds agent messages from a saved session.
func fromStored(st []session.StoredMessage) []llm.Message {
	out := make([]llm.Message, 0, len(st))
	for _, sm := range st {
		m := llm.Message{Role: llm.Role(sm.Role), Content: sm.Content, ToolCallID: sm.ToolCallID, Name: sm.ToolName}
		for _, tc := range sm.ToolCalls {
			var call llm.ToolCall
			call.ID = tc.ID
			call.Type = "function"
			call.Function.Name = tc.Name
			call.Function.Arguments = tc.Args
			m.ToolCalls = append(m.ToolCalls, call)
		}
		out = append(out, m)
	}
	return out
}

// saveSession persists the current conversation (skips empty ones).
func (m *model) saveSession() {
	userCount := 0
	for _, msg := range m.ag.Messages {
		if msg.Role == llm.RoleUser {
			userCount++
		}
	}
	if userCount == 0 {
		return
	}
	sess := &session.Session{
		ID:       m.sessID,
		Title:    firstUserText(m.ag.Messages, 60),
		Model:    m.ag.Client.Model,
		Created:  time.Now(),
		Updated:  time.Now(),
		Messages: toStored(m.ag.Messages),
		Todos:    todosToStored(m.todos),
	}
	_ = m.sessStore.Save(sess)
}

// todosToStored snapshots the checklist for the session file, so resuming a
// session brings back its plan (the panel is otherwise empty after a restart).
func todosToStored(ts *tools.TodoStore) []session.StoredTodo {
	if ts == nil {
		return nil
	}
	items := ts.Snapshot()
	if len(items) == 0 {
		return nil
	}
	out := make([]session.StoredTodo, 0, len(items))
	for _, it := range items {
		out = append(out, session.StoredTodo{ID: it.ID, Content: it.Content, Status: it.Status})
	}
	return out
}

// todosFromStored rebuilds checklist items from a saved session.
func todosFromStored(st []session.StoredTodo) []tools.TodoItem {
	out := make([]tools.TodoItem, 0, len(st))
	for _, t := range st {
		out = append(out, tools.TodoItem{ID: t.ID, Content: t.Content, Status: t.Status})
	}
	return out
}

// restoreTodos rebinds the store to sess and loads its plan. Session-file items
// win when present; otherwise whatever the per-session todo file holds stays.
func restoreTodos(ts *tools.TodoStore, sess *session.Session) {
	if ts == nil || sess == nil {
		return
	}
	ts.Retarget(sess.ID)
	if len(sess.Todos) > 0 {
		ts.Save(todosFromStored(sess.Todos))
	}
	// A resumed session isn't running anything yet: never restore a task as
	// in_progress or the panel lies about work in flight.
	ts.Demote()
}

func firstUserText(msgs []llm.Message, max int) string {
	for _, msg := range msgs {
		if msg.Role == llm.RoleUser {
			if s, ok := msg.Content.(string); ok {
				s = strings.TrimSpace(s)
				if len(s) > max {
					s = s[:max] + "…"
				}
				return s
			}
		}
	}
	return "(tanpa judul)"
}

// sessionDisplay renders "id · time · title" picker lines.
func sessionDisplay(list []session.Session) (display, ids []string) {
	for _, s := range list {
		display = append(display, fmt.Sprintf("%s · %s · %s", s.ID, s.Updated.Format("02/01 15:04"), s.Title))
		ids = append(ids, s.ID)
	}
	return display, ids
}

// openResumePicker shows saved sessions in the in-TUI overlay.
func (m *model) openResumePicker() {
	list := m.sessStore.List()
	if len(list) == 0 {
		m.lines = append(m.lines, line{lineInfo, stMuted.Render("belum ada sesi tersimpan")})
		return
	}
	display, ids := sessionDisplay(list)
	m.picker = newPicker("lanjutkan sesi", display, ids, func(mm *model, id string) {
		mm.loadSessionByID(id)
	})
}

// loadSessionByID swaps the agent's conversation with a saved one.
func (m *model) loadSessionByID(id string) bool {
	sess, err := m.sessStore.Load(id)
	if err != nil {
		m.lines = append(m.lines, line{lineError, stErr.Render("gagal memuat sesi: " + err.Error())})
		m.refresh()
		return false
	}
	m.ag.Messages = fromStored(sess.Messages)
	m.sessID = sess.ID
	if sess.Model != "" {
		m.ag.Client.Model = sess.Model
	}
	restoreTodos(m.todos, sess)
	m.lines = append(m.lines, line{lineInfo, stMuted.Render("sesi dilanjutkan: " + sess.ID + " · " + sess.Title)})
	if done, total := m.todos.Counts(); total > 0 {
		m.lines = append(m.lines, line{lineInfo, stMuted.Render(fmt.Sprintf("task list dipulihkan: %d/%d selesai", done, total))})
	}
	m.refresh()
	return true
}

// openModelPicker lists live models from the endpoint; picking persists to config.yaml.
// Models are auto-grouped by provider prefix (the part before the first '/').
func (m *model) openModelPicker() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := m.ag.Client.ListModels(ctx)
	if err != nil {
		m.lines = append(m.lines, line{lineError, stErr.Render("gagal ambil daftar model: " + err.Error())})
		m.refresh()
		return
	}
	if len(models) == 0 {
		m.lines = append(m.lines, line{lineInfo, stMuted.Render("endpoint tidak mengembalikan model")})
		m.refresh()
		return
	}
	display, values, headers := groupModelsByProvider(models, m.ag.Client.Model)
	m.picker = newPicker("pilih model", display, values, func(mm *model, name string) {
		mm.ag.Client.Model = name
		mm.cfg.Model.Name = name
		config.SaveModel(name)
		mm.lines = append(mm.lines, line{lineInfo, stMuted.Render("model: " + name + " (tersimpan)")})
	})
	m.picker.setHeaders(headers)
}

// groupModelsByProvider partitions models by their provider prefix (the part
// before the first '/'). Models without a prefix go into a "default" group.
// Returns display lines, corresponding values, and a header mask.
func groupModelsByProvider(models []string, active string) (display, values []string, headers []bool) {
	// Group by provider prefix.
	groups := make(map[string][]string)
	var groupOrder []string
	for _, name := range models {
		prefix := "default"
		if idx := strings.IndexByte(name, '/'); idx >= 0 {
			prefix = name[:idx]
		}
		if _, ok := groups[prefix]; !ok {
			groupOrder = append(groupOrder, prefix)
		}
		groups[prefix] = append(groups[prefix], name)
	}
	// Sort groups alphabetically, but keep "default" at the top.
	sortGroupOrder(groupOrder)
	// Build display with headers.
	for _, grp := range groupOrder {
		mdls := groups[grp]
		// Sort models within the group.
		sortModels(mdls)
		// Header line.
		display = append(display, "── "+grp+" ──")
		values = append(values, "")
		headers = append(headers, true)
		// Model lines.
		for _, name := range mdls {
			label := name
			if name == active {
				label = name + "  ● aktif"
			}
			display = append(display, "  "+label)
			values = append(values, name)
			headers = append(headers, false)
		}
	}
	return display, values, headers
}

// sortGroupOrder sorts providers alphabetically, but puts "default" first.
func sortGroupOrder(groups []string) {
	// Bubble sort — small N, readable.
	for i := 0; i < len(groups); i++ {
		for j := i + 1; j < len(groups); j++ {
			ai, aj := groups[i], groups[j]
			// "default" always first.
			if ai == "default" {
				continue
			}
			if aj == "default" || aj < ai {
				groups[i], groups[j] = groups[j], groups[i]
			}
		}
	}
}

// sortModels sorts model names alphabetically in-place.
func sortModels(names []string) {
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
}

// runOneShotContinue runs a one-shot query preloaded with the last saved
// session (hiroto -c "..."), then saves the extended conversation back.
func runOneShotContinue(cfg *config.Config, mem *memory.Store, query string) {
	ss := session.New()
	list := ss.List()
	if len(list) == 0 {
		fmt.Fprintln(os.Stderr, "hiroto: belum ada sesi tersimpan — pakai -q")
		os.Exit(1)
	}
	sess, err := ss.Load(list[0].ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hiroto: gagal muat sesi:", err)
		os.Exit(1)
	}
	ag := buildAgent(cfg, mem)
	ag.Messages = fromStored(sess.Messages)
	// -c continues a saved session, so it owns that session's checklist too.
	restoreTodos(tools.SharedTodo(), sess)
	oneShotHeader(cfg, len(ag.Skills))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	answer, err := ag.Run(ctx, query, func(chunk string) {
		fmt.Print(chunk)
	})
	fmt.Println()
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nhiroto: error:", err)
		os.Exit(1)
	}
	_ = answer
	sess.Messages = toStored(ag.Messages)
	sess.Todos = todosToStored(tools.SharedTodo())
	sess.Updated = time.Now()
	sess.Model = ag.Client.Model
	_ = ss.Save(sess)
}

// runResumeTUI opens the TUI with a saved session preloaded (hiroto --resume <id>).
func runResumeTUI(cfg *config.Config, mem *memory.Store, id string) error {
	ss := session.New()
	sess, err := ss.Load(id)
	if err != nil {
		return fmt.Errorf("sesi %q tidak ditemukan: %w", id, err)
	}
	ag := buildAgent(cfg, mem)
	ag.Messages = fromStored(sess.Messages)
	if sess.Model != "" {
		ag.Client.Model = sess.Model
	}

	// Load the transcript into the scrollback so the chat history is visible.
	m := initialModel(cfg, ag, mem, ss)
	m.sessID = sess.ID
	restoreTodos(m.todos, sess)
	if sess.Title != "" {
		m.lines = append(m.lines, line{lineInfo, stMuted.Render("sesi dilanjutkan: " + sess.ID + " · " + sess.Title)})
	}
	for _, msg := range ag.Messages {
		switch {
		case msg.Role == llm.RoleUser:
			if s, ok := msg.Content.(string); ok {
				m.lines = append(m.lines, line{lineUser, s})
			}
		case msg.Role == llm.RoleAssistant:
			if s, ok := msg.Content.(string); ok && strings.TrimSpace(s) != "" {
				m.lines = append(m.lines, line{lineAssistant, s})
			}
		}
	}
	m.refresh()

	setWindowTitle()
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	if err != nil {
		return err
	}
	printExitSummary()
	return nil
}

// runModelsCmd is the standalone `hiroto --models` picker.
func runModelsCmd(cfg *config.Config) {
	client := llm.New(cfg.Model.BaseURL, cfg.APIKey(), cfg.Model.Name)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := client.ListModels(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hiroto:", err)
		os.Exit(1)
	}
	if len(models) == 0 {
		fmt.Fprintln(os.Stderr, "hiroto: endpoint tidak mengembalikan model")
		os.Exit(1)
	}
	display, values, _ := groupModelsByProvider(models, cfg.Model.Name)
	idx := runCLIPicker("pilih model (tersimpan ke config.yaml)", display)
	if idx < 0 || idx >= len(values) {
		return
	}
	name := values[idx]
	if name == "" {
		return // header line
	}
	config.SaveModel(name)
	fmt.Println("model tersimpan:", name)
}
