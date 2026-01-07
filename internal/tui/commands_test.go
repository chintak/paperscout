package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/csheth/browse/internal/arxiv"
	"github.com/csheth/browse/internal/llm"
	"github.com/csheth/browse/internal/notes"
)

type fakeLLM struct{}

func (fakeLLM) Summarize(ctx context.Context, title, content string) (string, error) { return "", nil }
func (fakeLLM) Answer(ctx context.Context, title, question, content string) (string, error) {
	return "", nil
}
func (fakeLLM) SuggestNotes(ctx context.Context, title, abstract string, contributions []string, content string) ([]llm.SuggestedNote, error) {
	return nil, nil
}
func (fakeLLM) ReadingBrief(ctx context.Context, title, content string) (llm.ReadingBrief, error) {
	return llm.ReadingBrief{}, nil
}
func (fakeLLM) BriefSection(ctx context.Context, kind llm.BriefSectionKind, title, content string) ([]string, error) {
	return nil, nil
}
func (fakeLLM) StreamBriefSection(ctx context.Context, kind llm.BriefSectionKind, title, content string, handler llm.BriefSectionStreamHandler) error {
	return handler(llm.BriefSectionDelta{Kind: kind, Bullets: []string{"bullet"}, Done: true})
}
func (fakeLLM) Name() string { return "fake" }

func newTestModel(t *testing.T) *model {
	t.Helper()
	teaModel, ok := New(Config{}).(*model)
	if !ok {
		t.Fatalf("expected *model, got %T", teaModel)
	}
	return teaModel
}

func TestCommandAvailability(t *testing.T) {
	m := newTestModel(t)
	if m.commandAvailable(actionManualNote) {
		t.Fatal("manual note should be disabled without a paper")
	}

	m.paper = &arxiv.Paper{ID: "1234", Title: "Test"}
	if !m.commandAvailable(actionManualNote) {
		t.Fatal("manual note should be enabled when a paper is loaded")
	}
	if m.commandAvailable(actionSummarize) {
		t.Fatal("summarize requires LLM client")
	}

	m.config.LLM = fakeLLM{}
	if !m.commandAvailable(actionSummarize) {
		t.Fatal("summarize should be enabled with LLM + paper")
	}

	if m.commandAvailable(actionSaveNotes) {
		t.Fatal("save should require selected notes")
	}
}

func TestEnsureConversationSnapshotJobCreatesEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zettel.json")
	paper := &arxiv.Paper{ID: "1234", Title: "Snapshot"}

	runner := ensureConversationSnapshotJob(path, paper)
	if _, err := runner(context.Background()); err != nil {
		t.Fatalf("ensureConversationSnapshotJob() error = %v", err)
	}

	snapshots, err := notes.LoadConversationSnapshots(path)
	if err != nil {
		t.Fatalf("LoadConversationSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].PaperID != "1234" {
		t.Fatalf("unexpected snapshots payload: %#v", snapshots)
	}

	if _, err := runner(context.Background()); err != nil {
		t.Fatalf("ensureConversationSnapshotJob() error = %v", err)
	}
	snapshots, err = notes.LoadConversationSnapshots(path)
	if err != nil {
		t.Fatalf("LoadConversationSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected single snapshot entry, got %d", len(snapshots))
	}
}

func TestEnsureConversationSnapshotJobRejectsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zettel.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	paper := &arxiv.Paper{ID: "1234", Title: "Snapshot"}
	runner := ensureConversationSnapshotJob(path, paper)
	if _, err := runner(context.Background()); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestAppendConversationSnapshotJobPersistsBriefUpdates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zettel.json")
	paper := &arxiv.Paper{ID: "1234", Title: "Snapshot Paper"}
	update := notes.SnapshotUpdate{
		Brief: &notes.BriefSnapshot{
			Summary:   []string{"summary"},
			Technical: []string{"technical"},
			DeepDive:  []string{"deep"},
		},
		SectionMetadata: []notes.BriefSectionMetadata{
			{Kind: "summary", Status: "completed"},
		},
	}

	runner := appendConversationSnapshotJob(path, paper, update)
	if _, err := runner(context.Background()); err != nil {
		t.Fatalf("appendConversationSnapshotJob() error = %v", err)
	}

	snapshots, err := notes.LoadConversationSnapshots(path)
	if err != nil {
		t.Fatalf("LoadConversationSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Brief == nil || len(snapshots[0].Brief.Summary) != 1 {
		t.Fatalf("expected brief summary to be persisted: %#v", snapshots[0].Brief)
	}
	if len(snapshots[0].SectionMetadata) != 1 {
		t.Fatalf("expected section metadata persisted: %#v", snapshots[0].SectionMetadata)
	}
}
