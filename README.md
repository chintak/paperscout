# PaperScout – Research Paper TUI

PaperScout is a Go-based terminal UI that keeps research sessions entirely within a single, scrollable conversation. Paste an arXiv URL, fetch the paper, and the hero art, metadata, and reading brief stream into the same transcript you use to ask questions or jot notes. Everything that would traditionally live in a command palette or session log now lives in that conversation: the composer is the final message, hints follow the transcript, and scroll + selection behave exactly like a normal terminal because we never hijack the viewport.

## Prerequisites
- Go 1.24+
- (Optional) `ollama` with `ministral-3:latest` downloaded so the built-in LLM flow can run locally.

## Quick Start
```bash
go mod tidy
go run ./cmd/paperscout -zettel ~/notes/zettelkasten.json
```
- Paste an arXiv URL or bare identifier into the composer and press Alt+Enter to fetch metadata, load the paper, and trigger the three-pass reading brief.
- Once a paper is loaded, you stay in a single scrolling column: the hero art and intro live at the top, the transcript grows in the middle, and the composer renders as the latest `Command` message that scrolls with everything else.
- There is no command palette—the composer is always focused, and helper hints appear inline and in the status line (Enter, Alt+Enter, Ctrl+Enter, Esc, Ctrl+C) rather than in an overlay that steals focus.
- The footer status line now expands to the full viewport width in light gray, and it only shows the composer shortcuts plus the most recent transcript event (e.g., “Last: Paper loaded”) so you always see why the session moved.
- PaperScout keeps you in the normal screen buffer by default (`-no-alt-screen` defaults to `true`), letting your terminal’s scrollback, mouse wheel, and text selection behave exactly as usual; pass `-no-alt-screen=false` if you really need the alternate buffer.
- `Ctrl+C` quits, `Esc` clears the composer, `Ctrl+Enter` captures a note, `Enter` sends your question (once the paper & brief are ready), and Alt+Enter always means “load this URL.”

## Interaction & Layout
Every message in the conversation—including the composer—shares the same layout. There is no dedicated session log or telemetry strip anymore; the only persistent footer is the status line that mirrors the composer hints and the last transcript event. Mouse-driven scrolling and selection work across the entire viewport because Bubble Tea’s viewport hijacking is disabled and we render in the normal buffer. The composer always sits at the end of the transcript, so your latest command scrolls into history like any other message.

## Controls & Workflow
- **Load a paper** – Paste an arXiv URL and press Alt+Enter. The composer briefly shows “Fetching metadata…” while the hero summary loads above, and the reading brief runs automatically once the PDF text is available.
- **Ask questions** – Type a query and press Enter; the composer switches to question mode and dispatches the request once the current reading brief is complete. Answers stream back into the transcript as Scout entries, and the conversation snapshot captures the question/answer pair for future resumes.
- **Capture manual notes** – Start typing while the composer is in note mode (it defaults to that after a load) and hit Ctrl+Enter. Notes go straight into the transcript and are appended to the zettelkasten conversation snapshot immediately.
- **Composer shortcuts** – Alt+Enter loads URLs, Enter sends questions, Ctrl+Enter stores manual notes, Esc clears the composer, and Ctrl+C quits. Because everything lives in the conversation, there is no separate palette or cheat sheet to toggle.
- **Status line** – Light gray text spans the full width, shows the same helper text as the composer, and appends a “Last: …” event so you can tell whether the most recent job was a fetch, brief, note, or answer without resurrecting a separate log.
- **Scrolling & selection** – The entire transcript—including the command block—scrolls together; mouse wheel + drag selection work because we respect the terminal scrollback buffer.

## LLM Summaries & Questions
PaperScout downloads the linked “View PDF” asset, parses it locally, and streams the text into Ollama so you can ask the three-pass reading brief (summary, technical, deep dive) or follow-up questions. Start `ollama serve` and pull `ministral-3:latest`; PaperScout already defaults to that model, so you only need to point to the daemon via `-llm-endpoint` if you run it somewhere other than `http://localhost:11434`. Use `-llm-model` (or the `OLLAMA_MODEL` env var) to override the model if needed, and run `ollama show <model>` before starting PaperScout to confirm the advertised 262K‑token context window.

If no LLM is configured or the PDF text is missing, Scout still loads the hero + transcript and leaves informative placeholders in the conversation rather than blocking the UI.

## Testing
```bash
go test ./...
```
Runs the Go unit tests for the CLI and all supporting packages; GitHub Actions runs the same command on every push/PR.

## Controls & Features
- **Three-pass brief** – Summary, technical details, and deep dive sections are generated automatically, saved as Scout entries, and updated in place as each LLM response completes.
- **Full PDF ingestion** – The “View PDF” link is downloaded, converted to text locally, and that text is what feeds the reading brief and question-answer jobs.
- **LLM Q&A** – Questions enter the transcript while brief sections are streaming; answers stream back in the same conversation and update the zettelkasten snapshot as soon as they finish.
- **Subject metadata** – The hero renders the paper title plus a short list of authors and subjects to set context before the transcript grows.
- **Manual notes** – Notes are typed directly into the composer and stored both inline and inside the zettelkasten snapshot as soon as you press Ctrl+Enter, eliminating extra dialogs or palettes.
- **Persistent knowledge base** – `zettelkasten.json` (or whatever you pass to `-zettel`) keeps every note, LLM section, question, and answer linked to the paper so you can resume where you left off.
- **Scout timeline** – Each summary/technical/deep-dive completion writes a `Scout (brief)` message (with `kind` values `brief_summary`, `brief_technical`, `brief_deep_dive`) plus section metadata, so reloading the same paper rebuilds the full brief + QA history exactly as you last saw it.
- **Inline status hints** – The footer is now a light-gray stub that stretches the viewport width and only repeats the composer shortcuts plus the “Last: …” transcript event; the old job telemetry log has been removed.
- **Mouse + scroll friendly** – Because Bubble Tea no longer hijacks the viewport, you can scroll through the entire conversation (including the composer) with the wheel and select/copy text as you would in any other terminal.

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
Conversation snapshots are stored as additional entries with `entryType: "conversation"` so the transcript, manual notes, and Scout messages can be rehydrated later:
```json
{
  "entryType": "conversation",
  "paperId": "2101.00001",
  "paperTitle": "Awesome Research",
  "capturedAt": "2024-05-01T12:00:00Z",
  "messages": [
    { "kind": "brief_summary", "content": "...", "timestamp": "2024-05-01T12:01:00Z" },
    { "kind": "brief_technical", "content": "...", "timestamp": "2024-05-01T12:02:00Z" },
    { "kind": "brief_deep_dive", "content": "...", "timestamp": "2024-05-01T12:03:00Z" },
    { "kind": "question", "content": "What is the method?", "timestamp": "2024-05-01T12:05:10Z" },
    { "kind": "answer", "content": "We use contrastive learning.", "timestamp": "2024-05-01T12:05:45Z" }
  ],
  "notes": [{ "title": "Note", "body": "...", "kind": "manual", "createdAt": "2024-05-01T12:02:00Z" }],
  "brief": { "summary": ["..."], "technical": ["..."], "deepDive": ["..."] },
  "sectionMetadata": [{ "kind": "summary", "status": "completed", "durationMs": 1200 }],
  "llm": { "provider": "ollama", "model": "ministral-3:latest" }
}
```
Entries whose `kind` is `brief_summary`, `brief_technical`, or `brief_deep_dive` record each completed section’s bullet output, and the accompanying metadata tracks duration + status. Because these Scout messages are written the moment a section finishes, reloading that paper rebuilds the entire Scout timeline (brief output, QA answers, and manual notes) exactly as you last left it.

Use `jq` or your favorite database to query them later for ideation.
