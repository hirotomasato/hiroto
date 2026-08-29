---
name: linux-app-management
description: Fully remove/reinstall Linux deb desktop apps & artifacts.
---

# Linux Desktop App Management

Class-level workflow for fully removing, cleaning up, and (re)installing third-party desktop applications on the user's Ubuntu-family systems (Zorin OS laptop). Covers deb packages, Electron apps, and their user-level artifacts.

## When to use
- "uninstall X and delete all its artifacts", "reinstall the latest X", "upgrade X to the newest version", "X left junk on my system"

## Uninstall workflow (verified end-to-end on this machine)
1. **Identify install type** in one sweep: `which -a <name>`, `dpkg -l | grep -i <name>`, plus snap/flatpak/pip/npm lists — third-party apps arrive by any of these.
2. **Map the package's own files**: `dpkg -L <pkg>`. This is what purge will remove.
3. **Map ALL artifacts on disk** (purge does not reliably remove what the package doesn't list):
   `find / -iname "*<name>*" -not -path "/proc/*" -not -path "/sys/*" 2>/dev/null`
   Cross-check hits with `dpkg -S <path>` — but treat mismatches as "verify after purge", not as "must delete manually": maintainer scripts sometimes clean unlisted files anyway.
4. **Check it's not running** and has no auto-restart hooks: `pgrep -af <name>`, `ls ~/.config/autostart/`, `systemctl --user list-unit-files | grep`, `crontab -l | grep`.
5. **Elevation**: plain `sudo -n` fails on this machine (password required, agent has no tty). **Use `pkexec apt-get purge -y <pkg>`** — a GUI password dialog appears on the user's screen; warn the user to enter their password first. exit 0 = success. pkexec also works for `pkexec apt-get install -y /path/to/file.deb`.
6. **Delete user-level artifacts** (no sudo needed):
   - `~/.config/<App>` (settings/profile), `~/.cache/@<app>-updater` (pending auto-updates — often 100+ MB), `~/.<name>` (data/certs/logs), `~/.local/share/applications/<name>.desktop` (user-level launcher), `~/Downloads/<name>*.deb` (leftover installer).
   - Batch these in ONE `rm -rf` command; expect a security-scan flag for mass recursive deletion (review then proceed).
7. **Verify**: re-run the `find` sweep + `dpkg -l | grep` — expect zero hits. Report freed disk space from apt's "After this operation ... will be freed" line plus the du of removed home dirs.

## Finding the REAL latest version (reinstall/upgrade)
- Docs/changelog pages can LAG the actual release (changelog listed 3.8.1 as latest while the site's own CDN already served 3.10.1). Never take the docs version number at face value.
- Fetch the download page HTML directly and grep CDN URLs:
  `curl -sL <homepage> | grep -oE 'https?://[^"'"'"' ]+\.(deb|AppImage)' | sort -u`
- Confirm the file is live with `curl -sIL` (expect HTTP 200 + content-length), download, then verify the downloaded size matches content-length and record sha256sum.

## Electron app pitfalls
- **`app --version` launches the GUI instead of printing a version** — the verification call hangs until timeout. Verify installs via `dpkg -l <pkg>` + file existence, never by running the binary.
- If a verification attempt launched the app, check with the self-safe pattern `pgrep -af "[o]pt/<App>"` (bracket trick). Avoid `pkill -f "/opt/<App>"`: the pattern matches the harness shell's own command line and SIGTERMs your own command (exit -15).
- First launch (even accidental) re-initializes `~/.<name>` and registers protocol handlers — normal for a fresh install, not a leftover artifact.

## References
- `references/zcode.md` — ZCode (Z.ai) specifics: official CDN URL pattern, full artifact map, version-lag trap, session-verified Aug 2026.