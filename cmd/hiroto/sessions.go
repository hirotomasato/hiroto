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
	}
	_ = m.sessStore.Save(sess)
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
	m.lines = append(m.lines, line{lineInfo, stMuted.Render("sesi dilanjutkan: " + sess.ID + " · " + sess.Title)})
	m.refresh()
	return true
}

// openModelPicker lists live models from the endpoint; picking persists to config.yaml.
func (m *model) openModelPicker() {
	models, err := m.ag.Client.ListModels(context.Background())
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
	display := make([]string, len(models))
	for i, name := range models {
		display[i] = name
		if name == m.ag.Client.Model {
			display[i] = name + "  ● aktif"
		}
	}
	m.picker = newPicker("pilih model", display, models, func(mm *model, name string) {
		mm.ag.Client.Model = name
		mm.cfg.Model.Name = name
		config.SaveModel(name)
		mm.lines = append(mm.lines, line{lineInfo, stMuted.Render("model: " + name + " (tersimpan)")})
	})
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
	models, err := client.ListModels(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "hiroto:", err)
		os.Exit(1)
	}
	if len(models) == 0 {
		fmt.Fprintln(os.Stderr, "hiroto: endpoint tidak mengembalikan model")
		os.Exit(1)
	}
	display := make([]string, len(models))
	for i, name := range models {
		display[i] = name
		if name == cfg.Model.Name {
			display[i] = name + "  ● aktif"
		}
	}
	idx := runCLIPicker("pilih model (tersimpan ke config.yaml)", display)
	if idx < 0 || idx >= len(models) {
		return
	}
	config.SaveModel(models[idx])
	fmt.Println("model tersimpan:", models[idx])
}
