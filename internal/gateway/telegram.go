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
	"github.com/hirotomasato/hiroto/internal/tools"

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

// Options tunes gateway presentation (Hermes-style progress controls).
type Options struct {
	// ToolProgress: "all" (default) | "new" | "off".
	ToolProgress string
	// CleanupProgress deletes the progress bubble after the final answer lands
	// (successful turns only).
	CleanupProgress bool
	// TypingIndicator shows the "typing…" chat action while working.
	TypingIndicator bool
	// AllowedUsers is the set of Telegram user IDs permitted to use the bot.
	// Empty = deny everyone (safe default for a terminal-capable bot).
	AllowedUsers []int64
}

func (o Options) progressMode() string {
	switch o.ToolProgress {
	case "new", "off":
		return o.ToolProgress
	default:
		return "all"
	}
}

// gw holds the bot runtime state shared across all chats.
type gw struct {
	bot     *tgbotapi.BotAPI
	ag      *agent.Agent
	mem     *memory.Store
	sess    *session.Store
	todos   *tools.TodoStore // shared with the todo tool; retargeted per chat turn
	chats   map[int64]*chat
	cancels map[int64]context.CancelFunc // per-chat cancel for /stop
	cancMu  sync.Mutex
	opts    Options
	allowed map[int64]bool // Telegram user IDs permitted to use the bot
}

// isAllowed reports whether a Telegram user may use the bot. An empty allowlist
// denies everyone (safe default for a bot with terminal access).
func (g *gw) isAllowed(userID int64) bool {
	return g.allowed[userID]
}

// registerCommandMenu populates Telegram's "/" command picker (Hermes-style)
// so users discover commands without typing /help. Best-effort.
func registerCommandMenu(bot *tgbotapi.BotAPI) {
	cmds := []tgbotapi.BotCommand{
		{Command: "new", Description: "Start a new session"},
		{Command: "model", Description: "Show or switch model"},
		{Command: "resume", Description: "Resume a saved session"},
		{Command: "sessions", Description: "List / search sessions"},
		{Command: "memory", Description: "View or edit memory"},
		{Command: "skills", Description: "List skills"},
		{Command: "retry", Description: "Retry the last turn"},
		{Command: "undo", Description: "Undo the last turn"},
		{Command: "stop", Description: "Stop the running agent"},
		{Command: "status", Description: "Session & model info"},
		{Command: "help", Description: "Show help"},
	}
	cfg := tgbotapi.NewSetMyCommands(cmds...)
	if _, err := bot.Request(cfg); err != nil {
		log.Printf("[gateway] set command menu: %v", err)
	}
}

// Telegram starts a polling bot that forwards messages to the agent.
func Telegram(token string, ag *agent.Agent, opts Options) error {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	bot.Debug = false
	log.Printf("[gateway] Telegram bot aktif sebagai @%s", bot.Self.UserName)

	// Register the command menu so Telegram's "/" button lists our commands
	// (Hermes-style). Best-effort — a failure here must not stop the bot.
	registerCommandMenu(bot)

	mem := ag.Memory
	if mem == nil {
		mem = memory.New()
	}
	g := &gw{
		bot:     bot,
		ag:      ag,
		mem:     mem,
		sess:    session.New(),
		todos:   tools.SharedTodo(),
		chats:   make(map[int64]*chat),
		cancels: make(map[int64]context.CancelFunc),
		opts:    opts,
		allowed: make(map[int64]bool),
	}
	for _, id := range opts.AllowedUsers {
		g.allowed[id] = true
	}
	if len(g.allowed) == 0 {
		log.Printf("[gateway] ⚠ allowlist KOSONG — semua user ditolak. Set gateway.allowed_users di config.yaml atau HIROTO_TELEGRAM_ALLOWED_USERS di .env")
	} else {
		log.Printf("[gateway] allowlist: %d user diizinkan", len(g.allowed))
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

	// Access control: only allowlisted Telegram user IDs may use the bot.
	// This bot has full terminal access, so an empty allowlist denies all.
	var userID int64
	if msg.From != nil {
		userID = msg.From.ID
	}
	if !g.isAllowed(userID) {
		log.Printf("[gateway] tolak user %d (chat %d)", userID, chatID)
		send(g.bot, chatID, fmt.Sprintf(
			"⛔ Akses ditolak.\n\nID Telegram lo: %d\n\nMinta owner nambahin ID ini ke gateway.allowed_users (config.yaml) atau HIROTO_TELEGRAM_ALLOWED_USERS (.env), lalu restart gateway.",
			userID), replyTo)
		return
	}

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
			"/retry — ulang giliran terakhir\n"+
			"/undo — batalkan giliran terakhir\n"+
			"/stop — hentikan agent yang sedang jalan\n"+
			"/status — info sesi & model\n"+
			"/help — bantuan", replyTo)
		return

	case "/help":
		send(g.bot, chatID, "◆ Hiroto — bantuan\n\n"+
			"/new — sesi baru\n"+
			"/model — lihat model, /model <n> pilih, /model <nama> [--once]\n"+
			"/resume — list sesi, /resume <id> lanjut\n"+
			"/memory — lihat, /memory add <teks>, /memory del <id>\n"+
			"/skills [kata] — cari skill\n"+
			"/retry — ulang giliran terakhir\n"+
			"/undo — batalkan giliran terakhir\n"+
			"/stop — hentikan agent yang sedang jalan\n"+
			"/compress — ringkas konteks percakapan\n"+
			"/status — info sesi & model\n"+
			"/sessions [cari] — cari sesi\n"+
			"/todo — lihat task, /todo done <id>, /todo unstick, /todo clear", replyTo)
		return

	case "/new":
		delete(g.chats, chatID)
		// A new conversation gets a clean plan (the id is chat-stable, so the
		// old checklist file would otherwise be inherited).
		g.bindTodos(chatID)
		if g.todos != nil {
			g.todos.Clear()
		}
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
		g.handleTodo(chatID, arg, replyTo)
		return

	case "/retry":
		g.handleRetry(chatID, replyTo)
		return

	case "/undo":
		g.handleUndo(chatID, replyTo)
		return

	case "/status":
		g.handleStatus(chatID, replyTo)
		return

	case "/stop":
		g.handleStop(chatID, replyTo)
		return

	case "/compress":
		g.handleCompress(chatID, replyTo)
		return

	case "/sessions":
		g.handleSessions(chatID, arg, replyTo)
		return

	case "":
		return
	}

	if strings.HasPrefix(cmd, "/") {
		send(g.bot, chatID, "❓ perintah tidak dikenal: "+cmd+" — ketik /help", replyTo)
		return
	}

	// ---- free-form message -> agent ----
	st, ok := g.chats[chatID]
	if !ok {
		st = &chat{id: sessionIDFor(chatID)}
		// resume any prior conversation for this chat that survived a restart
		if saved, err := g.sess.Load(st.id); err == nil {
			st.messages = fromStored(saved.Messages)
		}
		g.chats[chatID] = st
	}
	g.runAgentTurn(chatID, st, text, replyTo)
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
		Todos:    todosToStored(g.todos),
	}
	_ = g.sess.Save(sess)
}

// todosToStored snapshots the checklist into the session file so a gateway
// restart doesn't lose the plan.
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

// restoreTodos loads a saved session's plan into the store, never as running.
func restoreTodos(ts *tools.TodoStore, sess *session.Session) {
	if ts == nil || sess == nil {
		return
	}
	ts.Retarget(sess.ID)
	if len(sess.Todos) > 0 {
		items := make([]tools.TodoItem, 0, len(sess.Todos))
		for _, t := range sess.Todos {
			items = append(items, tools.TodoItem{ID: t.ID, Content: t.Content, Status: t.Status})
		}
		ts.Save(items)
	}
	ts.Demote()
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
	// Parse flags: --once changes model for this session only.
	once := false
	if strings.HasSuffix(arg, " --once") {
		once = true
		arg = strings.TrimSuffix(arg, " --once")
	}
	if strings.HasSuffix(arg, " --global") {
		arg = strings.TrimSuffix(arg, " --global")
	}

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
			if !once {
				config.SaveModel(models[idx])
			}
			kind := "(tersimpan)"
			if once {
				kind = "(sesi ini)"
			}
			send(g.bot, chatID, "✓ model: "+models[idx]+" "+kind, replyTo)
			return
		}
		for _, m := range models {
			if m == arg {
				g.ag.Client.Model = m
				if !once {
					config.SaveModel(m)
				}
				kind := "(tersimpan)"
				if once {
					kind = "(sesi ini)"
				}
				send(g.bot, chatID, "✓ model: "+m+" "+kind, replyTo)
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
	b.WriteString("\n/model <nama> --once → cuma sesi ini")
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
		st.id = sess.ID
		restoreTodos(g.todos, sess)
		msg := "✓ sesi dilanjutkan: " + sess.ID + " · " + sess.Title
		if done, total := g.todos.Counts(); total > 0 {
			msg += fmt.Sprintf("\ntask list: %d/%d selesai", done, total)
		}
		send(g.bot, chatID, msg, replyTo)
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

func (g *gw) handleRetry(chatID int64, replyTo int) {
	st, ok := g.chats[chatID]
	if !ok || len(st.messages) == 0 {
		send(g.bot, chatID, "belum ada percakapan untuk diulang", replyTo)
		return
	}
	userText, ok := popLastUserTurn(st.messages)
	if !ok {
		send(g.bot, chatID, "tidak ada giliran user untuk diulang", replyTo)
		return
	}
	st.messages = st.messages[:len(st.messages)-userText.removed]
	g.runAgentTurn(chatID, st, userText.text, replyTo)
}

func (g *gw) handleUndo(chatID int64, replyTo int) {
	st, ok := g.chats[chatID]
	if !ok || len(st.messages) == 0 {
		send(g.bot, chatID, "belum ada percakapan", replyTo)
		return
	}
	userText, ok := popLastUserTurn(st.messages)
	if !ok {
		send(g.bot, chatID, "tidak ada giliran user untuk dibatalkan", replyTo)
		return
	}
	st.messages = st.messages[:len(st.messages)-userText.removed]
	send(g.bot, chatID, "✓ giliran dibatalkan ("+truncate(userText.text, 80)+")", replyTo)
}

func (g *gw) handleStatus(chatID int64, replyTo int) {
	st, ok := g.chats[chatID]
	msgCount := 0
	if ok {
		msgCount = len(st.messages)
	}
	var b strings.Builder
	b.WriteString("◆ Status\n\n")
	fmt.Fprintf(&b, "model: %s\n", g.ag.Client.Model)
	fmt.Fprintf(&b, "pesan: %d\n", msgCount)
	if ok {
		fmt.Fprintf(&b, "sesi: %s\n", st.id)
	}
	if g.todos != nil {
		g.bindTodos(chatID)
		if done, total := g.todos.Counts(); total > 0 {
			fmt.Fprintf(&b, "task: %d/%d selesai\n", done, total)
		}
	}
	b.WriteString("gateway: telegram polling")
	send(g.bot, chatID, b.String(), replyTo)
}

func (g *gw) handleSessions(chatID int64, arg string, replyTo int) {
	var list []session.Session
	if arg != "" {
		list = g.sess.Search(arg)
	} else {
		list = g.sess.List()
	}
	if len(list) == 0 {
		if arg != "" {
			send(g.bot, chatID, "tidak ada sesi cocok: "+arg, replyTo)
		} else {
			send(g.bot, chatID, "belum ada sesi tersimpan", replyTo)
		}
		return
	}
	var b strings.Builder
	if arg != "" {
		fmt.Fprintf(&b, "◆ Sesi cocok %q (%d)\n\n", arg, len(list))
	} else {
		b.WriteString("◆ Sesi tersimpan\n\n")
	}
	for i, s := range list {
		if i >= 15 {
			fmt.Fprintf(&b, "… dan %d sesi lagi\n", len(list)-15)
			break
		}
		fmt.Fprintf(&b, "%s · %s\n", s.ID, s.Title)
	}
	send(g.bot, chatID, b.String(), replyTo)
}

// runAgentTurn runs the agent for a single user turn and streams the result.
// It is shared between handle (free-form messages) and handleRetry.
func (g *gw) runAgentTurn(chatID int64, st *chat, text string, replyTo int) {
	log.Printf("[gateway] %s", text)
	if g.opts.TypingIndicator {
		typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
		g.bot.Send(typing)
	}

	ag := g.ag
	ag.Messages = st.messages
	// Bind the checklist to this chat's session before the agent can call the
	// todo tool, so chats never write into each other's plan.
	g.bindTodos(chatID)

	live := newLive(g.bot, chatID)
	live.cleanup = g.opts.CleanupProgress
	progress := g.opts.progressMode()
	ag.Emit = func(e agent.Event) {
		if progress == "off" {
			return
		}
		switch e.Type {
		case "tool_start":
			live.push("▸ " + agent.ActivityLabel(e.ToolName, e.ToolArgs) + " …")
		case "tool_end":
			if progress == "new" {
				return // "new" shows only start lines
			}
			live.push("  ✓ " + agent.ActivityLabel(e.ToolName, e.ToolArgs) + " (" + e.Duration.Round(100*time.Millisecond).String() + ")")
		case "compress_start", "compress_end":
			live.push("⚡ " + e.Text)
		case "error":
			live.push("  ✗ " + e.Text)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	g.cancMu.Lock()
	g.cancels[chatID] = cancel
	g.cancMu.Unlock()
	answer, err := ag.Run(ctx, text, live.text)
	cancel()
	g.cancMu.Lock()
	delete(g.cancels, chatID)
	g.cancMu.Unlock()
	ag.Emit = nil
	st.messages = ag.Messages

	if err != nil {
		// The run died (cancel/timeout/provider error): nothing is working on
		// the plan any more, so don't leave a task claiming to be in progress.
		if g.todos != nil {
			g.todos.Demote()
		}
		if ctx.Err() == context.Canceled {
			live.finish("■ dihentikan")
			g.saveChat(st)
			return
		}
		live.finish("✗ error: " + err.Error())
		g.saveChat(st)
		return
	}
	if answer == "" {
		answer = "(done)"
	}
	g.saveChat(st)
	live.finish(answer)
}

// handleStop cancels the agent turn currently running for this chat (if any).
func (g *gw) handleStop(chatID int64, replyTo int) {
	g.cancMu.Lock()
	cancel, ok := g.cancels[chatID]
	g.cancMu.Unlock()
	if !ok {
		send(g.bot, chatID, "tidak ada proses yang sedang jalan", replyTo)
		return
	}
	cancel()
	send(g.bot, chatID, "■ menghentikan…", replyTo)
}

// handleCompress summarizes older context for this chat's session.
func (g *gw) handleCompress(chatID int64, replyTo int) {
	st, ok := g.chats[chatID]
	if !ok || len(st.messages) == 0 {
		send(g.bot, chatID, "belum ada percakapan untuk diringkas", replyTo)
		return
	}
	ag := g.ag
	ag.Messages = st.messages
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := ag.CompressNow(ctx); err != nil {
		send(g.bot, chatID, "✗ kompresi gagal: "+err.Error(), replyTo)
		return
	}
	st.messages = ag.Messages
	g.saveChat(st)
	send(g.bot, chatID, "✓ konteks diringkas", replyTo)
}

// lastUserTurn holds the text of the last user message and how many messages
// to remove (including the user message itself) to undo/retry that turn.
type lastUserTurn struct {
	text    string
	removed int
}

// popLastUserTurn finds the last user message in the slice and returns its
// text + how many messages to truncate (the user message and everything after).
func popLastUserTurn(msgs []llm.Message) (lastUserTurn, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleUser {
			text := ""
			if s, ok := msgs[i].Content.(string); ok {
				text = s
			}
			return lastUserTurn{text: text, removed: len(msgs) - i}, true
		}
	}
	return lastUserTurn{}, false
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
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

	mu      sync.Mutex
	tools   []string
	body    string
	msgID   int
	last    time.Time
	cleanup bool // delete the progress bubble after a successful final answer
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

// finish flushes progress, then delivers the final answer as its own
// Markdown-formatted message (Hermes-style: progress breadcrumbs stay in the
// working bubble; the answer lands clean below). answer is the model's final
// text; when streaming produced deltas they already live in l.body.
func (l *live) finish(answer string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	final := answer
	if final == "" {
		final = l.body // streamed-only turn: promote the streamed body
	}

	// If there were no tool breadcrumbs, the working bubble IS the answer
	// bubble — just finalize it in place with Markdown.
	if len(l.tools) == 0 {
		l.body = clipLive(final)
		if l.msgID == 0 {
			l.sendFinalLocked(l.body)
		} else {
			l.editFinalLocked(l.body)
		}
		return
	}

	// Tool breadcrumbs exist. Either delete the progress bubble (cleanup) or
	// collapse it to a compact "done" summary, then send the answer fresh.
	if l.cleanup && l.msgID != 0 {
		l.deleteLocked()
	} else if l.msgID != 0 {
		summary := fmt.Sprintf("✓ selesai · %d langkah", len(l.tools))
		l.editLocked(summary)
	}
	l.body = ""
	l.msgID = 0
	l.tools = nil
	l.sendFinalLocked(clipLive(final))
}

// clipLive trims a body to Telegram's message limit.
func clipLive(s string) string {
	if len(s) > maxLiveLen {
		return s[:maxLiveLen] + "…"
	}
	return s
}

func (l *live) sendLocked(body string) {
	m := tgbotapi.NewMessage(l.chatID, body)
	if out, err := l.bot.Send(m); err == nil {
		l.msgID = out.MessageID
	}
	l.last = time.Now()
}

// sendFinalLocked sends a message with Markdown formatting, falling back to
// plain text if Telegram rejects the markup (unbalanced * or _ in output).
func (l *live) sendFinalLocked(body string) {
	if body == "" {
		return
	}
	m := tgbotapi.NewMessage(l.chatID, body)
	m.ParseMode = tgbotapi.ModeMarkdown
	if _, err := l.bot.Send(m); err != nil {
		m.ParseMode = ""
		l.bot.Send(m)
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

// deleteLocked removes the progress bubble (cleanup mode). Best-effort.
func (l *live) deleteLocked() {
	if l.msgID == 0 {
		return
	}
	l.bot.Request(tgbotapi.NewDeleteMessage(l.chatID, l.msgID))
}

// editFinalLocked edits the working bubble into the final Markdown answer,
// falling back to plain text if the markup is rejected.
func (l *live) editFinalLocked(body string) {
	if l.msgID == 0 || body == "" {
		return
	}
	e := tgbotapi.NewEditMessageText(l.chatID, l.msgID, body)
	e.ParseMode = tgbotapi.ModeMarkdown
	if _, err := l.bot.Send(e); err != nil {
		e.ParseMode = ""
		l.bot.Send(e)
	}
	l.last = time.Now()
}
