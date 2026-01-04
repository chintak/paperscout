# PaperScout Agent Playbook (Beads-Driven)

PaperScout runs entirely on beads. Treat `.beads/issues.jsonl` as the canonical queue, and keep every repo change backed by a bead event.

## Mission Priorities
1. Track implementation work (anything that touches code or repo artifacts) exclusively through beads: create or locate the right bead, keep its status updated as you work, and close it only after tests pass.
2. Requests limited to brainstorming, ideation, planning, or trade-off discussions stay outside the bead queue; treat them as conversational context until they produce real work.
3. When a discussion yields an executable ask, treat it as work: spin up dedicated bead(s), mark blockers/dependencies, add `discovered-from` links if it emerged mid-bead, and only move it forward when it is ready to execute.
4. Ship minimal, high-quality deltas that improve PaperScout without disturbing unrelated files or beads.
5. Maintain a clean reasoning trail: plans live in bead comments, validation steps live in commit messages, and risks are called out before handoff.
6. Communicate explicit next steps (remaining beads, follow-up tests, review asks) so another agent can resume instantly.

## Repository Map
- `cmd/paperscout`: CLI entrypoint; user-facing flags stay here.
- `internal/arxiv`, `internal/guide`, `internal/notes`, `internal/tui`, `internal/llm`: shared packages; add new domains as `internal/<domain>` to avoid leaking APIs.
- `assets/`, `docs/`, `scripts/`: fixtures, design notes, helper utilities—extend as needed.
- Ignore user-owned artifacts such as `zettelkasten.json`; never commit them.

## Beads-First Operating System

### Single Source of Truth
- Never track work in markdown TODOs or ad-hoc docs. If it matters, create or update a bead.
- `bd ready --json` lists unblocked beads sorted by priority; always start here before picking a task.
- Auto-sync keeps `.beads/issues.jsonl` current. Commit it with your code so the queue mirrors the repo state.

### Working a Bead
1. **Claim** – `bd update bd-### --status in_progress --assignee "$(whoami)" --json`
2. **Plan** – Summarize intent + acceptance criteria via `bd comments add <id> "<note>"`; note dependencies using `--deps blocks:bd-###` when needed.
3. **Deliver** – Build the change inside the relevant package, referencing bead context in code comments only when necessary.
4. **Validate** – Run unit tests and record the exact commands plus results using `bd comments add <id> "<note>"`.
5. **Close** – `bd close bd-### --reason "<short summary of changes/decisions>" --json` once code + tests + docs are merged. The reason must capture the work performed or rationale applied—never use generic text such as “Completed”.

### Creating and Linking Beads
- Use `bd create "Concise title" -t bug|feature|task|epic|chore -p 0-4 --json`.
- Link discovered work with `--deps discovered-from:bd-parent` and blockers with `--deps blocks:bd-blocker`.
- Keep bead descriptions action-oriented: context, definition of done, and observable impact.

### Status and Priority Best Practices
- Status flow: `backlog → ready → in_progress → blocked (optional) → review → done`.
- Promote beads to `ready` only when scope + acceptance criteria are clear.
- Priority semantics:
  - `0` Critical: security/data loss/broken builds.
  - `1` High: urgent features or major bug fixes.
  - `2` Medium: default work queue.
  - `3` Low: polish and optimizations.
  - `4` Backlog / speculative ideas.

### Observability
- Use `bd log bd-###` (or `bd history`) to audit changes; keep updates small and descriptive.
- When pairing work needs asynchronous handoff, drop your latest context in a bead comment plus a `blocked` or `review` status update.
- Reference bead IDs in commit subjects (`feat: improve parsing (bd-123)`) so git history mirrors the bead ledger.

## Standard Task Loop
1. **Plan** – Skim this playbook, the README (for any updated guidance or setup instructions), plus the bead you are working on; write a short plan for non-trivial work.
2. **Develop** – Implement inside the correct package; keep Bubble Tea updates pure and isolate side effects inside commands.
3. **Format & Lint** – Run `gofmt`/`goimports` on touched Go files; keep files under ~300 LOC by extracting helpers.
4. **Test** – Maintain `_test.go` coverage for each touched package, then run `go test ./...`.
5. **Record** – Post test output + remaining risks to the bead, then close or move to `review`.

## Command Reference
- `go mod tidy` – sync dependencies whenever modules change.
- `go run ./cmd/paperscout -zettel ~/notes/zettelkasten.json [-no-alt-screen]` – run the TUI locally.
- `go build ./cmd/paperscout` – compile the CLI for smoke tests.
- `go test ./...` – execute the full unit suite for `internal/*`.
- `bd ready --json`, `bd show bd-### --json`, `bd update ...`, `bd close ...` – daily bead hygiene commands.
- `ollama show <model_name>` – inspect the active LLM’s capabilities (context length, quantization, default sampling params) before shaping prompts or trimming paper context.

### LLM Context Guidelines
- Before prompting the Summary/Technical/Deep Dive sections, run `ollama show $OLLAMA_MODEL` (default `qwen3-vl:8b`, 262 144 token context) to confirm the actual context length and sampling defaults on the workstation.
- Keep our `max*Chars` guards in `internal/llm/llm.go` aligned with the discovered context window so prompts stay comfortably below the limit (leave at least 20% headroom for instructions/system text).
- If switching models, document the new context length in a bead comment and adjust any clipping logic/tests in the same change so future agents inherit the right budgets.

## Coding Standards
- Follow idiomatic Go naming: exported identifiers in PascalCase; CLI flags in kebab-case.
- Centralize Lip Gloss styles and Bubble Tea view helpers to avoid duplication.
- Add concise comments for heuristics or non-obvious parsing logic (e.g., arXiv ID normalization).
- Favor table-driven tests and small helpers over deeply nested conditionals.

## Testing Requirements
- Each code change must include targeted tests that cover both success and failure paths.
- Keep fixtures local so tests pass offline; stub external services (arXiv, LLMs) with deterministic data.
- Document regressions via test names and short comments (e.g., `// ensures blank titles fail validation`).
- Run `go test ./...` before finishing and note any failures or flakiness in the bead.

## Branching, Commits, and PRs
- Create branches as `<author_initials>/<conventional_commit_type>/<three_four_word_desc>` (e.g., `cs/feat/support-pdf-parse`).
- Write Conventional Commit messages and include the bead ID in the subject when possible.
- Stage and commit only the files you touched (code + `.beads/issues.jsonl`); leave pre-existing dirty files intact unless instructed.
- Include new/updated tests in the same commit as the implementation.
- PR descriptions must link the bead, state validation commands, and attach screenshots/logs for TUI-visible changes.

## Handoff Checklist
- Bead status reflects reality (`review`, `blocked`, or `done`).
- `.beads/issues.jsonl` is committed with the code changes.
- Latest bead comment includes: summary, validation commands, risks, and next steps/tests if outstanding.

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
