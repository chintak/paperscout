# PaperScout PRD

## Mission
- Keep every research-reading action inside a single scrolling conversation so that paste-an-arXiv-link → read → ask → note can happen without swapping contexts, palettes, or log files.
- Preserve a reusable knowledge base (zettelkasten) that ties hero metadata, generated briefs, manual notes, questions, and answers to each paper for future resumes or exports.
- Build on the existing three-pass mentality (Summary, Technical, Deep Dive) while leaving room for streaming, durability, and portability into a different UI stack.

## Personas & Motivations
1. **Solo researcher** who wants to stay inside the terminal, load a paper once, keep the transcript in view, and ask follow-ups without losing their place.
2. **Curious note keeper** who captures manual notes, auto-suggested takeaways, and imported questions into a single JSON-backed knowledge base for later reference or cross-linking.
3. **Documentation/engineering lead** who needs a deterministic pipeline (arXiv fetch → PDF text → LLM prompt → zettelkasten snapshot) to reason about latency, retries, and reproducibility before rewriting the experience in another stack.

## Core product pillars

### 1. CLI + runtime wiring
- `cmd/paperscout` exposes a tiny launcher with flags for knowledge-base path (`-zettel`), screen-buffer handling (`-no-alt-screen`), and LLM overrides (`-llm-model`, `-llm-endpoint`).
- Defaults point at `zettelkasten.json` inside the working directory and `ministral-3:latest` served by Ollama at `http://localhost:11434`; environment variables (`OLLAMA_MODEL`, `OLLAMA_HOST`) can replace them.
- `tea.WithAltScreen()` is opt-in (default `-no-alt-screen`), so the UI stays in the normal buffer, preserving mouse + selection behavior.

### 2. Paper ingestion pipeline
- `internal/arxiv.FetchPaper` normalizes arXiv IDs/URLs, calls the arXiv API for metadata, derives `KeyContributions` heuristically from the abstract (keyword scoring + dedup), and computes subjects/authors.
- The same flow downloads the `View PDF` link, caches it under `~/.cache/paperscout/pdfs` (or `PAPERSCOUT_CACHE_DIR`), and extracts clean text via `ledongthuc/pdf`, returning a `Paper` struct with `FullText` used by every LLM job.
- Failed downloads surface clear errors and fall back to metadata-only behavior; repeated runs use cache TTL (24h) plus ETag/Range headers.

### 3. Conversation-first UI & navigation
- `internal/tui` renders a hero area (logo, title, arXiv ID, authors, subjects) and a single viewport that mixes transcript entries and the composer. The Composer is always at the bottom so it scrolls with the transcript, and input helpers live in the footer status line (Enter, Ctrl+Enter, Alt+Enter, Esc, Ctrl+C).
- Composer modes: URL loader (Alt+Enter), question (Enter), manual note (Ctrl+Enter), with shortcuts (`m` to start note, `q` to ask, `s` to save, `g/G` for jump). Mouse selection is supported without overlaying the viewport, and clipboard copy happens on mouse release.
- The transcript tracks typed commands (`fetch`, `note`, `question`), Scout messages (brief sections, questions, answers), errors, and saves. Each entry is timestamped for snapshot rehydration.

### 4. LLM-driven reading brief & QA
- The UI orchestrates the three-pass brief (Summary, Technical, Deep Dive) via `briefSectionJob`. Each section gets its own `jobKind`/`llm.BriefSectionKind` and updates state as streaming deltas arrive from `llm.Client.StreamBriefSection`.
- `internal/llm/prompt.go` builds dedicated prompts (markdown bullet structure, budgets, guardrails) and parsing helpers (JSON arrays, fallback bullet list parsing). Budgets are capped per section (`60k/110k/40k chars`) via `BriefSectionLimit`.
- `internal/brief/context.Builder` deduplicates/filters PDF paragraphs, splits them into chunks, ranks them (especially technical section), and clips them per budget to keep prompts within the desired window.
- Streaming allows the UI to show partial bullets, update status hints, and mark sections complete independently; per-section metadata (`notes.BriefSectionMetadata`) is persisted alongside conversation snapshots.
- Questions reuse the same `llm.Client` (default Ollama) via `Answer`, with context trimmed by keywords, and enqueue new jobs via the job bus; answers append to the transcript and the zettelkasten snapshot.

### 5. Note capture & knowledge base
- Manual notes are entered directly into the composer (Ctrl+Enter) and stored as `notes.Note` (paper ID/title, kind `manual`). `notes.SuggestCandidates` pre-populates heuristic snippets (contributions, problem framing, method, result) to highlight inside the UI.
- `notes.Save` appends entries to the knowledge base JSON array; `notes.AppendConversationSnapshot` records the running transcript, brief output, section metadata, and LLM-specific tags (`llm.Provider`, `Model`). Snapshots deduplicate per paper and merge updates.
- Zettelkasten entries look like `{paperId, paperTitle, title, body, kind, createdAt}`; conversation snapshots add `{entryType: conversation, messages, notes, brief, sectionMetadata, llm}` blocks so future sessions can hydrate `m.brief`, `m.transcriptEntries`, etc.

### 6. Job bus + state machine resiliency
- `internal/tui/jobBus` serializes job lifecycle snapshots (start, success, failure) and optionally logs telemetry (`PAPERSCOUT_DEBUG`). Each job runs on `context.Background()` with tailored timeouts (fetch 3m, LLM jobs 2m).
- Jobs include: fetch paper, save notes, ensure/append snapshot, brief sections, note suggestions, QA, and zettel persistence. The model handles spinner ticks, stage transitions (input → loading → display) and error messaging.
- Brief jobs can be canceled when a new paper loads (`briefStreamCancels`). The UI also queues question jobs until all sections finish, letting the first answer slide in automatically.

### 7. Observability, fallbacks, and helpers
- Without an LLM client or when PDF text is empty, the UI shows fallback messages (arXiv abstract/deep metadata) instead of blocking the composer; the `briefFallbacks` map seeds placeholder bullets until a real brief arrives.
- The status line always shows composer hints plus the last transcript event (`Last: Brief Summary ready`, etc.) so users know what just happened.
- Layout helpers (word wrapping, markdown stylization, hero/section anchors) keep the viewport tidy, allow `[`/`]` navigation between sections, and support `Esc` to clear, `Ctrl+C` to exit, `Ctrl+Enter` to note, `Alt+Enter` to load.

## Architecture + Tech notes for refactor
1. **LLM client abstraction (`internal/llm`)** – expose `Summarize`, `Answer`, `SuggestNotes`, `ReadingBrief`, `BriefSection`, `StreamBriefSection`. Default implementation calls Ollama’s `/api/generate` (with streaming) and enforces budgets; new backend should honor these method contracts and streaming deltas.
2. **Brief context builder (`internal/brief/context`)** – dedupes paragraphs (hash + filters) and emits per-section strings; future systems should reuse chunk IDs, ranking heuristics, and budgets so the same fairness/regulation holds.
3. **Knowledge base storage (`internal/notes`)** – single JSON array file with entries that either represent notes or `entryType: conversation` snapshots. Append-only writes lock the file; future stacks should follow the same shape to keep snapshots interoperable.
4. **Job orchestration (`internal/tui/jobBus`)** – each user intent spawns a job with structured telemetry; the new stack should expose similar start/success/failure hooks and support spinner/status updates.
5. **UI layout contract** – the hero/composer/transcript stack (with `LipGloss`) is currently single-column, scrollable in the normal buffer, and uses dedicated statuses. The refactor should preserve those UX affordances: composer commands at the bottom, normal scrollback, and `Last: …` status message.
6. **Persistence/resume flow** – loading a paper hydrates `brief`, `transcriptEntries`, `guide`, `manualNotes`, and `qaHistory` from snapshots. Any refactor needs to regenerate conversation snapshots in the same format (`messages`, `notes`, `brief`, per-section metadata) so the new stack continues to resume seamlessly.

## Edge cases to watch out for
- **ArXiv or PDF failures** – timeouts, missing abstracts, or corrupted PDFs should leave the hero up with a descriptive error, fall back to metadata-only placeholders, and avoid crashing the UI. Cache retries keep stale PDFs from blocking new fetches, but the pipeline should re-download when TTL expires.
- **LLM unavailable or streaming cutoff** – without a reachable Ollama daemon, brief/question/note suggestion flows must fail gracefully, keep fallback bullets, and keep `infoMessage` prompts like “Configure an LLM provider”. Streaming cancels should not leak goroutines (`briefStreamCancels` helps). Section-specific errors should only fail that section, not the entire brief.
- **Knowledge base path issues** – missing directories, permission errors, or corrupted JSON must be surfaced (error message + transcript entry) without losing manual note drafts. Saves should be atomic to prevent truncating the file.
- **Concurrent sessions/reloads** – repeated paper loads must cancel previously running jobs, reset brief state, and rehydrate older snapshots; the job bus must ignore stray messages whose `paperID` no longer matches the current paper.
- **Large PDFs and token budgets** – the builder must dedupe/re-rank to stay within each section’s budget, and prompts should gracefully clip if budgets tighten. The streaming pipeline should buffer until bullet boundaries to avoid jittery screen redraws.
- **Suggestion dedup and persistence** – heuristics might emit duplicates; `notes.SuggestCandidates` uses `Reason`/`Kind` to categorize, and `candidateMatchesNotes` prevents re-saving the same suggestion twice. A future stack must keep the same de-dup logic so `persisted` markers stay accurate.
- **LLM concurrency limits** – three simultaneous section requests can trip provider rate limits. The refactor should allow throttling (e.g., only `n` sections in flight) and display a retry path when providers return 429s.
- **Conversation snapshot growth** – the JSON array grows by appending snapshots and notes; a future refactor should still maintain the append-only pattern and handle large files (stream parse when rehydrating).

## Quality & validation
- Unit tests exist for arXiv cache/client, brief context builder, prompt parsing, and TUI helpers; `go test ./...` currently validates the entire suite.
- Docs (README, `docs/multi-section-plan.md`) describe the three-pass workflow, CLI usage, and the future plan (ps-hbk) for streaming sections; this should accompany any refactor to ensure telemetry and streaming semantics are preserved.
- When re-implementing, re-run `go test ./...` (or equivalent) and validate streaming logs/telemetry around `jobBus` snapshots and `notes.AppendConversationSnapshot` updates.

## Next steps for the rewrite
1. **Recreate the ingestion → chunk builder → streaming brief pipeline** so the new stack can adopt the budgets and streaming API described in `internal/brief` + `docs/multi-section-plan.md`.
2. **Mirror the composer/transcript job/state machine** (specialized stages, info line, `Last:` label) so the UX feels “single column” as in Bubble Tea while giving the future stack a clean contract for commands.
3. **Serialize notes + snapshots in the same JSON schema** to keep backward compatibility with existing `zettelkasten.json` files.
4. **Expose the same CLI knobs** (knowledge base path, LLM model/endpoint, no-alt-screen toggle) or equivalent configuration so deployment scripts carrying existing flags remain unchanged.
