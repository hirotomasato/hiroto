package main

// Hiroto TUI — a modern Bubble Tea chat interface (Hiroto TUI-style):
// streaming assistant text, tool activity lines, spinners, session log.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hirotomasato/hiroto/internal/agent"
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
	cPrimary = lipgloss.Color("#E8A33D") // warm gold like Hiroto's default skin
	cMuted   = lipgloss.Color("#6E6A5E")
	cUser    = lipgloss.Color("#7FB4E8")
	cTool    = lipgloss.Color("#8FBF7F")
	cErr     = lipgloss.Color("#E06C60")
	cBgFaint = lipgloss.Color("#3A3630")

	stBanner  = lipgloss.NewStyle().Bold(true).Foreground(cPrimary)
	stUserTag = lipgloss.NewStyle().Bold(true).Foreground(cUser)
	stAsstTag = lipgloss.NewStyle().Bold(true).Foreground(cPrimary)
	stToolTag = lipgloss.NewStyle().Foreground(cTool)
	stMuted   = lipgloss.NewStyle().Foreground(cMuted)
	stErr     = lipgloss.NewStyle().Foreground(cErr)
	stInput   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBgFaint).Padding(0, 1)
	stHelp    = lipgloss.NewStyle().Foreground(cMuted)
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

	history  []string // input history
	histIdx  int
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
	m := model{cfg: cfg, ag: ag, mem: mem, sessStore: ss, sessID: newSessionID(), input: ta, spinner: sp, histIdx: -1}
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
	for _, ln := range bannerLines(w, m.cfg.Model.Name+" @ "+m.cfg.Model.BaseURL, len(m.ag.Skills)) {
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
		body := strings.Join(strings.Split(e.Text, "\n")[:minInt(3, len(strings.Split(e.Text, "\n")))], "\n")
		if len(body) > 300 {
			body = body[:300] + "…"
		}
		return line{lineTool, head + "\n" + stMuted.Render(indent(body, "    "))}
	case "compress_start", "compress_end":
		return line{lineInfo, stMuted.Render("⚡ "+e.Text)}
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
		m.vp.Height = msg.Height - 7
		m.input.SetWidth(msg.Width - 4)
		m.refresh()

	case tea.KeyMsg:
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
				m.lines = append(m.lines, line{lineInfo, stMuted.Render("(dibatalkan)")})
				m.refresh()
				return m, nil
			}
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
		m.appendStream(string(msg))
		m.refresh()
		cmds = append(cmds, waitForActivity())

	case eventMsg:
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
	// slash commands (Hiroto-style)
	if strings.HasPrefix(text, "/") {
		switch fields := strings.Fields(text); fields[0] {
		case "/help":
			m.lines = append(m.lines,
				line{lineInfo, stMuted.Render("/help /new /resume /compress /update /upgrade /skills /model /memory /memory add <teks> /memory del <id> /todo /quit")},
			)
		case "/quit", "/exit":
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
			var names []string
			for _, s := range m.ag.Skills {
				names = append(names, s.Name)
			}
			m.lines = append(m.lines, line{lineInfo, stMuted.Render("skills: " + strings.Join(names, ", "))})
		case "/model":
			if len(fields) >= 2 {
				m.cfg.Model.Name = fields[1]
				m.ag.Client.Model = fields[1]
				m.lines = append(m.lines, line{lineInfo, stMuted.Render("model sesi ini: " + fields[1] + " (permanen: edit model: di ~/.hiroto/config.yaml)")})
			} else {
				m.lines = append(m.lines, line{lineInfo, stMuted.Render("model aktif: " + m.ag.Client.Model + " · pakai: /model <nama>")})
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
		default:
			m.lines = append(m.lines, line{lineError, stErr.Render("perintah tidak dikenal: " + fields[0])})
		}
		m.input.Reset()
		m.refresh()
		return m, nil
	}

	m.history = append(m.history, text)
	m.histIdx = -1
	m.lines = append(m.lines, line{lineUser, stUserTag.Render("you ❯") + " " + text})
	m.lines = append(m.lines, line{lineAssistant, stAsstTag.Render("hiroto ◆ ")})
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
		// Render markdown for assistant messages
		if ln.kind == lineAssistant && text != "" {
			if rendered, err := mdRenderer().Render(text); err == nil {
				text = rendered
			}
		}
		b.WriteString(text + "\n")
	}
	content := b.String()
	if m.busy {
		content += stMuted.Render(m.spinner.View() + " berpikir…")
	}
	m.vp.SetContent(content)
	m.vp.GotoBottom()
}

func (m model) View() string {
	if m.width == 0 {
		return "memuat…"
	}
	status := stMuted.Render("siap")
	if m.busy {
		status = stToolTag.Render(m.spinner.View() + " bekerja…")
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
	body := lipgloss.JoinVertical(lipgloss.Left,
		m.vp.View(),
		clarifyBar,
		stInput.Render(m.input.View()),
		statusBar,
		stHelp.Render("Enter kirim • Ctrl+P model • Ctrl+R sesi • Ctrl+S simpan • Ctrl+L bersih • ↑↓ history • PgUp/Dn scroll • Ctrl+C keluar"),
	)
	if m.picker != nil {
		return m.renderPicker()
	}
	// Auto-suggest: show available slash commands when typing /
	if strings.HasPrefix(m.input.Value(), "/") && !strings.Contains(m.input.Value(), " ") {
		suggest := suggestCommands(m.input.Value())
		if suggest != "" {
			body = lipgloss.JoinVertical(lipgloss.Left, body, suggest)
		}
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
		for _, ln := range bannerLines(w, cfg.Model.Name+" @ "+cfg.Model.BaseURL, len(skillList)) {
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
			log.Printf("[mcp] %s: %v", srv.Command, err)
			continue
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
	if cfg.Gateway.TelegramToken == "" {
		fmt.Fprintln(os.Stderr, "hiroto: gateway.telegram_token tidak diatur di config.yaml")
		os.Exit(1)
	}
	ag := buildAgent(cfg, mem)
	fmt.Println("◆ Hiroto gateway — Telegram bot berjalan…")
	log.Fatal(gateway.Telegram(cfg.Gateway.TelegramToken, ag))
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

// suggestCommands returns a formatted list of slash commands matching the prefix.
func suggestCommands(prefix string) string {
	all := []string{
		"/help", "/new", "/resume", "/compress", "/skills",
		"/model", "/memory add", "/memory del", "/todo", "/quit",
	}
	var matches []string
	for _, c := range all {
		if strings.HasPrefix(c, prefix) {
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	return stMuted.Render(strings.Join(matches, "  "))
}
