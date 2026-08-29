# Security

Report vulnerabilities privately. Do not open a public issue for an exploit.

- Email the maintainer via GitHub: https://github.com/hirotomasato
- Or open a [private security advisory](https://github.com/hirotomasato/hiroto/security/advisories/new)

Include: affected version, reproduction, and impact.

## Scope

In scope: the Hiroto binary, `install.sh` / `install.ps1`, default tools that run without extra flags.

Out of scope: third-party security CLIs installed by `install.sh` (nuclei, sqlmap, …), LLM provider endpoints, and skills that tell the agent how to use those tools.

## Notes

Hiroto runs shell commands and browser automation on the host. Treat it like any other local agent: don't point it at untrusted prompts with secrets in the environment.
