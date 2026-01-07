# Faster Multi-Section Brief Generation (ps-hbk)

PaperScout currently issues a single, blocking LLM call for the entire three-pass brief and does not surface intermediate output. This document captures the current flow, profiling signals, and the plan for overlapping section generations while keeping total token usage predictable.

## Baseline & Profiling Notes

- `handlePaperResult` (`internal/tui/model.go`) immediately triggers `jobBus.Start(jobKindSummary, summarizePaperJob(...))`, resetting `m.brief` and setting `m.briefLoading = true`. No other jobs run until this completes.
- `summarizePaperJob` (`internal/tui/commands.go`) performs exactly one `client.ReadingBrief(ctx, title, content)` call with a 2-minute timeout. `llm.ollamaClient` clips the entire PDF to `maxBriefChars` (200k characters) and explicitly disables streaming (`stream:false` in `ollama_client.go`), so the call only returns once the backend has produced the full message.
- `handleBriefResult` replaces the entire `m.brief` struct and transcript entry in one step—there is no incremental section update message.
- Because `jobKindSummary` is the only job in flight, every spinner frame and job log entry between spinner start and `briefResultMsg` is attributable to the monolithic `ReadingBrief` request. Fetching/parsing already finished earlier in the pipeline, so we know the blocking path consists of:
  1. Clipping + prompt construction in-process (< 1s in benchmarks because it is mostly string slicing).
  2. The outbound HTTP call to the LLM, which is where users spend 40–90s today on 20+ page PDFs (per prior session logs where `[jobs] summary succeeded` durations matched the entire wait time).
- Failures are all-or-nothing; a single JSON parsing hiccup discards every section.

## Target Experience

1. Summary, Technical, and Deep Dive prompts start together, reuse the same deduplicated context, and stream tokens so each section can render independently.
2. The TUI footer/log shows progress per section (queued → streaming → done) so users know which parts remain.
3. Total latency drops because long-running sections no longer block shorter ones, and cached context avoids rebuilding prompt payloads.
4. Accuracy remains equal or better than today; deduped context keeps prompts within the same 200k-char guardrails, and retries are scoped to the affected section only.

## Proposed Architecture

### 1. Shared context packaging (feeds ps-3jt)

- Introduce `internal/brief/context` responsible for:
  - Splitting `paper.FullText` into semantic chunks (e.g., ~2k characters) with stable IDs (hash of normalized text + offsets).
  - Tracking overlaps across sections and deduplicating repeated boilerplate (front matter, references).
  - Enforcing section-specific budgets (`summary` gets 60k chars, `technical` 100k, `deepDive` 40k by default) while allowing global overrides tied to the detected model context window (`internal/llm/llm.go` guards stay the source of truth).
- The context manager exports both concatenated strings (for current synchronous code paths) and chunk metadata so future caching layers (e.g., PDF text cache, embeddings) can reuse them.

### 2. Multi-section generator interface

- Extend `llm.Client` with a new optional interface:

  ```go
  type BriefSectionKind string
  const (
      BriefSummary BriefSectionKind = "summary"
      BriefTechnical BriefSectionKind = "technical"
      BriefDeepDive BriefSectionKind = "deepDive"
  )

  type BriefSectionRequest struct {
      Kind    BriefSectionKind
      Title   string
      Context string
  }

  type BriefSectionDelta struct {
      Kind     BriefSectionKind
      Content  []string // aggregated bullets so far
      Done     bool
      Err      error
      Metadata map[string]any // timing, token counts
  }
  ```

- Add a `GenerateBriefSections(ctx context.Context, reqs []BriefSectionRequest) (<-chan BriefSectionDelta, error)` helper in `internal/llm/brief.go` that:
  - Accepts multiple section requests, starts one goroutine per section (bounded by provider limits), and forwards deltas back on a merged channel.
  - Wraps existing `llm.Client` implementations so the remainder of the codebase only needs to depend on this helper; initial implementation can still call `ReadingBrief` behind the scenes when streaming isn't yet available.
- For Ollama, set `"stream": true` and parse each JSON line from `/api/generate`. Emit a delta when `response` accumulates a newline or "DONE" and forward it via the merged `BriefSectionDelta` channel once the update is ready.

### 3. Parallel command execution in the TUI

- Update `jobBus` so a single user intent (pressing `a`) can spawn multiple logical jobs without losing telemetry. Two approaches:
  1. Teach `jobBus.Start` to accept a slice of `jobRunner`s and fan them in/out, or
  2. Keep `jobBus` as-is but introduce a higher-level `briefOrchestrator` that launches three jobs with new kinds (`brief-summary`, `brief-technical`, `brief-deepdive`).
- Add a new message type (`briefSectionMsg`) carrying `BriefSectionDelta`. `handleBriefResult` becomes a thin wrapper that tracks per-section arrays and marks sections complete when their `Done` delta arrives.
- The view layer uses `m.briefPartial[anchor]` to show streaming text and update anchors so keyboard shortcuts remain functional even before all sections finish.

### 4. Streaming UI affordances

- Composer/status footer shows `Summary ▸ streaming (45s)` etc. The session log appends events when each section starts/finishes/errors.
- Provide a clear fallback message if a section errors (e.g., "Technical section failed, press a to retry just this section"). Retrying only the failed section should reuse cached context and existing finished bullets.
- When all sections complete, coalesce them into the existing `llm.ReadingBrief` struct for downstream compatibility (notes, transcripts, persistence).

### 5. Telemetry, retries, and caching

- Record timestamps for `queued`, `first_token`, and `completed` per section and emit them as `jobSnapshot.Metadata`. This satisfies the "profile" requirement for future runs and lets us prove the latency win.
- Persist the chunk hash + rendered section text in a small on-disk cache (LRU under `~/.cache/paperscout/briefs/<paperID>`). If the same paper is re-summarized with identical context, reuse cached output immediately while background jobs refresh stale sections.
- Add exponential backoff retries per section for transient LLM or network issues; only the impacted section reruns.

## Implementation Phases

1. **Instrumentation & guard rails**
   - Add telemetry structs for section timings and expose them via the job log + transcript.
   - Keep the existing `ReadingBrief` path as a fallback flag (`-brief-mode monolithic|parallel`) so we can compare behaviors.
2. **Context manager (ties to ps-3jt)**
   - Build/context manager, update unit tests to cover chunking, deduping, and budgeting.
3. **Parallel generator**
   - Implement the helper + streaming wrappers. Provide fake clients for tests so `go test ./internal/llm` can simulate streaming and errors without hitting real APIs.
4. **TUI integration + UX polish**
   - Wire new message types, update the footer/log, and ensure focus handling works while sections stream.
5. **Caching & retries**
   - Persist outputs keyed by `paperID + section kind + chunk hash`.
   - Surface retry controls (press `a` to rerun everything, or a new shortcut to rerun a single failed section).

## Acceptance Criteria for ps-8u5

- Pressing `a` launches all three sections concurrently; the UI indicates their independent statuses.
- At least one section renders visible content before the slowest section finishes on a representative 20-page PDF (demonstrated via telemetry logs and/or screenshot).
- Section retries do not wipe completed sections; they reuse cached data.
- All existing tests still pass, and new unit tests cover the context manager, streaming parser, and TUI state transitions.
- CLI flag/docs describe the new behavior and allow opting back into the monolithic path temporarily.

## Risks & Trade-offs

- **LLM concurrency limits** – Running three prompts simultaneously can trip provider rate limits. Mitigation: allow configurable concurrency (1–3) and degrade gracefully by queueing sections when a 429 is returned.
- **Streaming parser coupling** – Ollama's `/api/generate` stream uses newline-delimited JSON. We keep the parser inside `internal/llm` and expose a stable `BriefSectionDelta` channel so the UI need not depend on transport details while keeping the parser well-tested.
- **Token budget regression** – Splitting prompts may increase total tokens. The context manager must dedupe shared chunks and enforce per-section caps to stay within the existing 262k-token envelope.
- **UI churn** – Frequent updates could cause viewport jumps. Buffer streaming text until we have a complete bullet (line ending or numbered prefix) before refreshing the viewport.

## Follow-ups

- `ps-3jt` owns the detailed context-packaging heuristics referenced here; once that bead lands we can plug it into the section generator.
- `ps-87n` ("Stream brief sections as they complete") will use the streaming UI infrastructure described above.
- Create a `docs/perf-playbook.md` entry that explains how to capture telemetry logs and compare monolithic vs parallel latency so future agents can validate improvements quickly.
