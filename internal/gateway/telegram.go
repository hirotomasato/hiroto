// Package gateway connects Hiroto to messaging platforms.
// Currently supports the Telegram Bot API (long polling).
package gateway

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/hirotomasato/hiroto/internal/agent"
	"github.com/hirotomasato/hiroto/internal/llm"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// chat keeps per-chat conversation state so users never share context.
type chat struct {
	messages []llm.Message
}

// Telegram starts a polling bot that forwards messages to the agent.
// Each chat gets its own isolated conversation (messages are swapped in and
// out of the shared agent around every turn).
func Telegram(token string, ag *agent.Agent) error {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	bot.Debug = false
	log.Printf("[gateway] Telegram bot aktif sebagai @%s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	chats := make(map[int64]*chat)

	for update := range updates {
		if update.Message == nil {
			continue
		}
		msg := update.Message
		chatID := msg.Chat.ID
		text := strings.TrimSpace(msg.Text)

		// Commands
		switch {
		case text == "/start":
			reply := "◆ Hiroto — personal AI agent\n\n" +
				"Kirim pesan untuk bertanya atau memberi tugas.\n" +
				"/new — mulai sesi baru\n" +
				"/help — bantuan"
			send(bot, chatID, reply, msg.MessageID)
			continue
		case text == "/new":
			delete(chats, chatID)
			send(bot, chatID, "— sesi baru —", msg.MessageID)
			continue
		case text == "/help":
			send(bot, chatID, "Hiroto · personal agent\nKirim pesan untuk bertanya, kasih tugas, atau minta bantuan.\n/new untuk mulai sesi baru.", msg.MessageID)
			continue
		case text == "":
			continue
		}

		// Process through agent with THIS chat's own history.
		log.Printf("[gateway] %s: %s", msg.From.UserName, text)
		typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
		bot.Send(typing)

		st, ok := chats[chatID]
		if !ok {
			st = &chat{}
			chats[chatID] = st
		}
		// Swap in this chat's history; Run appends to ag.Messages, so save it
		// back after the turn so the next message in this chat continues it.
		ag.Messages = st.messages

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		answer, err := ag.Run(ctx, text, nil)
		cancel()
		st.messages = ag.Messages
		if err != nil {
			send(bot, chatID, "✗ error: "+err.Error(), msg.MessageID)
			continue
		}
		if answer == "" {
			answer = "(done)"
		}
		// Truncate long answers for Telegram (4096 char limit)
		if len(answer) > 4000 {
			answer = answer[:4000] + "\n... (dipotong)"
		}
		send(bot, chatID, answer, msg.MessageID)
	}
	return nil
}

func send(bot *tgbotapi.BotAPI, chatID int64, text string, replyTo int) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyToMessageID = replyTo
	msg.ParseMode = tgbotapi.ModeMarkdown
	// If Markdown fails, resend as plain text
	if _, err := bot.Send(msg); err != nil {
		msg.ParseMode = ""
		bot.Send(msg)
	}
}
