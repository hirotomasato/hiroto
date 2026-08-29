# Hiroto

> AI agent yang tinggal di terminal kamu. Tulis kode, scan bug, otomatisasi workflow — semua dari command line.

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev)
[![Version](https://img.shields.io/github/v/release/hirotomasato/hiroto?color=blue)](https://github.com/hirotomasato/hiroto/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/hirotomasato/hiroto/ci.yml?branch=main)](https://github.com/hirotomasato/hiroto/actions)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

---

## Apa ini

Hiroto adalah agen AI yang jalan di terminal. Bukan chatbot biasa — dia bisa jalanin shell command, buka browser, scan kode, cari bug, bikin laporan, dan nyambung ke Telegram. Semua dari keyboard.

Ditulis dari nol pake Go. Ga ada dependency Python buat core-nya. Binary 24MB, jalan di Linux, Mac, dan ARM.

---

## Install

Satu command:

```bash
curl -fsSL https://raw.githubusercontent.com/hirotomasato/hiroto/main/install.sh | bash
```

Itu install Hiroto, 10+ security tools, 240 skills, dan bikin config — otomatis. Butuh Go 1.23+, Node.js, Python 3, sama Chrome.

Atau clone manual:

```bash
git clone https://github.com/hirotomasato/hiroto.git
cd hiroto
make install
```

Config ada di `~/.hiroto/config.yaml`. API key di `~/.hiroto/.env`.

---

## Cara pakai

```bash
hiroto                  # TUI interaktif
hiroto -q "siapa aku?"  # One-shot, langsung jawab
hiroto gateway          # Jalanin Telegram bot
hiroto --update         # Cek update
```

Di TUI, semua dari keyboard:

| Tombol | Buat apa |
|--------|----------|
| `Enter` | Kirim pesan |
| `Ctrl+P` | Ganti model |
| `Ctrl+R` | Lanjutin sesi lama |
| `Ctrl+L` | Bersihin layar |
| `Ctrl+S` | Simpan sesi |
| `Ctrl+C` | Batal / keluar |
| `PgUp` `PgDn` | Scroll naik-turun |

Slash commands: `/help /new /resume /compress /update /upgrade /model /memory /todo /quit`

---

## Yang bisa dia lakuin

**Terminal & file:** jalanin shell command, baca/tulis/edit file, background process, pipe chaining.

**Browser:** buka halaman, klik, ketik, ambil teks, screenshot. Full session Chrome headless. Bisa login, isi form, scrape.

**Kode:** jalanin JavaScript (Node) atau Python langsung dari chat. Script bisa panggil balik tool Hiroto.

**Security:** scan secret, cari pengetahuan dari 240 skill (recon, exploit, reporting), merge laporan, aggregate findings.

**Data:** web search, ekstrak konten halaman, cari di sesi lama, baca gambar.

**Otomatisasi:** cronjob buat tugas berulang, delegate task ke sub-agent, Telegram bot gateway.

---

## Tools bawaan

```
terminal    read_file    write_file    patch    search_files
web_search  web_extract  web_fetch
browser_start  browser_navigate  browser_click  browser_type
browser_exec   browser_screenshot_cdp  browser_stop
browser_fetch  browser_screenshot
execute_code   execute_python
secret_scan    search_knowledge   aggregate_reports   smart_pipe
process        delegate_task      cronjob
memory         todo               skill_view          skill_manage
session_search  vision_analyze    clarify
```

---

## Skills

240 skill siap pakai. Tinggal sebut nama skill-nya, agent bakal load dan jalanin.

```
recon     → subfinder + dnsx + httpx
hunt-xss  → 174 pola bug bounty XSS
hunt-sqli → SQL injection hunting
antislop  → filter anti AI-slop buat output
...dan 236 lainnya
```

---

## Kenapa Go

Binary 24MB, ga perlu runtime, ga perlu venv, ga perlu Docker. Build sekali, jalan di mana aja. Cross-compile ke Linux/Mac/ARM dari laptop.

---

## Development

```bash
make build    # go build
make test     # go test ./...
make vet      # go vet ./...
make install  # install ke ~/.local/bin
```

---

## License

MIT — [LICENSE](LICENSE)

---

Dibuat dari nol di Go. Ga ada dependency framework AI apa pun. Binary 24MB, 240 skills, 32 tools.