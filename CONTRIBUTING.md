# Contributing to Aegys Bot

Thanks for contributing to **Aegys Bot**! This project is designed to stay clean, modular, and easy to run locally without requiring external infrastructure or private credentials.

## Development Setup

Requirements: **Go 1.24+**.

```bash
git clone https://github.com/AetherCore-Works/aegys-bot.git
cd aegys-bot

# Run the test suite with race detector
go test -race ./...

# Try the interactive CLI sandbox (no API keys required)
go run ./cmd/aegys --dev-cli
```

## Project Layout

- `cmd/aegys/` — Main entrypoint (`--dev-cli` and daemon server).
- `internal/config/` — Environment configuration loader.
- `internal/rbac/` — Platform-scoped Role-Based Access Control (`admin`, `user`, `denied`).
- `internal/router/` — Hybrid router (`Fast-Path` commands vs `AI-Path` prompts) and declarative JSON commands.
- `internal/backend/` — Pluggable AI & ops backends (`agy-gateway`, `ollama`, `openai`, `mock`).
- `internal/channels/` — Channel adapters (`discord`, `telegram`, `whatsapp`, `mock`).
- `internal/format/` — Per-channel Markdown formatting and line-aware chunking.
- `internal/server/` — HTTP webhooks, `/api/v1/notify`, and dev simulator.

## How to Extend Aegys

1. **Add a WhatsApp Provider:** Implement `whatsapp.Provider` in `internal/channels/whatsapp/` and wire it in `NewProviderFromConfig`.
2. **Add an AI Backend:** Implement `backend.AIBackend` (and optionally `backend.OpsBackend`) in `internal/backend/`.
3. **Add a Custom Command:** Add entries to `config/commands.example.json` (or your local `AEGYS_COMMANDS_FILE`) without modifying Go code.

## Privacy & Security Rules

- **Never commit secrets or personal data:** No `.env` files, API tokens, real phone numbers, real Discord/Telegram user IDs, personal hostnames, or real LAN/MAC addresses.
- Use RFC 5737 documentation IPs (`192.0.2.x`), placeholder phone numbers (`5500999990000`), and dummy IDs (`111111111111111111`) in tests and documentation.
- CI runs `gitleaks` on every PR alongside `go vet` and `go test -race ./...`.

## Pull Request Workflow

1. Create a feature branch from `main` (`feat/...`, `fix/...`, `docs/...`).
2. Keep commit messages and PR descriptions in English.
3. Ensure `go vet ./...` and `go test -race ./...` pass locally before opening a PR.
