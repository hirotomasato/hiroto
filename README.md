# Hiroto

> Personal AI agent — terminal-native, Go-powered, cybersecurity-ready.

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev)
[![Version](https://img.shields.io/github/v/release/hirotomasato/hiroto?color=blue)](https://github.com/hirotomasato/hiroto/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/hirotomasato/hiroto/ci.yml?branch=main)](https://github.com/hirotomasato/hiroto/actions)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Lines](https://img.shields.io/badge/lines-5.0k-orange)](https://github.com/hirotomasato/hiroto)
[![Skills](https://img.shields.io/badge/skills-234-purple)](https://github.com/hirotomasato/hiroto)

Hiroto is a personal AI agent that lives in your terminal. Built in Go with a modern TUI, it connects to any OpenAI-compatible LLM and gives you 32 built-in tools — from running shell commands to full browser automation, from Python/JS execution to cybersecurity reconnaissance.

---

## Features

- **Modern TUI** — Bubble Tea interface with gradient banner, status bar, markdown rendering, and auto-suggest
- **32 built-in tools** — terminal, file ops, web, browser CDP, code execution, security scanning, process management, and more
- **234 skills** — cybersecurity, devops, and general-purpose knowledge base
- **One-shot mode** — `hiroto -q "question"` for scripts and automation
- **Session persistence** — auto-save and resume conversations
- **Model picker** — live model list from your endpoint, switch on the fly
- **Context compression** — auto-summarize long conversations
- **Plugin system** — load external tools from `~/.hiroto/plugins/`
- **MCP support** — connect to Model Context Protocol servers
- **Telegram gateway** — run as a Telegram bot
- **13 external security tools** — httpx, nuclei, subfinder, katana, sqlmap, and more

---

## Quick Start

### Prerequisites

- **Go 1.23+**
- **Node.js** (for `execute_code` JS runtime)
- **Python 3** (for `execute_python` runtime)
- **Google Chrome** (for `browser_*` tools)
- **LLM endpoint** — any OpenAI-compatible API (local proxy, Ollama, vLLM, etc.)

### Install

```bash
git clone https://github.com/hirotomasato/hiroto.git
cd hiroto
go build -o ~/.local/bin/hiroto ./cmd/hiroto
```

### Configure

```bash
mkdir -p ~/.hiroto
```

Create `~/.hiroto/config.yaml`:

```yaml
model:
  base_url: http://localhost:20128/v1
  model: your-model-name
  api_key: ${HIROTO_API_KEY}

agent:
  max_turns: 40
  terminal_timeout: 180
  compression_budget_tokens: 25000
  compression_keep_turns: 6
```

Create `~/.hiroto/.env`:

```bash
HIROTO_API_KEY=your-api-key
```

### Run

```bash
hiroto                  # Interactive TUI
hiroto -q "your query"  # One-shot mode
hiroto --banner         # Show banner
hiroto --models         # Interactive model picker
hiroto --set-model NAME # Switch model
hiroto gateway          # Start Telegram bot
```

---

## TUI Shortcuts

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Alt+Enter` | New line |
| `Ctrl+P` | Model picker |
| `Ctrl+R` | Resume session |
| `Ctrl+S` | Save session |
| `Ctrl+L` | Clear screen |
| `Ctrl+C` | Cancel / Quit |
| `↑` `↓` | Input history |
| `PgUp` `PgDn` | Scroll chat |
| `Home` `End` | Top / Bottom |

### Slash Commands

```
/help   /new   /resume   /compress   /skills
/model  /memory add  /memory del  /todo  /quit
```

---

## Built-in Tools (32)

| Category | Tools |
|----------|-------|
| **Shell** | `terminal`, `process` (background), `smart_pipe` (chain) |
| **Files** | `read_file`, `write_file`, `patch` (targeted edit), `search_files` |
| **Web** | `web_search`, `web_extract`, `web_fetch` |
| **Browser** | `browser_start`, `browser_navigate`, `browser_click`, `browser_type`, `browser_exec`, `browser_screenshot_cdp`, `browser_stop`, `browser_fetch`, `browser_screenshot` |
| **Code** | `execute_code` (JS/Node), `execute_python` |
| **Security** | `secret_scan`, `search_knowledge`, `aggregate_reports` |
| **Memory** | `memory` (persistent), `todo` |
| **Skills** | `skill_view`, `skill_manage` (create/edit/delete) |
| **Session** | `session_search` |
| **Vision** | `vision_analyze` (image to LLM) |
| **Agent** | `clarify` (ask user), `delegate_task` (sub-agent), `cronjob` (schedule) |

---

## External Security Tools

Install these for full cybersecurity capability:

```bash
go install github.com/projectdiscovery/httpx/cmd/httpx@latest
go install github.com/projectdiscovery/subfinder/v2/cmd/subfinder@latest
go install github.com/projectdiscovery/katana/cmd/katana@latest
go install github.com/projectdiscovery/nuclei/v3/cmd/nuclei@latest
go install github.com/projectdiscovery/dnsx/cmd/dnsx@latest
go install github.com/lc/gau/v2/cmd/gau@latest
go install github.com/owasp-amass/amass/v4/...@master
go install github.com/ffuf/ffuf/v2@latest
go install github.com/tomnomnom/waybackurls@latest
go install github.com/hakluke/hakrawler@latest
go install github.com/OJ/gobuster/v3@latest
pip install sqlmap strix --break-system-packages
```

---

## Project Structure

```
hiroto/
├── cmd/hiroto/           # Entry point: TUI, one-shot, gateway, CLI
│   ├── main.go           # Wiring, UI, update loop
│   ├── banner.go         # Gradient banner + branding
│   ├── picker.go         # List picker overlay (model, session)
│   └── sessions.go       # Session glue: save, load, resume
├── internal/
│   ├── agent/            # Agent loop, system prompt, compression
│   ├── llm/              # OpenAI-compatible client (stream + non-stream)
│   ├── tools/            # 32 tool implementations + registry
│   ├── skills/           # SKILL.md discovery + frontmatter parser
│   ├── memory/           # Persistent memory store (JSON)
│   ├── session/          # Conversation persistence (JSON)
│   ├── config/           # YAML config + .env resolver
│   ├── web/              # Web search (Bing RSS) + extract
│   ├── plugin/           # Plugin loader + MCP client
│   └── gateway/          # Telegram bot
├── go.mod
├── go.sum
├── README.md
└── LICENSE
```

---

## Data Directory

```
~/.hiroto/
├── config.yaml           # Settings
├── .env                  # Secrets (API keys)
├── skills/               # 234 skill files (cybersecurity, devops)
├── memory/               # user.json + memory.json
├── sessions/             # Saved conversations
├── plugins/              # External plugins
└── todo.json             # Agent checklist
```

---

## Skill Example

Skills are markdown files with YAML frontmatter. They define workflows the agent can follow:

```markdown
---
name: recon
description: Subdomain enumeration, DNS resolution, and HTTP probing for bug bounty targets.
---

# Recon Skill

1. Discover subdomains: `subfinder -d <target>`
2. Resolve DNS: `dnsx -l subs.txt`
3. Probe HTTP: `httpx -l resolved.txt`
```

The agent loads skills automatically and uses them when relevant.

---

## Development

```bash
# Build
go build -o hiroto ./cmd/hiroto

# Run tests
go test ./...

# Vet
go vet ./...

# Install locally
go build -o ~/.local/bin/hiroto ./cmd/hiroto
```

---

## License

MIT — see [LICENSE](LICENSE).

---

> **hirotomasato/hiroto** — personal AI agent, built from scratch in Go.