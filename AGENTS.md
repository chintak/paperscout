# PaperScout Agent Playbook

## Mission Priorities
1. Understand the user request and confirm the intended scope before writing code.
2. Ship minimal, high-quality changes that improve PaperScout without disturbing unrelated files.
3. Every code or doc change must end with passing tests plus clear reasoning about remaining risks.
4. Communicate follow-up steps (tests to run, branches to push, etc.) so the next agent has zero guesswork.

## Repository Map
- `cmd/paperscout`: CLI entrypoint; keep user-facing flag logic here.
- `internal/arxiv`, `internal/guide`, `internal/notes`, `internal/tui`, `internal/llm`: reusable packages. Add new domains under `internal/<domain>` to avoid leaking APIs.
- `assets/`, `docs/`, `scripts/`: sample payloads, design notes, and helper utilities (create as needed).
- Ignore user-owned artifacts such as `zettelkasten.json`; never commit them.

## Command Reference
- `go mod tidy` – sync dependencies (Bubble Tea, Lip Gloss, etc.) whenever modules change.
- `go run ./cmd/paperscout -zettel ~/notes/zettelkasten.json [-no-alt-screen]` – run the TUI locally.
- `go build ./cmd/paperscout` – compile the CLI for quick smoke testing.
- `go test ./...` – execute the full unit suite for `internal/*`.

## Standard Workflow for Any Task
1. **Plan** – Skim AGENTS.md and repo context, write a short plan when the task is non-trivial.
2. **Develop** – Work inside the appropriate package; keep Bubble Tea updates pure and isolate side effects in commands.
3. **Format & Lint** – Run `gofmt`/`goimports` on touched Go files; keep files under ~300 LOC by extracting helpers.
4. **Test** – Add/maintain `_test.go` coverage for each touched package before running `go test ./...`.
5. **Review before commit** – Ensure only your edits are staged, summarize risks, and call out any remaining manual steps.

## Coding Standards
- Use idiomatic Go naming: exported identifiers in PascalCase; CLI flags in kebab-case.
- Centralize Lip Gloss styles and Bubble Tea view helpers to avoid redefinition.
- Add concise comments for heuristics or non-obvious parsing logic (e.g., arXiv ID normalization).
- Prefer table-driven tests and small helpers to avoid deeply nested conditionals.

## Testing Requirements
- Each code change must include targeted tests covering success and failure paths of the affected module.
- When stubbing external services (arXiv, LLMs), keep fixtures local so tests pass offline.
- For every new test, add a short comment stating the precise edge case (e.g., `// ensures blank titles fail validation`).
- Regression tests are mandatory when fixing bugs; document the bug scenario in the test name.
- Run `go test ./...` before finishing; note any failures that require elevated permissions or follow-up.

## Branching, Commits, and PRs
- Create feature branches as `<author_initials>/<conventional_commit_type>/<three_four_word_desc>` (e.g., `cs/feat/support-pdf-parse`).
- Follow the full Conventional Commits spec for every commit message; keep subjects under ~60 characters.
- Expect multiple agents working in the same repo; stage/commit only the files you changed in this session and leave pre-existing dirty files alone unless the user explicitly instructs otherwise.
- Always include the tests you added/updated in the same commit as the implementation; focus on quality, not volume.
- Commit only when a user request or task is complete (or they explicitly ask for a checkpoint); avoid committing after every small interaction or trivial edit.
- PR descriptions must cover intent, validation steps (commands run), linked issues, and screenshots/logs for TUI-visible changes.
