// Package gateway connects Hiroto to messaging platforms.
// Currently supports the Telegram Bot API (long polling).
//
// Commands mirror the TUI: /model, /resume, /memory, /skills. Each chat keeps
// its own isolated conversation; tool activity is streamed live into a single
// edited Telegram message while the agent works.
package gateway

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hirotomasato/hiroto/internal/agent"
	"github.com/hirotomasato/hiroto/internal/config"
	"github.com/hirotomasato/hiroto/internal/llm"
	"github.com/hirotomasato/hiroto/internal/memory"
	"github.com/hirotomasato/hiroto/internal/session"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// chat keeps per-chat conversation state so users never share context.
// It is bound to a stable session ID so the conversation survives restarts.
type chat struct {
	id       string
	messages []llm.Message
}

// sessionIDFor derives a stable, store-safe session id from a Telegram chat id.
func sessionIDFor(chatID int64) string {
	// regexp-safe: digits, letters, '_' and '-' are allowed by the store.
	return fmt.Sprintf("tg%d", chatID)
}

// gw holds the bot runtime state shared across all chats.
type gw struct {
	bot   *tgbotapi.BotAPI
	ag    *agent.Agent
	mem   *memory.Store
	sess  *session.Store
	chats map[int64]*chat
}

// Telegram starts a polling bot that forwards messages to the agent.
func Telegram(token string, ag *agent.Agent) error {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	bot.Debug = false
	log.Printf("[gateway] Telegram bot aktif sebagai @%s", bot.Self.UserName)

	mem := ag.Memory
	if mem == nil {
		mem = memory.New()
	}
	g := &gw{
		bot:   bot,
		ag:    ag,
		mem:   mem,
		sess:  session.New(),
		chats: make(map[int64]*chat),
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		if update.Message == nil {
			continue
		}
		g.handle(update.Message)
	}
	return nil
}

func (g *gw) handle(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)
	replyTo := msg.MessageID

	fields := strings.Fields(text)
	cmd := ""
	if len(fields) > 0 {
		cmd = fields[0]
	}
	arg := strings.TrimSpace(strings.TrimPrefix(text, cmd))

	switch cmd {
	case "/start":
		send(g.bot, chatID, "◆ Hiroto — personal AI agent\n\n"+
			"Kirim pesan untuk bertanya atau memberi tugas.\n\n"+
			"/new — mulai sesi baru\n"+
			"/model — lihat & pilih model\n"+
			"/resume — lanjut sesi tersimpan\n"+
			"/memory — kelola memory\n"+
			"/skills — daftar skill\n"+
			"/help — bantuan", replyTo)
		return

	case "/help":
		send(g.bot, chatID, "◆ Hiroto — bantuan\n\n"+
			"/new — sesi baru\n"+
			"/model — lihat model, /model <n> pilih\n"+
			"/resume — list sesi, /resume <id> lanjut\n"+
			"/memory — lihat, /memory add <teks>, /memory del <id>\n"+
			"/skills [kata] — cari skill\n"+
			"/todo — catat tugas via agent", replyTo)
		return

	case "/new":
		delete(g.chats, chatID)
		send(g.bot, chatID, "— sesi baru —", replyTo)
		return

	case "/model":
		g.handleModel(chatID, arg, replyTo)
		return

	case "/resume":
		g.handleResume(chatID, arg, replyTo)
		return

	case "/memory":
		g.handleMemory(chatID, arg, text, replyTo)
		return

	case "/skills":
		g.handleSkills(chatID, arg, replyTo)
		return

	case "/todo":
		send(g.bot, chatID, "todo dibaca agent via tool — ketik aja tugasnya, agent yang catat.", replyTo)
		return

	case "":
		return
	}

	if strings.HasPrefix(cmd, "/") {
		send(g.bot, chatID, "❓ perintah tidak dikenal: "+cmd+" — ketik /help", replyTo)
		return
	}

	// ---- free-form message -> agent ----
	log.Printf("[gateway] %s: %s", msg.From.UserName, text)
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	g.bot.Send(typing)

	st, ok := g.chats[chatID]
	if !ok {
		st = &chat{id: sessionIDFor(chatID)}
		// resume any prior conversation for this chat that survived a restart
		if saved, err := g.sess.Load(st.id); err == nil {
			st.messages = fromStored(saved.Messages)
		}
		g.chats[chatID] = st
	}
	ag := g.ag
	ag.Messages = st.messages

	live := newLive(g.bot, chatID)
	ag.Emit = func(e agent.Event) {
		switch e.Type {
		case "tool_start":
			live.push("▸ " + e.ToolName + " …")
		case "tool_end":
			live.push("  ✓ " + e.ToolName + " (" + e.Duration.Round(100*time.Millisecond).String() + ")")
		case "error":
			live.push("  ✗ " + e.Text)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	answer, err := ag.Run(ctx, text, live.text)
	cancel()
	ag.Emit = nil
	st.messages = ag.Messages

	if err != nil {
		live.finish("✗ error: " + err.Error())
		return
	}
	if answer == "" {
		answer = "(done)"
	}
	g.saveChat(st)
	live.finish(answer)
}

// saveChat persists this chat's conversation under its stable session id so
// it survives a restart and can be resumed from the TUI or gateway.
func (g *gw) saveChat(st *chat) {
	users := 0
	for _, m := range st.messages {
		if m.Role == llm.RoleUser {
			users++
		}
	}
	if users == 0 {
		return
	}
	title := firstUserText(st.messages, 60)
	sess := &session.Session{
		ID:       st.id,
		Title:    title,
		Model:    g.ag.Client.Model,
		Created:  time.Now(),
		Updated:  time.Now(),
		Messages: toStored(st.messages),
	}
	_ = g.sess.Save(sess)
}

func firstUserText(msgs []llm.Message, max int) string {
	for _, m := range msgs {
		if m.Role == llm.RoleUser {
			if s, ok := m.Content.(string); ok {
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

// toStored converts llm messages for persistence.
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

// ---- command handlers ----

func (g *gw) handleModel(chatID int64, arg string, replyTo int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := g.ag.Client.ListModels(ctx)
	if err != nil {
		send(g.bot, chatID, "✗ gagal ambil model: "+err.Error(), replyTo)
		return
	}
	if len(models) == 0 {
		send(g.bot, chatID, "⚠ endpoint tidak mengembalikan model", replyTo)
		return
	}

	if arg != "" {
		idx := -1
		if n, err := fmt.Sscanf(arg, "%d", &idx); err == nil && n == 1 && idx >= 0 && idx < len(models) {
			g.ag.Client.Model = models[idx]
			config.SaveModel(models[idx])
			send(g.bot, chatID, "✓ model: "+models[idx]+" (tersimpan)", replyTo)
			return
		}
		for _, m := range models {
			if m == arg {
				g.ag.Client.Model = m
				config.SaveModel(m)
				send(g.bot, chatID, "✓ model: "+m+" (tersimpan)", replyTo)
				return
			}
		}
		send(g.bot, chatID, "✗ model tidak ditemukan — /model buat lihat daftar.", replyTo)
		return
	}

	var b strings.Builder
	b.WriteString("◆ Model tersedia — balas /model <n> untuk ganti\n\n")
	for i, m := range models {
		mark := "  "
		if m == g.ag.Client.Model {
			mark = "• "
		}
		fmt.Fprintf(&b, "%s%d. %s\n", mark, i, m)
	}
	send(g.bot, chatID, b.String(), replyTo)
}

func (g *gw) handleResume(chatID int64, arg string, replyTo int) {
	if arg != "" {
		sess, err := g.sess.Load(arg)
		if err != nil {
			send(g.bot, chatID, "✗ sesi tidak ditemukan: "+arg, replyTo)
			return
		}
		st, ok := g.chats[chatID]
		if !ok {
			st = &chat{}
			g.chats[chatID] = st
		}
		st.messages = fromStored(sess.Messages)
		if sess.Model != "" {
			g.ag.Client.Model = sess.Model
		}
		send(g.bot, chatID, "✓ sesi dilanjutkan: "+sess.ID+" · "+sess.Title, replyTo)
		return
	}

	list := g.sess.List()
	if len(list) == 0 {
		send(g.bot, chatID, "belum ada sesi tersimpan", replyTo)
		return
	}
	var b strings.Builder
	b.WriteString("◆ Sesi tersimpan — balas /resume <id>\n\n")
	for i, s := range list {
		if i >= 10 {
			fmt.Fprintf(&b, "… dan %d sesi lagi\n", len(list)-10)
			break
		}
		fmt.Fprintf(&b, "%s · %s\n", s.ID, s.Title)
	}
	send(g.bot, chatID, b.String(), replyTo)
}

func (g *gw) handleMemory(chatID int64, arg, text string, replyTo int) {
	if strings.HasPrefix(arg, "add") {
		content := strings.TrimSpace(strings.TrimPrefix(text, "/memory add"))
		if content == "" {
			send(g.bot, chatID, "pakai: /memory add <teks>", replyTo)
			return
		}
		id := g.mem.Add("memory", content)
		send(g.bot, chatID, "✓ memory tersimpan: "+id, replyTo)
		return
	}
	if strings.HasPrefix(arg, "del") {
		which := strings.TrimSpace(strings.TrimPrefix(text, "/memory del"))
		if which == "" {
			send(g.bot, chatID, "pakai: /memory del <id|kata>", replyTo)
			return
		}
		if g.mem.Remove("memory", which) {
			send(g.bot, chatID, "✓ dihapus", replyTo)
		} else {
			send(g.bot, chatID, "✗ tidak ditemukan", replyTo)
		}
		return
	}

	block := g.mem.PromptBlock()
	if block == "" {
		block = "(kosong)"
	}
	send(g.bot, chatID, "◆ Memory\n\n"+block, replyTo)
}

func (g *gw) handleSkills(chatID int64, arg string, replyTo int) {
	all := g.ag.Skills
	if len(all) == 0 {
		send(g.bot, chatID, "belum ada skill terindex", replyTo)
		return
	}
	q := strings.ToLower(strings.TrimSpace(arg))
	names := make([]string, 0, len(all))
	for _, s := range all {
		if q == "" || strings.Contains(strings.ToLower(s.Name), q) {
			names = append(names, s.Name)
		}
	}
	sort.Strings(names)

	var b strings.Builder
	fmt.Fprintf(&b, "◆ Skills (%d", len(names))
	if q != "" {
		fmt.Fprintf(&b, " cocok %q", arg)
	}
	b.WriteString(")\n\n")
	for i, n := range names {
		if i >= 50 {
			fmt.Fprintf(&b, "… dan %d lagi (sempitin: /skills <kata>)\n", len(names)-50)
			break
		}
		b.WriteString("· " + n + "\n")
	}
	send(g.bot, chatID, b.String(), replyTo)
}

func send(bot *tgbotapi.BotAPI, chatID int64, text string, replyTo int) {
	msg := tgbotapi.NewMessage(chatID, text)
	if replyTo != 0 {
		msg.ReplyToMessageID = replyTo
	}
	msg.ParseMode = tgbotapi.ModeMarkdown
	if _, err := bot.Send(msg); err != nil {
		msg.ParseMode = ""
		bot.Send(msg)
	}
}

// fromStored rebuilds llm messages from persisted session messages.
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

// live streams tool activity + assistant text into a single Telegram message,
// editing it as new content arrives (throttled to avoid rate limits).
// finish() performs a final flush so the last chunks always land.
type live struct {
	bot    *tgbotapi.BotAPI
	chatID int64

	mu    sync.Mutex
	tools []string
	body  string
	msgID int
	last  time.Time
}

const maxLiveLen = 4000 // Telegram message limit; cap the streamed body below it.

func newLive(bot *tgbotapi.BotAPI, chatID int64) *live {
	return &live{bot: bot, chatID: chatID}
}

// render builds the current message body from tool lines + streamed text.
func (l *live) render() string {
	if len(l.tools) == 0 {
		return l.body
	}
	if l.body == "" {
		return strings.Join(l.tools, "\n")
	}
	return strings.Join(l.tools, "\n") + "\n\n" + l.body
}

// push adds a fixed tool-activity line (started / finished / error).
func (l *live) push(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tools = append(l.tools, line)
	l.flushLocked()
}

// text appends a streamed assistant chunk (capped for Telegram's limit).
func (l *live) text(chunk string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.body) >= maxLiveLen {
		return
	}
	room := maxLiveLen - len(l.body)
	if len(chunk) > room {
		chunk = chunk[:room]
	}
	l.body += chunk
	l.flushLocked()
}

func (l *live) flushLocked() {
	if l.msgID == 0 {
		l.sendLocked(l.render())
	} else if time.Since(l.last) > 400*time.Millisecond {
		l.editLocked(l.render())
	}
}

// finish flushes whatever is pending; answer is the fallback when nothing was
// streamed (or the final text when streaming produced no deltas).
func (l *live) finish(answer string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.body == "" && answer != "" {
		l.body = answer
		if len(l.body) > maxLiveLen {
			l.body = l.body[:maxLiveLen]
		}
	}
	if l.msgID == 0 {
		l.sendLocked(l.render())
		return
	}
	l.editLocked(l.render())
}

func (l *live) sendLocked(body string) {
	m := tgbotapi.NewMessage(l.chatID, body)
	if out, err := l.bot.Send(m); err == nil {
		l.msgID = out.MessageID
	}
	l.last = time.Now()
}

func (l *live) editLocked(body string) {
	if l.msgID == 0 {
		return
	}
	e := tgbotapi.NewEditMessageText(l.chatID, l.msgID, body)
	l.bot.Send(e)
	l.last = time.Now()
}
