# PaperScout – Research Paper TUI

PaperScout is a Go-based TUI that streamlines reading arXiv papers: paste a paper URL, see auto-generated contribution summaries, follow a structured guide inspired by S. Keshav's "How to Read a Paper", and capture suggested or manual notes into a lightweight zettelkasten (`zettelkasten.json`).

## Prerequisites
- Go 1.24+
- Network access for `go mod tidy` (downloads Charmbracelet Bubble Tea and friends).

## Quick Start
```bash
go mod tidy
go run ./cmd/paperscout -zettel ~/notes/zettelkasten.json
```
- Paste an arXiv URL or bare identifier and hit Enter.
- Toggle suggested notes with `space`, add manual notes with `m`, regenerate the LLM summary with `a`, ask questions with `q`, search within the current view with `/`, and save with `s`.
- Press `r` to load another paper or `ctrl+c` to quit.

## LLM Summaries & Questions
PaperScout now downloads the linked "View PDF" asset, parses it locally, and can summarize it or answer follow-up questions through either OpenAI or Ollama. Ollama is the default, so once you have `ollama serve` running you can simply start PaperScout and hit `a`/`q` for summaries or questions.

- **OpenAI (hosted)** – set `OPENAI_API_KEY` (or pass `-openai-api-key`) and optionally `OPENAI_MODEL`/`OPENAI_BASE_URL`, then run `go run ./cmd/paperscout -llm-provider openai`.
- **Ollama (local)** – run `ollama serve` with your preferred model, or override with `-llm-provider ollama -llm-model mistral` and `-llm-endpoint http://localhost:11434`.
- **Auto switch** – pass `-llm-provider auto` if you want PaperScout to prefer OpenAI whenever a key is present and otherwise fall back to Ollama.

You can still launch without any LLM; summaries and Q&A sections will show setup instructions instead of results.

## Testing
```bash
go test ./...
```
Runs table-driven unit tests for the `internal/arxiv` and `internal/notes` packages; the GitHub Actions workflow (`.github/workflows/go-tests.yml`) executes the same commands on every push/PR.

## Controls & Features
- **Automatic summary** – Fetches title, authors, abstract, extracts up to four key contribution sentences, and now parses the PDF to power richer LLM summaries.
- **Full PDF ingestion** – Downloads the "View PDF" link, converts it to text locally, and uses that text for summaries, search, and question answering.
- **LLM summaries & Q&A** – Hit `a` to re-run the summary and `q` to ask questions. PaperScout streams the relevant PDF context to OpenAI or Ollama and logs each answer in the transcript.
- **Reading plan** – Displays a five-step guide following the three-pass method plus capture prompts.
- **Note suggestions** – Highlights problem/method/result heuristics for quick capture; toggle them for saving.
- **Searchable viewport** – Scroll suggestions and guides like a pager (mouse wheel, PgUp/PgDn, or viewport keys) and jump between highlighted matches with `/`, `n`, and `N`.
- **Subject context** – Shows arXiv subject tags alongside the title to prime your reading session.
- **Manual notes** – Write free-form notes without leaving the TUI; they'll be bundled with selected suggestions.
- **Persistent knowledge base** – Saved notes reappear when revisiting the same paper so you can extend or refactor them without duplication.

## Knowledge Base Format
`zettelkasten.json` contains entries like:
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
Use `jq` or your favorite database to query them later for ideation.
