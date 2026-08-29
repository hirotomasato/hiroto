# Contributing

PRs welcome. Keep changes small and tied to one problem.

## Setup

```bash
git clone https://github.com/hirotomasato/hiroto.git
cd hiroto
go test ./...
go vet ./...
go build -o hiroto ./cmd/hiroto
```

Need Go 1.26+. Config lives in `~/.hiroto/` — don't commit secrets.

## Before you open a PR

1. `gofmt -w` on touched files
2. `go vet ./...` and `go test ./...`
3. Don't add a Python/Node runtime to the core. Tools stay Go.
4. Don't mention Hermes or Cybermes in code, comments, or docs
5. Skills go under `skills/<name>/SKILL.md` with YAML frontmatter (`name`, `description`)

## Commit style

```
area: short summary
```

Examples: `tools: windows shell fallback`, `readme: drop binary download claim`.

## Issues

Use the bug / feature templates. Include OS, Go version, and the exact command that failed.
