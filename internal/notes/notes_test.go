package notes

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSuggestCandidatesUsesContributionsAndHeuristics(t *testing.T) {
	t.Parallel()

	abstract := "We introduce a framework. The approach improves performance. Experiments achieve strong results."
	contribs := []string{
		"Contribution A",
		"Contribution B",
		"Contribution C",
	}

	got := SuggestCandidates("Great Paper", abstract, contribs)
	if len(got) < 4 {
		t.Fatalf("expected at least four suggestions, got %d", len(got))
	}

	if got[0].Title != "Contribution #1" || got[0].Body != "Contribution A" {
		t.Fatalf("expected first suggestion to be contribution, got %+v", got[0])
	}

	var hasProblem, hasMethod, hasResult bool
	for _, candidate := range got {
		switch candidate.Title {
		case "Problem Framing":
			hasProblem = true
		case "Method Snapshot":
			hasMethod = true
		case "Result Highlight":
			hasResult = true
		}
	}

	if !hasProblem || !hasMethod || !hasResult {
		t.Fatalf("expected heuristic suggestions, got %#v", got)
	}
}

func TestSuggestCandidatesFallsBackToOverview(t *testing.T) {
	t.Parallel()

	got := SuggestCandidates("Untitled", "", nil)
	if len(got) != 1 || got[0].Title != "Paper Overview" {
		t.Fatalf("expected fallback overview suggestion, got %#v", got)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "zettel.json")

	payload := []Note{
		{
			PaperID:    "1234",
			PaperTitle: "Sample",
			Title:      "Contribution #1",
			Body:       "We propose something.",
			Kind:       "contribution",
		},
	}

	if err := Save(path, payload); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(got) != 1 || got[0].Title != payload[0].Title || got[0].Body != payload[0].Body {
		t.Fatalf("unexpected notes payload: %#v", got)
	}
}

func TestSavePreservesConversationSnapshots(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "zettel.json")
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

	snapshot := ConversationSnapshot{
		PaperID:    "5678",
		PaperTitle: "Snapshot Paper",
		CapturedAt: now,
		Messages: []ConversationMessage{
			{Kind: "brief", Content: "Generated summary", Timestamp: now},
		},
		Notes: []SnapshotNote{
			{Title: "Quick Note", Body: "Remember this.", Kind: "manual", CreatedAt: now},
		},
		Brief: &BriefSnapshot{
			Summary: []string{"Summary bullet"},
		},
		SectionMetadata: []BriefSectionMetadata{
			{Kind: "summary", Status: "completed", DurationMs: 1200},
		},
		LLM: &LLMMetadata{Provider: "ollama", Model: "ministral-3:latest"},
	}

	if err := SaveConversationSnapshots(path, []ConversationSnapshot{snapshot}); err != nil {
		t.Fatalf("SaveConversationSnapshots() error = %v", err)
	}

	note := Note{
		PaperID:    "5678",
		PaperTitle: "Snapshot Paper",
		Title:      "Contribution #1",
		Body:       "Follow-up note.",
		Kind:       "contribution",
	}

	if err := Save(path, []Note{note}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	conversations, err := LoadConversationSnapshots(path)
	if err != nil {
		t.Fatalf("LoadConversationSnapshots() error = %v", err)
	}
	if len(conversations) != 1 || conversations[0].PaperID != "5678" {
		t.Fatalf("unexpected conversations payload: %#v", conversations)
	}

	notes, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(notes) != 1 || notes[0].Title != "Contribution #1" {
		t.Fatalf("unexpected notes payload: %#v", notes)
	}
}

func TestAppendConversationSnapshotAppendsUpdates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "zettel.json")
	now := time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC)

	update := SnapshotUpdate{
		Messages: []ConversationMessage{
			{Kind: "question", Content: "What is this about?", Timestamp: now},
		},
	}
	if err := AppendConversationSnapshot(path, "paper-1", "Title", update); err != nil {
		t.Fatalf("AppendConversationSnapshot() error = %v", err)
	}

	snapshots, err := LoadConversationSnapshots(path)
	if err != nil {
		t.Fatalf("LoadConversationSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 || len(snapshots[0].Messages) != 1 {
		t.Fatalf("unexpected snapshots payload: %#v", snapshots)
	}

	notesUpdate := SnapshotUpdate{
		Notes: []SnapshotNote{
			{Title: "Note", Body: "Manual note", Kind: "manual", CreatedAt: now},
		},
	}
	if err := AppendConversationSnapshot(path, "paper-1", "Title", notesUpdate); err != nil {
		t.Fatalf("AppendConversationSnapshot() error = %v", err)
	}

	snapshots, err = LoadConversationSnapshots(path)
	if err != nil {
		t.Fatalf("LoadConversationSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 || len(snapshots[0].Notes) != 1 {
		t.Fatalf("unexpected snapshots payload: %#v", snapshots)
	}
}

func TestAppendConversationSnapshotRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "zettel.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	update := SnapshotUpdate{
		Messages: []ConversationMessage{
			{Kind: "question", Content: "What is this about?", Timestamp: time.Now()},
		},
	}
	if err := AppendConversationSnapshot(path, "paper-1", "Title", update); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
