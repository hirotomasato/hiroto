---
name: linux-desktop-admin
description: Use when sudo needs a password; desktop Linux app admin.
category: devops
---

# Desktop Linux Administration from Agent Sessions

Scope: managing installed applications and system state on a desktop Linux machine directly from the agent terminal (user's main laptop is Zorin OS), where interactive `sudo` is unavailable.

## Privilege escalation: pkexec, not just sudo

1. Probe first: `sudo -n true 2>&1` — exit 1 means sudo needs a password (typical in agent shells). Do not stop there.
2. On a desktop session use `pkexec <command>`: a PolicyKit auth dialog pops up on the user's screen. Tell the user to type their password there.
3. Verified working: `pkexec apt-get purge -y <pkg>` and `pkexec apt-get install -y /path/to/file.deb`.
4. Requires a running polkit agent (GUI session). Headless/SSH → hand the exact command to the user instead of guessing.

## Uninstall workflow (clean, verified)

1. Identify: `which -a <cmd>`, `dpkg -l | grep -i <name>`, plus snap/flatpak/pip/npm lists.
2. Footprint: `dpkg -L <pkg> | sort` for the exact file list, then `find / -iname "*<name>*" -not -path "/proc/*" -not -path "/sys/*"` for non-package artifacts (configs, caches, apparmor profiles, leftover symlinks).
3. Check persistence: `~/.config/autostart/`, `systemctl --user list-unit-files`, `crontab -l`; check for running processes with the bracket-trick pgrep (see pitfalls).
4. Home artifacts need no sudo: delete the specific paths found (`~/.<app>`, `~/.config/<App>`, `~/.cache/@<app>*`, `~/.local/share/applications/*.desktop`, installers in ~/Downloads), then verify each with `ls` and show the proof.
5. Purge via pkexec. Note: files NOT owned by the package (e.g. apparmor profiles the installer dropped) survive purge — remove them explicitly.
6. Final sweep: rerun find + `dpkg -l`; report freed disk space from apt output.

## Pitfalls

- **pkill -f self-match**: from an agent wrapper, `pkill -f "/opt/App/binary"` matches the wrapper's own `bash -c "...pattern..."` command line and SIGTERMs the running command itself (observed twice in one session: exit_code -15, empty output). To list processes use `pgrep -af "[o]pt/App"` (bracket trick); to kill, prefer `pkill -x <exact-name>` or kill the PID from pgrep.
- **Electron binaries launch the GUI on `--version`**: `app --version` may start the main process and hang the terminal instead of printing a version. Read versions from `dpkg -l`, and close anything accidentally launched (see pkill pitfall).
- **Verify downloads byte-exact**: `curl -sIL <url>` for `Content-Length`, compare against downloaded size before installing a deb.
- **Installer URLs hide in HTML**: marketing pages often don't render download links as text (and article extraction truncates them). `curl -sL <page> | grep -oE 'https?://[^"]+\.(deb|AppImage|dmg|exe)'` finds CDN links reliably.