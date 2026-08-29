# Hiroto

<p align="center">
  <img src="assets/hiroto.svg" alt="Hiroto — personal agent · go core · cyberteam" width="720">
</p>

> Your terminal, your agent. Write code, hunt bugs, automate workflows — all from the command line.

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev)
[![Version](https://img.shields.io/github/v/release/hirotomasato/hiroto?color=blue)](https://github.com/hirotomasato/hiroto/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/hirotomasato/hiroto/ci.yml?branch=main)](https://github.com/hirotomasato/hiroto/actions)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Go Report](https://goreportcard.com/badge/github.com/hirotomasato/hiroto)](https://goreportcard.com/report/github.com/hirotomasato/hiroto)

---

## What is this

Hiroto is an AI agent that lives in your terminal. Not a web chatbot — it runs shell commands, opens browsers, scans code, hunts bugs, writes reports, and connects to Telegram. All from your keyboard.

Written from scratch in Go. No Python runtime needed for the core. 24MB binary, runs on Linux, macOS, and Windows.

---

## Install

One command:

```bash
curl -fsSL https://raw.githubusercontent.com/hirotomasato/hiroto/main/install.sh | bash
```

That installs Hiroto, 10+ security tools, and 240 skills automatically. You need Go 1.26+, Node.js, Python 3, and Chrome.

Windows:

```powershell
irm https://raw.githubusercontent.com/hirotomasato/hiroto/main/install.ps1 | iex
```

Or clone and build:

```bash
git clone https://github.com/hirotomasato/hiroto.git
cd hiroto
make install
```

Config lives in `~/.hiroto/config.yaml`. API key goes in `~/.hiroto/.env`.

---

## Usage

```bash
hiroto                  # Interactive TUI
hiroto -q "who am i?"   # One-shot, prints answer
hiroto -c "prompt"      # Continue the last session (one-shot)
hiroto --resume <id>    # Reopen a saved session in the TUI
hiroto gateway          # Telegram bot
hiroto --update         # Check for updates
```

In the TUI, everything is keyboard-driven:

| Key | What it does |
|-----|-------------|
| `Enter` | Send message |
| `Ctrl+P` | Switch model |
| `Ctrl+R` | Resume an old session |
| `Ctrl+L` | Clear screen |
| `Ctrl+S` | Force save session |
| `Ctrl+C` | Cancel / quit |
| `PgUp` `PgDn` | Scroll up and down |

Slash commands: `/help /new /resume /compress /update /upgrade /model /memory /todo /quit`

Quitting (`Ctrl+C` or `/quit`) prints a resume recap — session id, title, duration, message counts — so you can pick the conversation back up straight from the shell:

```
Resume this session with:
  hiroto --resume 20260829_174120_39daaf
  hiroto -c "Perbaiki error push dan protect main branch"

Session:        20260829_174120_39daaf
Title:          Perbaiki error push dan protect main branch
Duration:       18m 24s
Messages:       81 (2 user, 77 tool calls)
```

---

## What it can do

**Terminal & files:** run shell commands, read, write, edit files, background processes, pipe chaining.

**Browser:** open pages, click, type, extract text, screenshot. Full Chrome headless session. Log in, fill forms, scrape.

**Code:** run JavaScript (Node) or Python directly from chat. Scripts can call back into Hiroto's tools.

**Security:** scan for secrets, search 240 skills (recon, exploit, reporting), merge reports, aggregate findings.

**Data:** web search, extract page content, search old sessions, read images.

**Automation:** cron jobs for recurring tasks, delegate work to sub-agents, Telegram bot gateway.

---

## Built-in tools

| Category | Tools |
|----------|-------|
| Shell | `terminal` `process` `smart_pipe` |
| Files | `read_file` `write_file` `patch` `search_files` |
| Web | `web_search` `web_extract` `web_fetch` |
| Browser | `browser_start` `browser_navigate` `browser_click` `browser_type` `browser_exec` `browser_screenshot_cdp` `browser_stop` `browser_fetch` `browser_screenshot` |
| Code | `execute_code` (JS) `execute_python` |
| Security | `secret_scan` `search_knowledge` `aggregate_reports` |
| Agent | `delegate_task` `cronjob` `clarify` |
| Memory | `memory` `todo` |
| Skills | `skill_view` `skill_manage` |
| Data | `session_search` `vision_analyze` |

---

## Skills

240 skills included. Mention a skill name and the agent loads and runs it.

```
recon     → subfinder + dnsx + httpx
hunt-xss  → 174 XSS bug bounty patterns
hunt-sqli → SQL injection hunting
antislop  → anti-AI-slop output filter
...and 236 more
```

---

## Project layout

```
cmd/hiroto/          Entry point: TUI, one-shot, gateway
internal/
  agent/             Agent loop, system prompt, compression
  llm/               OpenAI-compatible client
  tools/             Tool implementations + registry
  skills/            Skill discovery + parser
  memory/            Persistent memory (JSON)
  session/           Conversation persistence
  config/            YAML config + .env resolver
  web/               Web search + extract
  plugin/            Plugin loader + MCP client
  gateway/           Telegram bot
skills/              240 bundled skills
```

---

## Releases

GitHub already ships a source zip/tarball for every tag. That's enough if you build from source (`go install` / `install.sh`).

`hiroto --update` looks at `/releases/latest`. A tag without a published Release is invisible to the updater. Install is `go install` / `install.sh`, not a downloaded binary.

Download: https://github.com/hirotomasato/hiroto/releases

---

## Development

```bash
make build    # go build
make test     # go test ./...
make vet      # go vet ./...
make install  # install to ~/.local/bin
```

See [CONTRIBUTING.md](CONTRIBUTING.md). Security reports: [SECURITY.md](SECURITY.md). Changes: [CHANGELOG.md](CHANGELOG.md).

---

## License

MIT — [LICENSE](LICENSE)

---

Built from scratch in Go. 24MB binary. 240 skills. 32 tools. No AI framework dependencies.