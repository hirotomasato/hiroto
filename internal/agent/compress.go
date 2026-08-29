package agent

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/hirotomasato/hiroto/internal/llm"
)

// CompressNow forces a compression regardless of budget (manual trigger).
func (a *Agent) CompressNow(ctx context.Context) error {
	budget := a.CompressBudget
	keep := a.CompressKeepTurns
	if keep <= 0 { keep = 6 }
	keepMsgs := keep * 2
	msgs := a.Messages
	if len(msgs) <= keepMsgs+2 {
		return fmt.Errorf("belum cukup pesan untuk dikompresi (%d pesan)", len(msgs))
	}
	// Temporarily override budget to force compression
	a.CompressBudget = 1
	defer func() { a.CompressBudget = budget }()
	return a.compress(ctx)
}

const compressSystemPrompt = `Anda adalah peringkas percakapan untuk konteks kompresi agen AI.
Ringkas percakapan berikut dengan mempertahankan SEMUA informasi penting:
- Keputusan yang dibuat
- Kode snippet dan tujuannya
- Action items dan statusnya
- Fakta dan informasi yang ditemukan
- Preferensi dan feedback pengguna
- Tools yang dipakai dan hasilnya

Output ringkasan singkat dalam bahasa yang sama dengan percakapan.
Gunakan format bullet-point atau paragraf padat.`

// compress is the auto-trigger: called before each user turn.
func (a *Agent) compress(ctx context.Context) error {
	if a.CompressBudget <= 0 {
		return nil
	}
	budget := a.CompressBudget
	keep := a.CompressKeepTurns
	if keep <= 0 {
		keep = 6
	}
	keepMsgs := keep * 2 // user + assistant per turn (rough; tool results are interleaved)

	msgs := a.Messages
	if len(msgs) <= keepMsgs+4 {
		return nil
	}

	if estimateTokens(msgs) < budget {
		return nil
	}

	toSummarize := msgs[1 : len(msgs)-keepMsgs]
	summaryBlock := buildSummaryBlock(toSummarize)

	a.emit(Event{Type: "compress_start", Text: fmt.Sprintf("meringkas %d pesan…", len(toSummarize))})

	summaryResp, err := a.Client.Chat(ctx, []llm.Message{
		{Role: llm.RoleSystem, Content: compressSystemPrompt},
		{Role: llm.RoleUser, Content: summaryBlock},
	}, nil)
	if err != nil {
		return fmt.Errorf("compression failed: %w", err)
	}

	summary := ""
	if s, ok := summaryResp.Content.(string); ok {
		summary = s
	}

	// Rebuild: system + summary + last K messages
	newMsgs := []llm.Message{msgs[0]}
	newMsgs = append(newMsgs, llm.Message{
		Role:    llm.RoleUser,
		Content: "[RINGKASAN PERCAKAPAN SEBELUMNYA]\n" + summary,
	})
	newMsgs = append(newMsgs, msgs[len(msgs)-keepMsgs:]...)

	oldTokens := estimateTokens(msgs)
	a.Messages = newMsgs
	newTokens := estimateTokens(newMsgs)

	a.emit(Event{Type: "compress_end", Text: fmt.Sprintf("konteks dikompresi: %d → %d token (~%d%% ruang)", oldTokens, newTokens, 100*newTokens/oldTokens)})
	return nil
}

// estimateTokens gives a rough token count from the character lengths.
func estimateTokens(msgs []llm.Message) int {
	chars := 0
	for _, m := range msgs {
		if s, ok := m.Content.(string); ok {
			chars += utf8.RuneCountInString(s) // closer to token count for CJK
		}
		for _, tc := range m.ToolCalls {
			chars += utf8.RuneCountInString(tc.Function.Name) + utf8.RuneCountInString(tc.Function.Arguments)
		}
	}
	// ~3 runes per token for mixed Latin/Asian text; tools use ~0.5 of that
	return chars / 3
}

// buildSummaryBlock formats messages as a readable transcript for the summariser LLM.
func buildSummaryBlock(msgs []llm.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		role := string(m.Role)
		switch role {
		case "user":
			if s, ok := m.Content.(string); ok {
				b.WriteString(fmt.Sprintf("[USER] %s\n", s))
			}
		case "assistant":
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					b.WriteString(fmt.Sprintf("[TOOL CALL] %s(%s)\n", tc.Function.Name, tc.Function.Arguments))
				}
			}
			if s, ok := m.Content.(string); ok && s != "" {
				b.WriteString(fmt.Sprintf("[ASSISTANT] %s\n", s))
			}
		case "tool":
			if s, ok := m.Content.(string); ok {
				short := s
				if len(short) > 300 {
					short = short[:300] + "…"
				}
				b.WriteString(fmt.Sprintf("[TOOL RESULT: %s] %s\n", m.Name, short))
			}
		}
	}
	return b.String()
}