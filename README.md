# PaperScout – Research Paper TUI

PaperScout is a Go-based TUI that streamlines reading arXiv papers: paste a paper URL, receive a three-pass reading brief inspired by S. Keshav's "How to Read a Paper", and capture manual notes into a lightweight zettelkasten (`zettelkasten.json`).

## Prerequisites
- Go 1.24+
- Network access for `go mod tidy` (downloads Charmbracelet Bubble Tea and friends).

## Quick Start
```bash
go mod tidy
go run ./cmd/paperscout -zettel ~/notes/zettelkasten.json
```
- Paste an arXiv URL or bare identifier and hit Enter.
- Regenerate the LLM reading brief with `a`, add manual notes with `m`, ask questions with `q`, and save with `s`.
- Hit `Ctrl+K` to open the command palette whenever you forget a shortcut—type to filter and press Enter to run the highlighted action.
- The layout is a single scrolling column—new fetches append session updates without switching views, so the current context stays visible while jobs run.
- Every paper renders an AI-powered reading brief with three passes (summary, technical details, deep-dive references); press `a` anytime to regenerate it from the parsed PDF.
- Interactions stream into a dedicated Session Log pane, and the fixed bottom composer lets you jot notes—press `Ctrl+Enter` to capture and `Esc` to cancel.
- Press `r` to load another paper or `Ctrl+C` to quit.

## LLM Summaries & Questions
PaperScout now downloads the linked "View PDF" asset, parses it locally, and can summarize it or answer follow-up questions through either OpenAI or Ollama. Ollama is the default, so once you have `ollama serve` running you can simply start PaperScout and hit `a`/`q` for summaries or questions.

- **OpenAI (hosted)** – set `OPENAI_API_KEY` (or pass `-openai-api-key`) and optionally `OPENAI_MODEL`/`OPENAI_BASE_URL`, then run `go run ./cmd/paperscout -llm-provider openai`.
- **Ollama (local)** – run `ollama serve` after pulling `ministral-3:latest`, which PaperScout now selects by default. Override with your own model via `-llm-provider ollama -llm-model <name>` (or `OLLAMA_MODEL`) plus `-llm-endpoint http://localhost:11434` if your daemon runs elsewhere. Ministral 3 exposes a 262K-token context window (per `ollama show`), which leaves plenty of headroom for long PDFs and follow-up questions without extra tuning.
- **Auto switch** – pass `-llm-provider auto` if you want PaperScout to prefer OpenAI whenever a key is present and otherwise fall back to Ollama.

You can still launch without any LLM; summaries and Q&A sections will show setup instructions instead of results.

## Testing
```bash
go test ./...
```
Runs table-driven unit tests for the `internal/arxiv` and `internal/notes` packages; the GitHub Actions workflow (`.github/workflows/go-tests.yml`) executes the same commands on every push/PR.

## Controls & Features
- **Three-pass brief** – Generates summary, technical, and deep-dive sections straight from the parsed PDF.
- **Full PDF ingestion** – Downloads the "View PDF" link, converts it to text locally, and uses that text for summaries and question answering.
- **LLM summaries & Q&A** – Hit `a` to re-run the summary and `q` to ask questions. PaperScout streams the relevant PDF context to OpenAI or Ollama and logs each answer in the transcript.
- **Subject context** – Shows arXiv subject tags alongside the title to prime your reading session.
- **Manual notes** – Write free-form notes without leaving the TUI; they'll be appended to the transcript and saved via the composer.
- **Persistent knowledge base** – Saved notes reappear when revisiting the same paper so you can extend or refactor them without duplication.
- **Command palette** – `Ctrl+K` exposes every action (summaries, Q&A, save, etc.) with fuzzy filtering so you can stay in flow without memorizing all shortcuts.
- **Job telemetry** – The session meter shows live job states/durations (fetching, saving, LLM tasks), and the job bus logs each completion with timing/error details for easy tracing.
- **Session log + composer** – The log captures paper loads, summaries, Q&A, and saves for later review, while the fixed multi-line composer at the bottom keeps note-taking front-and-center.

## Knowledge Base Format
`zettelkasten.json` is a JSON array. Note entries look like:
```json
{
  "paperId": "2101.00001",
  "paperTitle": "Awesome Research",
  "title": "Contribution #1",
  "body": "We introduce ...",
  "kind": "contribution",
  "createdAt": "2024-05-01T12:00:00Z"
}
```
Conversation snapshots are stored as additional entries with `entryType: "conversation"` and capture messages, notes, and LLM section metadata:
```json
{
  "entryType": "conversation",
  "paperId": "2101.00001",
  "paperTitle": "Awesome Research",
  "capturedAt": "2024-05-01T12:00:00Z",
  "messages": [{ "kind": "brief", "content": "...", "timestamp": "2024-05-01T12:01:00Z" }],
  "notes": [{ "title": "Note", "body": "...", "kind": "manual", "createdAt": "2024-05-01T12:02:00Z" }],
  "brief": { "summary": ["..."], "technical": ["..."], "deepDive": ["..."] },
  "sectionMetadata": [{ "kind": "summary", "status": "completed", "durationMs": 1200 }],
  "llm": { "provider": "ollama", "model": "ministral-3:latest" }
}
```
Use `jq` or your favorite database to query them later for ideation.
