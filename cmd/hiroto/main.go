package main

// Hiroto TUI — a modern Bubble Tea chat interface (Hiroto TUI-style):
// streaming assistant text, tool activity lines, spinners, session log.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hirotomasato/hiroto/internal/agent"
	"github.com/hirotomasato/hiroto/internal/api"
	"github.com/hirotomasato/hiroto/internal/config"
	"github.com/hirotomasato/hiroto/internal/gateway"
	"github.com/hirotomasato/hiroto/internal/llm"
	"github.com/hirotomasato/hiroto/internal/memory"
	"github.com/hirotomasato/hiroto/internal/plugin"
	"github.com/hirotomasato/hiroto/internal/session"
	"github.com/hirotomasato/hiroto/internal/skills"
	"github.com/hirotomasato/hiroto/internal/tools"
	"github.com/hirotomasato/hiroto/internal/web"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// ---------- styles (Hiroto-skin-inspired palette) ----------
var (
	cPrimary  = lipgloss.Color("#E8A33D") // warm gold like Hiroto's default skin
	cMuted    = lipgloss.Color("#6E6A5E")
	cUser     = lipgloss.Color("#7FB4E8")
	cTool     = lipgloss.Color("#8FBF7F")
	cErr      = lipgloss.Color("#E06C60")
	cBgFaint  = lipgloss.Color("#3A3630")
	cBackdrop = lipgloss.Color("#141210")

	stBanner  = lipgloss.NewStyle().Bold(true).Foreground(cPrimary)
	stUserTag = lipgloss.NewStyle().Bold(true).Foreground(cUser)
	stAsstTag = lipgloss.NewStyle().Bold(true).Foreground(cPrimary)
	stToolTag = lipgloss.NewStyle().Foreground(cTool)
	stMuted   = lipgloss.NewStyle().Foreground(cMuted)
	stErr     = lipgloss.NewStyle().Foreground(cErr)
	stInput   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBgFaint).Padding(0, 1)
	stHelp    = lipgloss.NewStyle().Foreground(cMuted)

	// Flash card (floating error/info popup — Hermes-style).
	stFlashErr  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cErr).Padding(0, 1).Foreground(cErr)
	stFlashInfo = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cPrimary).Padding(0, 1).Foreground(cPrimary)
)

// ---------- model ----------
type lineKind int

const (
	lineUser lineKind = iota
	lineAssistant
	lineTool
	lineInfo
	lineError
)

type line struct {
	kind lineKind
	text string
}

type (
	streamMsg        string        // assistant text chunk
	eventMsg         = agent.Event // alias: tool_start / tool_end / error
	assistantDoneMsg struct{}      // turn finished
	clarifyMsg       struct {
		req tools.ClarifyRequest
	}
)

type model struct {
	cfg       *config.Config
	ag        *agent.Agent
	mem       *memory.Store
	sessStore *session.Store
	sessID    string
	picker    *pickerState
	lines     []line
	vp        viewport.Model
	input     textarea.Model
	spinner   spinner.Model
	busy      bool
	cancel    context.CancelFunc
	width     int
	height    int

	clarifyQuestion string
	clarifyResp     chan string

	history []string // input history
	histIdx int

	startedAt time.Time // session start (exit summary duration)

	streaming bool // true when assistant is writing text (vs thinking or running tools)
	verbose   int  // 0=compact, 1=full (tool output verbosity)
	steerCh   chan string
	goal      string // standing goal across turns
	reasoning string // reasoning effort level
	flash     string // temporary flash message (error/warning card)
	flashKind string // "error" or "info"

	// Command picker (slash suggestions).
	cmdPicker struct {
		active  bool
		items   []string
		cursor  int
	}
}

func initialModel(cfg *config.Config, ag *agent.Agent, mem *memory.Store, ss *session.Store) model {
	ta := textarea.New()
	ta.Placeholder = "ketik pesan untuk hiroto…"
	ta.ShowLineNumbers = false
	promptStyle := lipgloss.NewStyle().Foreground(cPrimary).Bold(true)
	ta.SetPromptFunc(lipgloss.Width("HR ❯ "), func(lineIdx int) string {
		if lineIdx == 0 {
			return promptStyle.Render("HR ❯ ")
		}
		return promptStyle.Render("    ")
	})
	ta.CharLimit = 8000
	ta.SetHeight(2)
	ta.Focus()
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(cPrimary)))
	m := model{cfg: cfg, ag: ag, mem: mem, sessStore: ss, sessID: newSessionID(), input: ta, spinner: sp, histIdx: -1, startedAt: time.Now()}
	m.vp = viewport.New(80, 20)
	m.vp.KeyMap = viewport.KeyMap{} // disable default pager keys — we handle scrolling ourselves
	m.vp.SetContent("")
	m.banner()
	return m
}

func (m *model) banner() {
	w := m.width
	if w == 0 {
		w = 80
	}
	for _, ln := range bannerLines(w, m.cfg.Model.Name, len(m.ag.Skills)) {
		m.lines = append(m.lines, line{lineInfo, ln})
	}
	// Check for updates on startup
	go func() {
		if latest, ok := checkUpdate(); ok {
			streamCh <- eventMsg(agent.Event{Type: "error", Text: "◆ update tersedia: v" + latest + " — ketik /upgrade"})
		}
	}()
}

// ---------- agent plumbing ----------

func (m *model) renderAgentEvent(e agent.Event) line {
	switch e.Type {
	case "tool_start":
		return line{lineTool, stToolTag.Render("⚒ "+e.ToolName) + stMuted.Render(" …")}
	case "tool_end":
		head := stToolTag.Render("⚒ "+e.ToolName) + stMuted.Render(fmt.Sprintf(" (%.1fs)", e.Duration.Seconds()))
		body := e.Text
		// Verbose: 0=compact (first few lines), 1=full, 2=log (all)
		if m.verbose == 0 {
			lines := strings.Split(body, "\n")
			if len(lines) > 3 {
				body = strings.Join(lines[:3], "\n") + "\n…"
			}
		}
		if len(body) > 300 {
			body = body[:300] + "…"
		}
		return line{lineTool, head + "\n" + stMuted.Render(indent(body, "    "))}
	case "compress_start", "compress_end":
		return line{lineInfo, stMuted.Render("⚡ " + e.Text)}
	case "error":
		return line{lineError, stErr.Render("✗ " + e.Text)}
	}
	return line{lineInfo, e.Text}
}

func indent(s, pad string) string {
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m *model) runTurn(text string) {
	m.busy = true
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	// Steer channel: buffer of 1 so /steer doesn't block.
	m.steerCh = make(chan string, 1)
	m.ag.SteerCh = m.steerCh
	go func() {
		defer cancel()
		_, err := m.ag.Run(ctx, text, func(chunk string) {
			// events channel owns UI updates; stream via busy-safe channel below
			chMu.Lock()
			streamCh <- streamMsg(chunk)
			chMu.Unlock()
		})
		chMu.Lock()
		if err != nil {
			streamCh <- eventMsg(agent.Event{Type: "error", Text: err.Error()})
		}
		streamCh <- assistantDoneMsg{}
		chMu.Unlock()
	}()
}

var (
	chMu     sync.Mutex
	streamCh = make(chan tea.Msg, 256)
	mdRender *glamour.TermRenderer
)

func mdRenderer() *glamour.TermRenderer {
	if mdRender == nil {
		r, _ := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(0))
		mdRender = r
	}
	return mdRender
}

func waitForActivity() tea.Cmd {
	return func() tea.Msg { return <-streamCh }
}

func waitForClarify() tea.Cmd {
	return func() tea.Msg {
		req := <-tools.ClarifyChan
		return clarifyMsg{req: req}
	}
}

// ---------- bubbletea update ----------

func (m model) Init() tea.Cmd {
	return tea.Batch(tea.SetWindowTitle("◆ hiroto"), textarea.Blink, waitForActivity(), waitForClarify())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.vp.Width = msg.Width
		// Clamp: a height < 8 (tiny terminal, or size reported before PTY
		// init) would make vp.Height negative and panic in GotoBottom.
		h := msg.Height - 7
		if h < 1 {
			h = 1
		}
		m.vp.Height = h
		if msg.Width >= 4 {
			m.input.SetWidth(msg.Width - 4)
		} else {
			m.input.SetWidth(msg.Width)
		}
		m.refresh()

	case tea.KeyMsg:
		// Command picker intercepts keys first.
		if m.cmdPicker.active {
			switch msg.String() {
			case "up", "k":
				if m.cmdPicker.cursor > 0 {
					m.cmdPicker.cursor--
				}
				return m, nil
			case "down", "j":
				if m.cmdPicker.cursor < len(m.cmdPicker.items)-1 {
					m.cmdPicker.cursor++
				}
				return m, nil
			case "enter":
				if m.cmdPicker.cursor >= 0 && m.cmdPicker.cursor < len(m.cmdPicker.items) {
					m.input.SetValue(m.cmdPicker.items[m.cmdPicker.cursor] + " ")
				}
				m.cmdPicker.active = false
				return m, nil
			case "esc":
				m.cmdPicker.active = false
				return m, nil
			default:
				// Any other key closes the picker and passes through.
				m.cmdPicker.active = false
			}
		}

		// picker overlay consumes keys first
		if m.picker != nil {
			if m.updatePicker(msg) {
				return m, nil
			}
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			if m.busy && m.cancel != nil {
				m.cancel()
				m.busy = false
				m.streaming = false
				m.lines = append(m.lines, line{lineInfo, stMuted.Render("(dibatalkan)")})
				m.refresh()
				return m, nil
			}
			// Persist before quitting so --resume captures the last turn.
			m.saveSession()
			collectExitSummary(&m)
			return m, tea.Quit
		case tea.KeyCtrlP:
			m.openModelPicker()
			m.refresh()
			return m, nil
		case tea.KeyCtrlR:
			m.openResumePicker()
			m.refresh()
			return m, nil
		case tea.KeyEnter:
			if msg.Alt {
				m.input.InsertString("\n")
				return m, nil
			}
			// Clarify answer mode: send answer back to agent.
			if m.clarifyResp != nil {
				answer := strings.TrimSpace(m.input.Value())
				if answer == "" {
					answer = "(tidak menjawab)"
				}
				m.clarifyResp <- answer
				m.clarifyResp = nil
				m.clarifyQuestion = ""
				m.input.Placeholder = "ketik pesan untuk hiroto…"
				m.input.SetValue("")
				m.input.Focus()
				m.refresh()
				return m, nil
			}
			text := strings.TrimSpace(m.input.Value())
			if text == "" || m.busy {
				return m, nil
			}
			m.clearFlash()
			return m.handleSubmit(text)
		case tea.KeyUp:
			if len(m.history) > 0 && m.histIdx < len(m.history)-1 {
				m.histIdx++
				m.input.SetValue(m.history[len(m.history)-1-m.histIdx])
				return m, nil
			}
		case tea.KeyDown:
			if m.histIdx > 0 {
				m.histIdx--
				v := m.history[len(m.history)-1-m.histIdx]
				m.input.SetValue(v)
				return m, nil
			} else if m.histIdx == 0 {
				m.histIdx = -1
				m.input.SetValue("")
				return m, nil
			}
		case tea.KeyPgUp:
			m.vp.LineUp(m.vp.Height / 2)
			m.refresh()
			return m, nil
		case tea.KeyPgDown:
			m.vp.LineDown(m.vp.Height / 2)
			m.refresh()
			return m, nil
		case tea.KeyHome:
			m.vp.GotoTop()
			m.refresh()
			return m, nil
		case tea.KeyEnd:
			m.vp.GotoBottom()
			m.refresh()
			return m, nil
		case tea.KeyCtrlL:
			// Clear screen (Hiroto-style): reset lines + banner.
			m.lines = nil
			m.banner()
			m.refresh()
			return m, nil
		case tea.KeyCtrlS:
			m.saveSession()
			m.lines = append(m.lines, line{lineInfo, stMuted.Render("sesi disimpan")})
			m.refresh()
			return m, nil
		}

	case streamMsg:
		m.streaming = true
		m.appendStream(string(msg))
		m.refresh()
		cmds = append(cmds, waitForActivity())

	case eventMsg:
		m.streaming = false
		if msg.Type == "tool_start" {
			m.lines = append(m.lines, m.renderAgentEvent(msg))
		} else if msg.Type == "tool_end" {
			m.lines = append(m.lines, m.renderAgentEvent(msg))
		} else if msg.Type == "error" {
			m.lines = append(m.lines, m.renderAgentEvent(msg))
		}
		m.refresh()
		cmds = append(cmds, waitForActivity())

	case assistantDoneMsg:
		m.busy = false
		m.streaming = false
		m.endAssistantLine()
		m.saveSession()
		m.refresh()
		cmds = append(cmds, waitForActivity())

	case clarifyMsg:
		// Agent wants to ask the user a question.
		m.clarifyQuestion = msg.req.Question
		m.clarifyResp = msg.req.Response
		m.input.Placeholder = "jawab…"
		m.input.SetValue("")
		m.input.Focus()
		m.refresh()
	}

	var c1, c2 tea.Cmd
	m.input, c1 = m.input.Update(msg)
	m.spinner, c2 = m.spinner.Update(msg)
	// Viewport no longer handles keys — we do it explicitly via PgUp/Dn/Home/End.
	cmds = append(cmds, c1, c2)
	return m, tea.Batch(cmds...)
}

func (m *model) handleSubmit(text string) (tea.Model, tea.Cmd) {
	// Expand @-syntax context references before processing.
	text = expandContextRefs(text, m.ag.Workdir)

	// slash commands (Hiroto-style)
	if strings.HasPrefix(text, "/") {
		switch fields := strings.Fields(text); fields[0] {
		case "/help":
			m.lines = append(m.lines,
				line{lineInfo, stMuted.Render("/help /new /resume /compress /update /upgrade /skills /model /memory /memory add <teks> /memory del <id> /retry /undo /diff /stop /steer <pesan> /verbose /usage /rollback [save|restore <hash>] /prompt /bg /goal /branch /copy /title /reload /image /config /reasoning /todo /quit")},
			)
		case "/quit", "/exit":
			m.saveSession()
			collectExitSummary(m)
			return m, tea.Quit
		case "/new":
			m.ag.Messages = nil
			m.sessID = newSessionID()
			m.lines = append(m.lines, line{lineInfo, stMuted.Render("— sesi baru —")})
		case "/resume":
			m.openResumePicker()
		case "/compress":
			if err := m.ag.CompressNow(context.Background()); err != nil {
				m.lines = append(m.lines, line{lineError, stErr.Render("kompresi: " + err.Error())})
			}
		case "/update":
			latest, ok := checkUpdate()
			if ok {
				m.lines = append(m.lines, line{lineInfo, stBanner.Render("◆ update tersedia: v" + latest + " — ketik /upgrade untuk install")})
			} else {
				m.lines = append(m.lines, line{lineInfo, stMuted.Render("hiroto v" + version + " — versi terbaru")})
			}
		case "/upgrade":
			m.lines = append(m.lines, line{lineInfo, stMuted.Render("mengupdate hiroto…")})
			m.refresh()
			go func() {
				out, err := doUpdate()
				if err != nil {
					streamCh <- eventMsg(agent.Event{Type: "error", Text: err.Error()})
				} else {
					streamCh <- eventMsg(agent.Event{Type: "error", Text: "◆ hiroto updated! restart untuk versi baru."})
				}
				_ = out
			}()
			return m, nil
		case "/skills":
			if len(fields) >= 2 {
				m.openSkillsPicker()
				if m.picker != nil {
					m.picker.filter = strings.Join(fields[1:], " ")
					m.picker.applyFilter()
				}
			} else {
				m.openSkillsPicker()
			}
		case "/model":
			if len(fields) >= 2 {
				m.cfg.Model.Name = fields[1]
				m.ag.Client.Model = fields[1]
				m.lines = append(m.lines, line{lineInfo, stMuted.Render("model sesi ini: " + fields[1] + " (permanen: edit model: di ~/.hiroto/config.yaml)")})
			} else {
				m.openModelPicker()
			}
		case "/memory":
			if len(fields) >= 2 && fields[1] == "add" {
				content := strings.TrimSpace(strings.TrimPrefix(text, "/memory add"))
				if content != "" {
					id := m.mem.Add("memory", content)
					m.lines = append(m.lines, line{lineInfo, stMuted.Render("memory tersimpan: " + id)})
				}
			} else if len(fields) >= 3 && fields[1] == "del" {
				ok := m.mem.Remove("memory", fields[2])
				m.lines = append(m.lines, line{lineInfo, stMuted.Render(fmt.Sprintf("hapus: %v", ok))})
			} else {
				block := m.mem.PromptBlock()
				if block == "" {
					block = "(kosong)"
				}
				m.lines = append(m.lines, line{lineInfo, stMuted.Render(block)})
			}
		case "/todo":
			m.lines = append(m.lines, line{lineInfo, stMuted.Render("todo dibaca via tool oleh agent")})
		case "/retry":
			m.handleRetry()
		case "/undo":
			m.handleUndo()
		case "/diff":
			out, _ := exec.Command("git", "-C", m.ag.Workdir, "diff", "--stat").CombinedOutput()
			text := strings.TrimSpace(string(out))
			if text == "" {
				text = "(tidak ada perubahan)"
			}
			m.lines = append(m.lines, line{lineInfo, stMuted.Render(text)})
		case "/stop":
			n := tools.KillAll()
			m.lines = append(m.lines, line{lineInfo, stMuted.Render(fmt.Sprintf("mematikan %d proses background", n))})
		case "/steer":
			if len(fields) < 2 {
				m.flashMsg("pakai: /steer <pesan>", "error")
			} else if m.busy && m.steerCh != nil {
				msg := strings.Join(fields[1:], " ")
				select {
				case m.steerCh <- msg:
					m.lines = append(m.lines, line{lineInfo, stMuted.Render("steer: " + msg)})
				default:
					m.flashMsg("steer: channel penuh, coba lagi", "error")
				}
			} else {
				m.flashMsg("agent sedang tidak sibuk", "info")
			}
		case "/verbose":
			m.verbose = (m.verbose + 1) % 3
			labels := []string{"compact", "full", "log"}
			m.lines = append(m.lines, line{lineInfo, stMuted.Render("verbose: " + labels[m.verbose])})
		case "/usage":
			tokens := estimateTokens(m.ag.Messages)
			m.lines = append(m.lines, line{lineInfo, stMuted.Render(fmt.Sprintf("~%d token · %d pesan · %d turn", tokens, len(m.ag.Messages), countUserTurns(m.ag.Messages)))})
		case "/rollback":
			m.handleRollback(fields)
		case "/prompt":
			// Open $EDITOR for composing a long prompt.
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vim"
			}
			tmp, _ := os.CreateTemp("", "hiroto-prompt-*.md")
			tmp.Close()
			cmd := exec.Command(editor, tmp.Name())
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				m.flashMsg("editor: " + err.Error(), "error")
			} else {
				data, _ := os.ReadFile(tmp.Name())
				text := strings.TrimSpace(string(data))
				os.Remove(tmp.Name())
				if text != "" {
					return m.handleSubmit(text)
				}
			}
		case "/bg":
			if len(fields) < 2 {
				m.flashMsg("pakai: /bg <prompt>", "error")
			} else {
				bgText := strings.Join(fields[1:], " ")
				m.lines = append(m.lines, line{lineInfo, stMuted.Render("background: " + truncateStr(bgText, 80))})
				m.refresh()
				go func() {
					ag2 := m.ag // shallow copy — same client, separate messages
					messages := make([]llm.Message, len(ag2.Messages))
					copy(messages, ag2.Messages)
					ag2.Messages = messages
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
					defer cancel()
					_, err := ag2.Run(ctx, bgText, nil)
					if err != nil {
						streamCh <- eventMsg(agent.Event{Type: "error", Text: "bg: " + err.Error()})
					} else {
						streamCh <- eventMsg(agent.Event{Type: "error", Text: "✓ bg: selesai — " + truncateStr(bgText, 60)})
					}
				}()
			}
		case "/goal":
			if len(fields) < 2 {
				m.lines = append(m.lines, line{lineInfo, stMuted.Render("goal: " + m.goal)})
			} else {
				m.goal = strings.Join(fields[1:], " ")
				m.ag.Goal = m.goal
				m.lines = append(m.lines, line{lineInfo, stBanner.Render("◆ goal: " + m.goal)})
			}
		case "/branch":
			m.saveSession()
			oldID := m.sessID
			m.sessID = newSessionID()
			m.lines = append(m.lines, line{lineInfo, stMuted.Render(fmt.Sprintf("branch: %s → %s", oldID, m.sessID))})
		case "/copy":
			// Copy last assistant response to clipboard.
			last := ""
			for i := len(m.lines) - 1; i >= 0; i-- {
				if m.lines[i].kind == lineAssistant {
					last = m.lines[i].text
					break
				}
			}
			if last == "" {
				m.flashMsg("tidak ada response untuk disalin", "error")
			} else {
				copyToClipboard(last)
				m.flashMsg("disalin ke clipboard", "info")
			}
		case "/title":
			if len(fields) < 2 {
				m.flashMsg("pakai: /title <judul>", "error")
			} else {
				m.sessID = strings.Join(fields[1:], " ")
				m.saveSession()
				m.lines = append(m.lines, line{lineInfo, stMuted.Render("sesi: " + m.sessID)})
			}
		case "/reload":
			// Re-scan skills directory.
			m.ag.Skills = skills.Discover(m.cfg.Skills.Dirs)
			m.lines = append(m.lines, line{lineInfo, stMuted.Render(fmt.Sprintf("reload: %d skill", len(m.ag.Skills)))})
		case "/image":
			if len(fields) < 2 {
				m.flashMsg("pakai: /image <path>", "error")
			} else {
				imgPath := strings.Join(fields[1:], " ")
				data, err := os.ReadFile(imgPath)
				if err != nil {
					m.flashMsg("gagal baca: " + err.Error(), "error")
				} else {
					// Add as user message with image content (base64 data URL).
					b64 := encodeBase64(data)
					ext := strings.ToLower(filepath.Ext(imgPath))
					mime := "image/png"
					switch ext {
					case ".jpg", ".jpeg":
						mime = "image/jpeg"
					case ".gif":
						mime = "image/gif"
					case ".webp":
						mime = "image/webp"
					}
					dataURL := "data:" + mime + ";base64," + b64
					m.lines = append(m.lines, line{lineUser, "[image: " + imgPath + "]"})
					m.lines = append(m.lines, line{lineAssistant, ""})
					m.input.Reset()
					m.busy = true
					m.streaming = false
					m.refresh()
					// Send as multimodal message.
					m.ag.Messages = append(m.ag.Messages, llm.Message{
						Role: llm.RoleUser,
						Content: []llm.ContentPart{
							{Type: "image_url", ImageURL: map[string]string{"url": dataURL}},
						},
					})
					m.runTurnSilent("Describe this image.")
				}
			}
		case "/config":
			cfg := m.cfg
			var b strings.Builder
			b.WriteString(fmt.Sprintf("model: %s\n", cfg.Model.Name))
			b.WriteString(fmt.Sprintf("base_url: %s\n", cfg.Model.BaseURL))
			b.WriteString(fmt.Sprintf("max_turns: %d\n", cfg.Agent.MaxTurns))
			b.WriteString(fmt.Sprintf("skills: %d\n", len(m.ag.Skills)))
			b.WriteString(fmt.Sprintf("gateway: %v\n", cfg.Gateway.TelegramToken != ""))
			m.lines = append(m.lines, line{lineInfo, stMuted.Render("◆ config\n" + b.String())})
		case "/reasoning":
			if len(fields) < 2 {
				m.lines = append(m.lines, line{lineInfo, stMuted.Render("reasoning: " + m.reasoning)})
			} else {
				m.reasoning = fields[1]
				m.ag.Reasoning = m.reasoning
				m.lines = append(m.lines, line{lineInfo, stMuted.Render("reasoning: " + m.reasoning)})
			}
		case "/review":
			// Review the current git diff and provide feedback.
			m.lines = append(m.lines, line{lineInfo, stMuted.Render("reviewing changes…")})
			m.lines = append(m.lines, line{lineAssistant, ""})
			m.input.Reset()
			m.busy = true
			m.streaming = false
			m.refresh()
			out, _ := exec.Command("git", "-C", m.ag.Workdir, "diff").CombinedOutput()
			diff := string(out)
			if diff == "" {
				diff = "(tidak ada perubahan — coba git diff --cached untuk staged changes)"
			}
			prompt := "Review the following git diff. Point out bugs, style issues, security concerns, and suggest improvements. Be concise.\n\n```diff\n" + diff + "\n```"
			m.runTurn(prompt)
		case "/explain":
			if len(fields) < 2 {
				// Explain the current codebase structure.
				m.lines = append(m.lines, line{lineInfo, stMuted.Render("explaining codebase…")})
				m.lines = append(m.lines, line{lineAssistant, ""})
				m.input.Reset()
				m.busy = true
				m.streaming = false
				m.refresh()
				m.runTurn("Explain the architecture and structure of this codebase. What are the main packages, their responsibilities, and how they connect?")
			} else {
				target := strings.Join(fields[1:], " ")
				m.lines = append(m.lines, line{lineUser, "/explain " + target})
				m.lines = append(m.lines, line{lineAssistant, ""})
				m.input.Reset()
				m.busy = true
				m.streaming = false
				m.refresh()
				m.runTurn("Explain this code: " + target + ". What does it do, how does it work, and what are the key patterns?")
			}
		case "/test":
			m.lines = append(m.lines, line{lineInfo, stMuted.Render("running tests…")})
			m.lines = append(m.lines, line{lineAssistant, ""})
			m.input.Reset()
			m.busy = true
			m.streaming = false
			m.refresh()
			prompt := "Run the test suite for this project. If tests fail, debug the failures, fix the code, and re-run until all tests pass. Report the final result."
			m.runTurn(prompt)
		default:
			m.flashMsg("perintah tidak dikenal: " + fields[0], "error")
		}
		m.input.Reset()
		m.refresh()
		return m, nil
	}

	m.history = append(m.history, text)
	m.histIdx = -1
	m.streaming = false
	m.lines = append(m.lines, line{lineUser, text})
	m.lines = append(m.lines, line{lineAssistant, ""})
	m.input.Reset()
	m.busy = true
	m.refresh()
	m.runTurn(text)
	return m, waitForActivity()
}

// streaming buffer: last line is the in-progress assistant message
const streamPrefixLen = 1

func (m *model) appendStream(chunk string) {
	if len(m.lines) == 0 || m.lines[len(m.lines)-1].kind != lineAssistant {
		m.lines = append(m.lines, line{lineAssistant, ""})
	}
	l := &m.lines[len(m.lines)-1]
	l.text += chunk
}

func (m *model) endAssistantLine() {
	if len(m.lines) > 0 && m.lines[len(m.lines)-1].kind == lineAssistant && m.lines[len(m.lines)-1].text == "" {
		m.lines = m.lines[:len(m.lines)-1]
	}
	m.lines = append(m.lines, line{lineInfo, ""})
}

func (m *model) refresh() {
	var b strings.Builder
	for _, ln := range m.lines {
		text := ln.text
		switch ln.kind {
		case lineUser:
			if text != "" {
				text = stUserTag.Render("you ❯") + " " + text
			}
		case lineAssistant:
			if strings.TrimSpace(text) == "" {
				continue // streaming placeholder — nothing to show yet
			}
			// Render markdown on the RAW body only: never feed styled text
			// (embedded ANSI) through glamour — its writer can split escape
			// sequences and leak fragments like [1;38;2;…m as literal text.
			if rendered, err := mdRenderer().Render(text); err == nil {
				text = rendered
			}
			text = stAsstTag.Render("hiroto ◆ ") + text
		}
		b.WriteString(text + "\n")
	}
	content := b.String()
	if m.busy {
		label := "berpikir…"
		if m.streaming {
			label = "menulis…"
		}
		content += stMuted.Render(m.spinner.View() + " " + label)
	}
	atBottom := m.vp.AtBottom()
	m.vp.SetContent(content)
	if atBottom {
		m.vp.GotoBottom()
	}
}

func (m model) View() string {
	if m.width == 0 {
		return "memuat…"
	}
	status := stMuted.Render("siap")
	if m.busy {
		label := "bekerja…"
		if m.streaming {
			label = "menulis…"
		}
		status = stToolTag.Render(m.spinner.View() + " " + label)
	}
	left := stChip.Render(" HR ") + stMuted.Render(" hiroto v"+version)
	right := status
	if len(m.ag.Messages) > 0 {
		tok := 0
		for _, msg := range m.ag.Messages {
			if s, ok := msg.Content.(string); ok {
				tok += len(s) / 3
			}
		}
		right = stMuted.Render(fmt.Sprintf("~%dK", tok/1000)) + "  " + status
	}
	pad := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if pad < 1 {
		pad = 1
	}
	statusBar := left + strings.Repeat(" ", pad) + right
	clarifyBar := ""
	if m.clarifyQuestion != "" {
		clarifyBar = stBanner.Render("◆ " + m.clarifyQuestion)
	}
	// Flash card (floating error/info popup).
	flashBar := ""
	if m.flash != "" {
		if m.flashKind == "error" {
			flashBar = stFlashErr.Render(m.flash)
		} else {
			flashBar = stFlashInfo.Render(m.flash)
		}
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		m.vp.View(),
		clarifyBar,
		flashBar,
		stInput.Render(m.input.View()),
		statusBar,
		stHelp.Render("Enter kirim • Ctrl+P model • Ctrl+R sesi • Ctrl+S simpan • Ctrl+L bersih • ↑↓ history • PgUp/Dn scroll • Ctrl+C keluar"),
	)
	if m.picker != nil {
		box := m.renderPicker()
		// center the modal over a dimmed backdrop (Hermes-style overlay)
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			box,
			lipgloss.WithWhitespaceBackground(cBackdrop),
		)
	}
	// Auto-suggest: show available slash commands when typing /
	if strings.HasPrefix(m.input.Value(), "/") && !strings.Contains(m.input.Value(), " ") {
		matches := matchingCommands(m.input.Value())
		if !m.cmdPicker.active {
			m.cmdPicker.active = true
			m.cmdPicker.items = matches
			m.cmdPicker.cursor = 0
		} else {
			m.cmdPicker.items = matches
			if m.cmdPicker.cursor >= len(matches) {
				m.cmdPicker.cursor = 0
			}
		}
		if len(matches) > 0 {
			suggest := renderCmdPicker(m.cmdPicker.items, m.cmdPicker.cursor)
			if suggest != "" {
				body = lipgloss.JoinVertical(lipgloss.Left, body, suggest)
			}
		}
	} else {
		m.cmdPicker.active = false
	}
	return body
}

// ---------- entry ----------

func main() {
	cfg := config.Load()
	mem := memory.New()

	// --banner: print the startup banner and exit (optional width arg)
	if len(os.Args) >= 2 && os.Args[1] == "--banner" {
		w := 80
		if len(os.Args) >= 3 {
			fmt.Sscanf(os.Args[2], "%d", &w)
		}
		skillList := skills.Discover(cfg.Skills.Dirs)
		for _, ln := range bannerLines(w, cfg.Model.Name, len(skillList)) {
			fmt.Println(ln)
		}
		return
	}

	// --models: interactive model picker; choice persists to config.yaml
	if len(os.Args) >= 2 && os.Args[1] == "--models" {
		runModelsCmd(cfg)
		return
	}

	// hiroto tool <name>  — execute a single tool (reads JSON args from stdin, used by execute_code)
	if len(os.Args) >= 3 && os.Args[1] == "tool" {
		runToolCmd(os.Args[2])
		return
	}

	// hiroto gateway  — start Telegram bot (and future WhatsApp)
	if len(os.Args) >= 2 && os.Args[1] == "gateway" {
		runGateway(cfg, mem)
		return
	}

	// hiroto --api  — start OpenAI-compatible API server
	if len(os.Args) >= 2 && os.Args[1] == "--api" {
		runAPI(cfg, mem)
		return
	}

	// hiroto --update  — check for and install updates
	if len(os.Args) >= 2 && os.Args[1] == "--update" {
		latest, ok := checkUpdate()
		if ok {
			fmt.Printf("◆ update tersedia: v%s → v%s\n", version, latest)
			fmt.Print("install sekarang? [Y/n] ")
			var answer string
			fmt.Scanln(&answer)
			if answer == "" || answer == "y" || answer == "Y" {
				out, err := doUpdate()
				if err != nil {
					fmt.Fprintln(os.Stderr, "hiroto:", err)
					os.Exit(1)
				}
				fmt.Println(out)
				fmt.Println("◆ hiroto updated! restart untuk versi baru.")
			}
		} else {
			fmt.Printf("hiroto v%s — versi terbaru\n", version)
		}
		return
	}

	// --set-model <name>: non-interactive model switch
	if len(os.Args) >= 3 && os.Args[1] == "--set-model" {
		config.SaveModel(os.Args[2])
		fmt.Println("model tersimpan:", os.Args[2])
		return
	}

	// one-shot mode: hiroto -q "question" (like `hiroto -q`)
	if len(os.Args) >= 3 && os.Args[1] == "-q" {
		runOneShot(cfg, mem, strings.Join(os.Args[2:], " "))
		return
	}

	// continue last session, one-shot: hiroto -c "prompt"
	if len(os.Args) >= 2 && os.Args[1] == "-c" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, `pakai: hiroto -c "prompt" (lanjut sesi terakhir)`)
			os.Exit(1)
		}
		runOneShotContinue(cfg, mem, strings.Join(os.Args[2:], " "))
		return
	}

	// resume a saved session in the TUI: hiroto --resume <id>
	if len(os.Args) >= 2 && os.Args[1] == "--resume" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "pakai: hiroto --resume <session-id>")
			os.Exit(1)
		}
		if err := runResumeTUI(cfg, mem, os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "hiroto:", err)
			os.Exit(1)
		}
		return
	}

	// wire agent: skills + registry + memory + web backends
	ag := buildAgent(cfg, mem)
	ss := session.New()

	// Enable clarify channel for interactive user questions.
	tools.ClarifyChan = make(chan tools.ClarifyRequest, 1)

	setWindowTitle()
	p := tea.NewProgram(initialModel(cfg, ag, mem, ss), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "hiroto:", err)
		os.Exit(1)
	}
	// Altscreen closed — print the resume recap on the normal terminal.
	printExitSummary()
}

func workdir() string {
	wd, _ := os.Getwd()
	return wd
}

// buildAgent assembles the agent with all tools wired (shared by TUI and one-shot).
func buildAgent(cfg *config.Config, mem *memory.Store) *agent.Agent {
	skillList := skills.Discover(cfg.Skills.Dirs)
	ag := &agent.Agent{
		Client:            llm.New(cfg.Model.BaseURL, cfg.APIKey(), cfg.Model.Name),
		Skills:            skillList,
		Memory:            mem,
		MaxTurns:          cfg.Agent.MaxTurns,
		CompressBudget:    cfg.Agent.CompressBudget,
		CompressKeepTurns: cfg.Agent.CompressKeepTurns,
		RetryAttempts:     2,
		Workdir:           workdir(),
	}
	reg := tools.NewRegistry()
	tools.RegisterBuiltin(reg, tools.Options{
		TermTimeout: time.Duration(cfg.Agent.TermTimeout) * time.Second,
		Workdir:     workdir(),
		WebSearch: func(query string, limit int) ([]tools.SearchHit, error) {
			hits, err := web.Search(context.Background(), query, limit)
			out := make([]tools.SearchHit, 0, len(hits))
			for _, h := range hits {
				out = append(out, tools.SearchHit(h))
			}
			return out, err
		},
		WebExtract: func(urls []string) ([]tools.PageResult, error) {
			pages, err := web.Extract(context.Background(), urls)
			out := make([]tools.PageResult, 0, len(pages))
			for _, p := range pages {
				out = append(out, tools.PageResult(p))
			}
			return out, err
		},
		Memory: mem,
		Todo:   tools.NewTodoStore(),
		Skills: newSkillIdx(skillList),
		SessionSearch: func(q string) []session.Session {
			return session.New().Search(q)
		},
		LLMClient: &llmAdapter{client: llm.New(cfg.Model.BaseURL, cfg.APIKey(), cfg.Model.Name)},
	})
	ag.Reg = reg

	// Load plugins and MCP servers.
	pluginDirs := cfg.Plugins.Dirs
	if len(pluginDirs) == 0 {
		pluginDirs = []string{filepath.Join(config.HomeDir(), "plugins")}
	}
	for _, pt := range plugin.LoadPlugins(pluginDirs) {
		reg.Register(&tools.Tool{
			Name: pt.Name, Description: pt.Description, Parameters: pt.Parameters,
			Exec: func(ctx context.Context, args map[string]any) tools.Result {
				out, isErr := pt.Exec(ctx, args)
				return tools.Result{Output: out, IsError: isErr}
			},
		})
	}
	for _, srv := range cfg.MCP.Servers {
		mcp, err := plugin.ConnectMCP(context.Background(), plugin.MCPServerConfig{
			Command: srv.Command, Args: srv.Args,
		})
		if err != nil {
			continue // non-fatal: a failed MCP server must not pollute the TUI
		}
		for _, mt := range mcp.Tools() {
			reg.Register(&tools.Tool{
				Name: mt.Name, Description: mt.Description, Parameters: mt.Parameters,
				Exec: func(ctx context.Context, args map[string]any) tools.Result {
					out, isErr := mt.Exec(ctx, args)
					return tools.Result{Output: out, IsError: isErr}
				},
			})
		}
	}
	return ag
}

// runOneShot executes a single query headlessly (no TUI), streaming to stdout.
func runOneShot(cfg *config.Config, mem *memory.Store, query string) {
	ag := buildAgent(cfg, mem)
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
}

// newSkillIdx adapts []skills.Skill to tools.SkillIndex (mirrors agent.skillAdapter).
type skillIdx struct{ byName map[string]string }

func newSkillIdx(all []skills.Skill) skillIdx {
	m := map[string]string{}
	for _, s := range all {
		m[s.Name] = s.Path
	}
	return skillIdx{byName: m}
}

func (s skillIdx) Find(name string) (string, bool) { p, ok := s.byName[name]; return p, ok }
func (s skillIdx) Names() []string {
	out := make([]string, 0, len(s.byName))
	for n := range s.byName {
		out = append(out, n)
	}
	return out
}

// runToolCmd executes a single tool (used by execute_code via `hiroto tool <name>`).
// Reads JSON args from stdin, prints JSON result {output, isError} to stdout.
func runToolCmd(name string) {
	cfg := config.Load()
	mem := memory.New()
	ag := buildAgent(cfg, mem)
	t, ok := ag.Reg.Get(name)
	if !ok {
		fmt.Fprintf(os.Stderr, `{"output":"unknown tool: %s","isError":true}`, name)
		os.Exit(1)
	}
	data, _ := io.ReadAll(os.Stdin)
	args := tools.JSONArgs(string(data))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result := t.Exec(ctx, args)
	out, _ := json.Marshal(result)
	fmt.Println(string(out))
}

// runGateway starts the Telegram bot (and future WhatsApp) if configured.
func runGateway(cfg *config.Config, mem *memory.Store) {
	token := cfg.GatewayToken()
	if token == "" {
		// Setup wizard: prompt for the token, then persist it to ~/.hiroto/.env.
		fmt.Print("◆ Setup gateway — Token bot Telegram (dari @BotFather): ")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			fmt.Fprintln(os.Stderr, "hiroto: input dibatalkan")
			os.Exit(1)
		}
		token = strings.TrimSpace(scanner.Text())
		if token == "" {
			fmt.Fprintln(os.Stderr, "hiroto: token tidak boleh kosong")
			os.Exit(1)
		}
		if err := config.SaveGatewayToken(token); err != nil {
			fmt.Fprintln(os.Stderr, "hiroto: gagal simpan token:", err)
			os.Exit(1)
		}
		fmt.Println("✓ token tersimpan di ~/.hiroto/.env (HIROTO_TELEGRAM_TOKEN)")
	}
	ag := buildAgent(cfg, mem)
	fmt.Println("◆ Hiroto gateway — Telegram bot berjalan…")
	log.Fatal(gateway.Telegram(token, ag))
}

// runAPI starts the OpenAI-compatible API server.
func runAPI(cfg *config.Config, mem *memory.Store) {
	port := cfg.API.Port
	if port == 0 {
		port = 20129
	}
	ag := buildAgent(cfg, mem)
	api.PrintBanner(port, ag.Client.Model)
	srv := api.New(ag, port)
	log.Fatal(srv.Start())
}

// llmAdapter bridges tools.LLMClient to the internal LLM client.
type llmAdapter struct {
	client *llm.Client
}

func (a *llmAdapter) Chat(ctx context.Context, messages []tools.LLMMessage) (string, error) {
	msgs := make([]llm.Message, len(messages))
	for i, m := range messages {
		msgs[i] = llm.Message{Role: llm.Role(m.Role), Content: m.Content}
	}
	result, err := a.client.Chat(ctx, msgs, nil)
	if err != nil {
		return "", err
	}
	if s, ok := result.Content.(string); ok {
		return s, nil
	}
	return "", fmt.Errorf("no content in response")
}

// matchingCommands returns slash commands that match the prefix.
func matchingCommands(prefix string) []string {
	all := []string{
		"/help", "/new", "/resume", "/compress", "/skills",
		"/model", "/memory add", "/memory del", "/todo", "/quit",
		"/retry", "/undo", "/diff", "/stop", "/steer",
		"/verbose", "/usage", "/rollback",
		"/prompt", "/bg", "/goal", "/branch", "/copy",
		"/title", "/reload", "/image", "/config", "/reasoning",
		"/review", "/explain", "/test",
	}
	var matches []string
	for _, c := range all {
		if strings.HasPrefix(c, prefix) {
			matches = append(matches, c)
		}
	}
	return matches
}

// renderCmdPicker draws the scrollable command picker card.
func renderCmdPicker(items []string, cursor int) string {
	if len(items) == 0 {
		return ""
	}
	const win = 10
	var b strings.Builder
	start, end := 0, len(items)
	if end > win {
		start = cursor - win/2
		if start < 0 {
			start = 0
		}
		if start+win > end {
			start = end - win
		}
		end = start + win
	}
	for i := start; i < end; i++ {
		if i > start {
			b.WriteString("\n")
		}
		if i == cursor {
			b.WriteString(pkCur.Render("❯ " + items[i]))
		} else {
			b.WriteString(pkItem.Render("  " + items[i]))
		}
	}
	if len(items) > win {
		b.WriteString("\n" + stMuted.Render(fmt.Sprintf("  %d/%d  ↑↓ pilih  Enter ok  esc batal", cursor+1, len(items))))
	} else {
		b.WriteString("\n" + stMuted.Render("↑↓ pilih  Enter ok  esc batal"))
	}
	return stFlashInfo.Render(b.String())
}

// ---- retry / undo (TUI) ----

func (m *model) handleRetry() {
	text, ok := popLastUserMsg(m.ag.Messages)
	if !ok {
		m.lines = append(m.lines, line{lineError, stErr.Render("tidak ada giliran user untuk diulang")})
		m.refresh()
		return
	}
	// Remove last user turn + everything after.
	m.removeLastUserTurn()
	// Remove the corresponding display lines.
	m.stripLastUserTurnLines()
	m.lines = append(m.lines, line{lineInfo, stMuted.Render("retry: " + truncateStr(text, 80))})
	m.lines = append(m.lines, line{lineAssistant, ""})
	m.input.Reset()
	m.busy = true
	m.streaming = false
	m.refresh()
	m.runTurn(text)
}

func (m *model) handleUndo() {
	text, ok := popLastUserMsg(m.ag.Messages)
	if !ok {
		m.lines = append(m.lines, line{lineError, stErr.Render("tidak ada giliran user untuk dibatalkan")})
		m.refresh()
		return
	}
	m.removeLastUserTurn()
	m.stripLastUserTurnLines()
	m.lines = append(m.lines, line{lineInfo, stMuted.Render("undo: " + truncateStr(text, 80))})
	m.refresh()
}

func (m *model) removeLastUserTurn() {
	msgs := m.ag.Messages
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleUser {
			m.ag.Messages = msgs[:i]
			return
		}
	}
}

func (m *model) stripLastUserTurnLines() {
	// Remove the last user line + everything after it in the display.
	idx := -1
	for i := len(m.lines) - 1; i >= 0; i-- {
		if m.lines[i].kind == lineUser {
			idx = i
			break
		}
	}
	if idx >= 0 {
		m.lines = m.lines[:idx]
	}
}

func popLastUserMsg(msgs []llm.Message) (string, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleUser {
			if s, ok := msgs[i].Content.(string); ok {
				return s, true
			}
			return "", true
		}
	}
	return "", false
}

// ---- rollback ----

func (m *model) handleRollback(fields []string) {
	// List checkpoints if no arg.
	if len(fields) < 2 {
		out, _ := exec.Command("git", "-C", m.ag.Workdir, "log", "--oneline", "-20", "--grep=hiroto checkpoint", "HEAD").CombinedOutput()
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = "(belum ada checkpoint — ketik /rollback save untuk buat)"
		}
		m.lines = append(m.lines, line{lineInfo, stMuted.Render("◆ rollback checkpoints\n" + text)})
		m.refresh()
		return
	}
	sub := fields[1]
	switch sub {
	case "save":
		// Create a checkpoint commit.
		exec.Command("git", "-C", m.ag.Workdir, "add", "-A").Run()
		ts := time.Now().Format("15:04:05")
		out, err := exec.Command("git", "-C", m.ag.Workdir, "commit", "--allow-empty", "-m", "hiroto checkpoint "+ts).CombinedOutput()
		exit := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				exit = ee.ExitCode()
			}
		}
		// Exit code 1 = nothing to commit (not an error).
		if exit == 1 {
			m.lines = append(m.lines, line{lineInfo, stMuted.Render("tidak ada perubahan untuk di-checkpoint")})
		} else {
			m.lines = append(m.lines, line{lineInfo, stMuted.Render("checkpoint: " + strings.TrimSpace(string(out)))})
		}
	case "restore":
		if len(fields) < 3 {
			m.flashMsg("pakai: /rollback restore <hash>", "error")
			return
		}
		hash := fields[2]
		out, err := exec.Command("git", "-C", m.ag.Workdir, "reset", "--hard", hash).CombinedOutput()
		if err != nil {
			m.flashMsg("rollback gagal: " + string(out), "error")
		} else {
			m.lines = append(m.lines, line{lineInfo, stMuted.Render("rollback ke " + hash + " — file dikembalikan")})
		}
	default:
		m.flashMsg("pakai: /rollback [save|restore <hash>]", "error")
	}
	m.refresh()
}

// ---- context expansion (@-syntax) ----

func expandContextRefs(text, workdir string) string {
	if !strings.Contains(text, "@") {
		return text
	}
	var result strings.Builder
	words := strings.Fields(text)
	for _, word := range words {
		if strings.HasPrefix(word, "@file:") {
			path := strings.TrimPrefix(word, "@file:")
			path = expandPath(path, workdir)
			data, err := os.ReadFile(path)
			if err == nil {
				result.WriteString("\n\n[file: " + path + "]\n```\n")
				content := string(data)
				if len(content) > 8000 {
					content = content[:8000] + "\n... (truncated)"
				}
				result.WriteString(content)
				result.WriteString("\n```\n")
			}
		} else if strings.HasPrefix(word, "@folder:") {
			path := strings.TrimPrefix(word, "@folder:")
			path = expandPath(path, workdir)
			entries, err := os.ReadDir(path)
			if err == nil {
				result.WriteString("\n\n[folder: " + path + "]\n")
				for i, e := range entries {
					if i >= 50 {
						result.WriteString("... dan lainnya\n")
						break
					}
					result.WriteString("  " + e.Name())
					if e.IsDir() {
						result.WriteString("/")
					}
					result.WriteString("\n")
				}
			}
		} else if word == "@diff" {
			out, _ := exec.Command("git", "-C", workdir, "diff", "--stat").CombinedOutput()
			if len(out) > 0 {
				result.WriteString("\n\n[git diff]\n```\n")
				result.Write(out)
				result.WriteString("```\n")
			}
		} else {
			if result.Len() > 0 {
				result.WriteString(" ")
			}
			result.WriteString(word)
		}
	}
	expanded := result.String()
	if expanded == "" {
		return text
	}
	return expanded
}

func expandPath(path, workdir string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workdir, path)
	}
	return path
}

// ---- token estimation ----

func estimateTokens(msgs []llm.Message) int {
	total := 0
	for _, m := range msgs {
		if s, ok := m.Content.(string); ok {
			// Rough: ~3 chars per token for English, ~2 for code.
			total += len(s) / 3
		}
		for _, tc := range m.ToolCalls {
			total += len(tc.Function.Name)/3 + len(tc.Function.Arguments)/3
		}
	}
	return total
}

func countUserTurns(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == llm.RoleUser {
			n++
		}
	}
	return n
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// flashMsg shows a temporary floating card above the input.
// It clears automatically on the next action.
func (m *model) flashMsg(msg, kind string) {
	m.flash = msg
	m.flashKind = kind
}

func (m *model) clearFlash() {
	m.flash = ""
	m.flashKind = ""
}

// ---- clipboard + base64 helpers ----

func copyToClipboard(text string) {
	// Try xclip first, then wl-copy, then pbcopy (macOS).
	for _, cmd := range [][]string{
		{"xclip", "-selection", "clipboard"},
		{"wl-copy"},
		{"pbcopy"},
	} {
		c := exec.Command(cmd[0], cmd[1:]...)
		c.Stdin = strings.NewReader(text)
		if err := c.Run(); err == nil {
			return
		}
	}
}

func encodeBase64(data []byte) string {
	// Simple base64 encoding without importing encoding/base64 in main.
	var b strings.Builder
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	for i := 0; i < len(data); i += 3 {
		var chunk [3]byte
		chunk[0] = data[i]
		if i+1 < len(data) {
			chunk[1] = data[i+1]
		}
		if i+2 < len(data) {
			chunk[2] = data[i+2]
		}
		b.WriteByte(alphabet[chunk[0]>>2])
		b.WriteByte(alphabet[((chunk[0]&3)<<4)|(chunk[1]>>4)])
		if i+1 < len(data) {
			b.WriteByte(alphabet[((chunk[1]&15)<<2)|(chunk[2]>>6)])
		} else {
			b.WriteByte('=')
		}
		if i+2 < len(data) {
			b.WriteByte(alphabet[chunk[2]&63])
		} else {
			b.WriteByte('=')
		}
	}
	return b.String()
}

// runTurnSilent runs the agent without adding a new user message to the display.
// Used by /image — the image content is already in Messages.
func (m *model) runTurnSilent(text string) {
	m.busy = true
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.steerCh = make(chan string, 1)
	m.ag.SteerCh = m.steerCh
	go func() {
		defer cancel()
		_, err := m.ag.Run(ctx, text, func(chunk string) {
			chMu.Lock()
			streamCh <- streamMsg(chunk)
			chMu.Unlock()
		})
		chMu.Lock()
		if err != nil {
			streamCh <- eventMsg(agent.Event{Type: "error", Text: err.Error()})
		}
		streamCh <- assistantDoneMsg{}
		chMu.Unlock()
	}()
}
