# Changelog

## Unreleased

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
