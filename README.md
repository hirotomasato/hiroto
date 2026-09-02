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

Built from scratch in Go. No Python runtime needed for the core. 24MB binary, runs on Linux, macOS, and Windows.

---

## Install

One command:

```bash
curl -fsSL https://raw.githubusercontent.com/hirotomasato/hiroto/main/install.sh | bash
```

That installs Hiroto, security tools, and 240+ skills automatically. Requires Go 1.26+, Node.js, Python 3, and Chrome.

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
hiroto -c "prompt"      # Continue last session (one-shot)
hiroto --resume <id>    # Reopen a saved session in the TUI
hiroto gateway          # Telegram bot
hiroto --api            # OpenAI-compatible API server
hiroto --update         # Check for updates
```

### TUI keyboard shortcuts

| Key | What it does |
|-----|-------------|
| `Enter` | Send message |
| `Ctrl+P` | Switch model |
| `Ctrl+R` | Resume old session |
| `Ctrl+L` | Clear screen |
| `Ctrl+S` | Force save session |
| `Ctrl+C` | Cancel / quit |
| `PgUp` `PgDn` | Scroll |
| `Home` `End` | Jump to top/bottom |

### Slash commands (31 commands)

Type `/` to open the scrollable command picker — arrow keys to navigate, Enter to select.

**Session:**
`/help` `/new` `/resume` `/compress` `/quit` `/branch` `/title`

**Model & config:**
`/model` `/config` `/reasoning` `/verbose`

**Coding:**
`/diff` `/review` `/explain` `/test` `/stop`

**Iteration:**
`/retry` `/undo` `/steer`

**Tools & skills:**
`/skills` `/memory` `/reload` `/usage`

**Productivity:**
`/prompt` `/bg` `/goal` `/copy` `/image` `/todo`

`/todo` manages the live checklist shown above the input: bare `/todo` lists it,
`/todo add <teks>`, `/todo done <id>`, `/todo undo <id>`, `/todo unstick` (release
tasks the agent left marked in-progress), `/todo clear`. The list is per session
and is stored with it, so resuming a session brings its plan back.

**Update & rollback:**
`/update` `/upgrade` `/rollback`

### Exit summary

Quitting (`Ctrl+C` or `/quit`) prints a resume recap — session id, title, duration, message counts — so you can pick the conversation back up from the shell:

```
Resume this session with:
  hiroto --resume 20260829_174120_39daaf
  hiroto -c "Fix the push error and protect main branch"

Session:        20260829_174120_39daaf
Title:          Fix the push error and protect main branch
Duration:       18m 24s
Messages:       81 (2 user, 77 tool calls)
```

---

## Project context

When you open Hiroto in a project folder, it auto-detects context files (`AGENTS.md`, `CLAUDE.md`, `.cursorrules`, `.hermes.md`) and injects them into the system prompt. The agent follows your project conventions without being told.

The system prompt also includes your git working directory, skill index, memory, and any standing goal you've set.

---

## OpenAI-compatible API server

```bash
hiroto --api
```

Opens `http://localhost:20129/v1` — an OpenAI-compatible endpoint. Streaming + non-streaming, full tool access.

Use with VS Code (Continue, Cline, Cody), Aider, Open WebUI, LibreChat, or plain curl:

```bash
curl http://localhost:20129/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"tok/deepseek/deepseek-v4-pro","messages":[{"role":"user","content":"hello"}]}'
```

Port can be set in `~/.hiroto/config.yaml`:
```yaml
api:
  port: 20129
```

Endpoints: `/v1/chat/completions`, `/v1/models`, `/health`.

---

## Telegram gateway

```bash
hiroto gateway
```

On first run, you're prompted for a Telegram bot token (from @BotFather). The token is stored in `.env`, never in config.yaml. The bot supports:

- Per-chat isolated conversations (no context leakage)
- Live text + tool activity streaming
- Persistent sessions (survive restarts, resume via `/resume`)
- Full command set: `/model` `/resume` `/memory` `/skills` `/retry` `/undo` `/status` `/sessions`

---

## What it can do

**Terminal & files:** shell commands, read, write, edit, background processes, pipe chaining. LSP diagnostics auto-run after writes (go vet, py_compile, cargo check, tsc).

**Browser:** 9 tools — start, navigate, click, type, JS exec, screenshot, stop, fetch, DOM dump. Full Chrome headless session.

**Code:** execute_code (Python + tool RPC), execute_python. Scripts can call back into Hiroto's tools.

**Safety:** auto git checkpoint before write/patch/terminal. `/rollback` to undo. Dangerous command detection (rm -rf, git push --force, DROP, dd, chmod).

**Security:** scan secrets, 240+ skills (recon, exploit, reporting), merge reports.

**Data:** web search, extract page content, session search, vision (image analysis).

**Automation:** cron jobs, delegate to sub-agents, background prompt (`/bg`), standing goal (`/goal`).

**Productivity:** edit prompt in $EDITOR (`/prompt`), copy response to clipboard (`/copy`), attach images (`/image`), steer mid-run (`/steer`).

---

## Built-in tools

| Category | Tools |
|----------|-------|
| Shell | `terminal` `process` `smart_pipe` |
| Files | `read_file` `write_file` `patch` `search_files` |
| Web | `web_search` `web_extract` `web_fetch` |
| Browser | `browser_start` `browser_navigate` `browser_click` `browser_type` `browser_exec` `browser_screenshot_cdp` `browser_stop` `browser_fetch` `browser_screenshot` |
| Code | `execute_code` `execute_python` |
| Security | `secret_scan` `search_knowledge` `aggregate_reports` |
| Agent | `delegate_task` `cronjob` `clarify` |
| Memory | `memory` `todo` |
| Skills | `skill_view` `skill_manage` |
| Data | `session_search` `vision_analyze` |

---

## Skills

240+ skills included. Mention a skill name and the agent loads and runs it.

```
recon     → subfinder + dnsx + httpx
hunt-xss  → 174 XSS bug bounty patterns
hunt-sqli → SQL injection hunting
antislop  → anti-AI-slop output filter
...and 236+ more
```

---

## Project layout

```
cmd/hiroto/          Entry point: TUI, one-shot, gateway, API server
internal/
  agent/             Agent loop, system prompt, compression, steer
  api/               OpenAI-compatible HTTP server
  llm/               OpenAI-compatible client (streaming + non-streaming)
  tools/             Tool implementations + registry + LSP + safety
  skills/            Skill discovery + parser
  memory/            Persistent memory (JSON)
  session/           Conversation persistence
  config/            YAML config + .env resolver
  web/               Web search + extract
  plugin/            Plugin loader + MCP client
  gateway/           Telegram bot
skills/              240+ bundled skills
```

---

## Releases

`go install github.com/hirotomasato/hiroto@latest` — or `install.sh`. GitHub Releases are auto-created for every `v*` tag.

`hiroto --update` checks `/releases/latest`. Tags without a published Release are invisible to the updater.

Download: https://github.com/hirotomasato/hiroto/releases

---

## Development

```bash
make build    # go build
make test     # go test ./...
make vet      # go vet ./...
make install  # install to ~/.local/bin
```

See [CONTRIBUTING.md](CONTRIBUTING.md). Security reports: [SECURITY.md](SECURITY.md). Changelog: [CHANGELOG.md](CHANGELOG.md).

---

## License

MIT — [LICENSE](LICENSE)

---

Built from scratch in Go. 24MB binary. 240+ skills. 26 tools. 31 slash commands. API server. Telegram gateway. No AI framework dependencies.