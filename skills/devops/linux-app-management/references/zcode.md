# ZCode (Z.ai) — session-verified specifics (Aug 2026)

Session context: user asked to uninstall ZCode 3.7.7 completely, then reinstall the latest version. Everything below was verified live on the user's Zorin OS machine.

## Product facts
- ZCode = Z.ai (Zhipu) desktop agentic coding IDE, "Official Harness for GLM-5.3". Electron app.
- Linux builds are marked Beta. Ships as `.deb` and `.AppImage` for x64 and arm64.
- Deb version format: `3.10.1-6272` (version-build). Installed size ~631 MB per apt.
- Docs site: https://zcode.z.ai/en/docs/install — recommends AppImage for Linux x64, but the deb works fine and registers `zcode` in PATH + app menu.
- Troubleshooting docs (linux-wsl): don't run deb and AppImage simultaneously; don't launch with sudo.

## Official CDN URL pattern (do not guess — grep the homepage)
```
https://cdn-zcode.z.ai/zcode/electron/releases/<VER>/linux-x64/ZCode-<VER>-linux-x64.deb
https://cdn-zcode.z.ai/zcode/electron/releases/<VER>/linux-x64/ZCode-<VER>-linux-x64.AppImage
https://cdn-zcode.z.ai/zcode/electron/releases/<VER>/linux-arm64/ZCode-<VER>-linux-arm64.deb
(also: macos-arm64/macos-x64/*.dmg, windows-x64/windows-arm64/*.exe)
```
Discovery command that worked:
`curl -sL https://zcode.z.ai/en | grep -oE 'https?://[^"'"'"' ]+\.(deb|AppImage)' | sort -u`

## Version-lag trap (IMPORTANT)
On 2026-08-29 the changelog page (zcode.z.ai/en/changelog) listed **3.8.1 (Aug 20, 2026)** as the newest release, and web search results also said 3.8.1 — but the homepage's own download links already served **3.10.1** (deb last-modified Aug 28, 2026). The changelog lags the CDN. Always grep the homepage HTML for the actual newest version.

## Full artifact map (3.7.7, observed before removal)
System (owned by deb, removed by `pkexec apt-get purge -y zcode`):
- `/opt/ZCode/` — app itself (~597 MB freed per apt)
- `/usr/bin/zcode` → `/etc/alternatives/zcode` → `/opt/ZCode/zcode` (update-alternatives link group `zcode`)
- `/usr/share/applications/zcode.desktop`, `/usr/share/doc/zcode/`, hicolor icons 16–1024px
- `/etc/apparmor.d/zcode` — NOT in dpkg -L, but purge removed it anyway (maintainer script)

User-level (removed manually, no sudo):
- `~/.zcode/` (66M) — certs (`v2/certs/zcode-network-ca.*`), CLI plugins/cache/logs (`cli/plugins/...`, `cli/log/zcode-YYYY-MM-DD.jsonl`), crash dirs (recreated on launch: `v2/crash/{live,archive}`)
- `~/.config/ZCode/` (62M) — Electron profile incl. `session/Partitions/zcode-embedded-browser`
- `~/.cache/@zcodedesktop-updater/` (132M) — contained pending `ZCode-3.8.1-linux-x64.deb` auto-update
- `~/.local/share/applications/zcode.desktop`
- `~/Downloads/ZCode-3.7.7-linux-x64.deb` (132M) — original installer
- `~/.cache/zcode-check/` — NOT created by the app; leftover from Hiroto endpoint/quota inspection sessions (check_endpoints*.py scripts + raw API responses incl. `balance_jwt.json` / token material). Not in the app's own footprint, but delete it on any full clean — it contains session token data. Verified present Aug 2026.

## Clean reinstall session (2026-08-29, second run)
- Same flow re-verified: pkexec purge zcode 3.10.1-6272 (631 MB freed) → rm user artifacts incl. ~/.cache/zcode-check and /tmp/zcode_home.html (cached homepage HTML from version check) → reinstall same 3.10.1 deb.
- Local installer sha256 re-matched the recorded hash exactly → reused instead of re-downloading. CDN homepage still listed 3.10.1 as newest.
- Post-install find sweep was clean: only /usr/bin/zcode, /etc/alternatives/zcode, /etc/apparmor.d/zcode, /opt/ZCode, /var/lib/dpkg/info/zcode.* and the installer deb in ~/Downloads remained.

## Reinstall result (verified)
- Downloaded `ZCode-3.10.1-linux-x64.deb` (146,822,456 bytes, sha256 `1deceec01e0545a4c7f8e26516269b1e845dfc3524598b1655e38bc61acbf08b`) to `~/Downloads/`.
- `pkexec apt-get install -y ~/Downloads/ZCode-3.10.1-linux-x64.deb` → exit 0, version `3.10.1-6272`.
- Fresh install re-creates `~/.zcode` on first launch (certs, crash dirs) and registers the `zcode://` protocol handler — expected, not junk.
