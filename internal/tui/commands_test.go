package tui

import (
	"context"
	"testing"

	"github.com/csheth/browse/internal/arxiv"
	"github.com/csheth/browse/internal/llm"
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
