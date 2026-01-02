# Repository Guidelines

## Project Structure & Module Organization
PaperScout is a Go module. CLI entrypoints stay under `cmd/` (currently `cmd/paperscout/`), while all reusable packages live in `internal/` (`internal/arxiv`, `internal/guide`, `internal/notes`, `internal/tui`, etc.). Keep additional integrations in `internal/<domain>/` so they remain importable only inside this repo. Persisted state (e.g., `zettelkasten.json`) is user-provided and should not be committed; sample payloads or screenshots belong in `assets/` if needed. Use `docs/` for design write-ups and `scripts/` for helper binaries or shell utilities.

## Build, Test, and Development Commands
- `go mod tidy`: ensure dependencies (Bubble Tea, Lip Gloss, etc.) are synced.  
- `go run ./cmd/paperscout -zettel ~/notes/zettelkasten.json`: launch the TUI; pass `-no-alt-screen` during development if the alternate screen is inconvenient.  
- `go build ./cmd/paperscout`: produce a local binary.  
- `go test ./...`: execute all unit tests for `internal/arxiv`, `internal/notes`, and future packages.

## Coding Style & Naming Conventions
Follow idiomatic Go: run `gofmt`/`goimports` (or rely on your editor) before committing, keep exported identifiers in `PascalCase`, and limit CLI flags to `kebab-case`. Prefer table-driven tests and keep files <300 lines where possible by extracting helpers. When touching the TUI, ensure Bubble Tea updates remain pure (no side effects outside commands) and keep lipgloss styles centralized. Add short comments for non-obvious logic (e.g., heuristics in `internal/arxiv` parsing).

## Testing Guidelines
Each package under `internal/` should have a corresponding `_test.go` exercising both success and failure paths (e.g., parser errors, note persistence). Use stubbed HTTP clients or fixtures for arXiv interactions so tests pass offline. When bugs are fixed, add a regression test to the affected package. Aim for high coverage on parsing and note-saving code, and run `go test ./...` before opening a PR.

## Commit & Pull Request Guidelines
Adopt Conventional Commits (`feat:`, `fix:`, `chore:`) with < ~60 character subjects. PR descriptions must include: intent summary, validation steps (build/test commands run locally), linked issues (e.g., `Fixes #42`), and screenshots/logs for UX-visible updates to the TUI. Keep drafts open until lint/tests are green, then request review.
