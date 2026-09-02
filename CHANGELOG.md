# Changelog

## 0.6.0 — 2026-08-30

Hermes-parity pass across the TUI and Telegram gateway.

### TUI
- Live tool-activity lines with natural English labels ("Reading main.go", "Running go build"); model can author its own via an `activity` arg
- Working spinner (was static) + mouse-wheel scrolling
- Colored unified diffs for `patch`/`write_file` output
- Live todo panel (bordered checklist above the input)
- Chroma syntax highlighting for markdown code blocks
- Budgeted tool output (compact/full/log) with hidden-count footer
- Banner top padding

### Agent
- Parallel tool execution: independent tool calls in one turn run concurrently, results order-preserved
- Proactive skill-capture guidance in the system prompt

### Telegram gateway
- Command menu (Telegram "/" button) via setMyCommands
- Clean final answer as its own Markdown message, separated from progress breadcrumbs
- `/stop` (cancel running turn) and `/compress`
- Configurable `tool_progress` (all/new/off), `cleanup_progress`, `typing_indicator`
- User allowlist (deny-by-default) via `gateway.allowed_users` or `HIROTO_TELEGRAM_ALLOWED_USERS`
- Shared activity labels with the TUI

## Unreleased

### Layout (fix: garis atas banner hilang)
- Frame TUI kini dijamin pas sama tinggi terminal: chrome layout diukur dari satu sumber (`layoutParts`), slot kosong (clarify/flash/panel) gak lagi ngeluarin baris hantu, jadi garis rule di atas wordmark HIROTO balik tampil di semua ukuran terminal

### Task list (fix: task nyangkut di panel)
- Checklist is now **per session** (`~/.hiroto/todos/<id>.json`) instead of one global `todo.json`; a fresh launch no longer inherits the previous run's plan, and the legacy file is removed on first start
- Plan is stored with the session and restored on `/resume` / `--resume` / `-c` (never restored as in-progress)
- `todo` tool gained working `update` (merge by id), plus `complete` and `clear`; exactly one task may be in_progress
- In-progress tasks are released back to pending when a run is cancelled or fails (Ctrl+C, gateway error) — the panel can't claim work is running when nothing is
- `/todo` is a real command: `add`, `done <id>`, `undo <id>`, `unstick`, `clear`; Telegram `/todo` shows and edits the same list
- Panel windows around the active task on long plans, marks an idle in-progress row, and refreshes via stat instead of re-parsing every frame
- Agent prompt now spells out task-list discipline (advance by id, close out before finishing)

- Exit summary on quit: resume commands (`hiroto --resume <id>`, `hiroto -c "<title>"`), session id, title, duration, message counts
- `hiroto -c "prompt"` — continue the last saved session, one-shot
- `hiroto --resume <id>` — reopen a saved session in the TUI with full transcript

## 0.4.2 — 2026-08-29

- Fix `.gitignore` silently dropping `cmd/hiroto/` — the tagged module now ships the full source and `go install ...@latest` works again
- Banner polish: tighter layout, higher contrast

## 0.4.1 — 2026-08-29

- First tagged GitHub Release (`/releases/latest` for `hiroto --update`)
- 240 bundled skills, including antislop
- Cross-platform shell (`bash -c` / `cmd /c`)
- Windows installer (`install.ps1`)
- CI on push/PR; Release workflow publishes notes on `v*` tags
- README in English; banner under `assets/`

## 0.4.0

- TUI polish, markdown render, 32 built-in tools
- Telegram gateway, plugins, MCP
- Native tools: `secret_scan`, `search_knowledge`, `aggregate_reports`, `smart_pipe`
