package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/csheth/browse/internal/arxiv"
	"github.com/csheth/browse/internal/llm"
	"github.com/csheth/browse/internal/notes"
)

func TestComposerEscInURLModeKeepsFocus(t *testing.T) {
	m := newTestModel(t)
	if !m.composer.Focused() {
		t.Fatalf("composer should start focused in URL mode")
	}
	m.composer.SetValue("https://arxiv.org/abs/1234.5678")

	if cmd, handled := m.processComposerKey(tea.KeyMsg{Type: tea.KeyEsc}); !handled {
		t.Fatalf("esc should be handled while composer focused")
	} else if cmd != nil {
		t.Fatalf("esc should not trigger a command, got %T", cmd)
	}

	if got := strings.TrimSpace(m.composer.Value()); got != "" {
		t.Fatalf("composer value not cleared, got %q", got)
	}
	if !m.composer.Focused() {
		t.Fatal("composer should remain focused after clearing URL mode")
	}
	if m.composerMode != composerModeURL {
		t.Fatalf("composer mode changed, got %v want %v", m.composerMode, composerModeURL)
	}
}

func TestComposerEscCancelsNoteMode(t *testing.T) {
	m := newTestModel(t)
	m.stage = stageDisplay
	m.startNoteEntry("hello world")

	if !m.composer.Focused() || m.mode != modeInsert {
		t.Fatalf("composer should be focused in insert mode (mode=%v)", m.mode)
	}

	if _, handled := m.processComposerKey(tea.KeyMsg{Type: tea.KeyEsc}); !handled {
		t.Fatal("esc should cancel manual note entry")
	}

	if m.composer.Focused() {
		t.Fatal("composer should blur after canceling manual note")
	}
	if value := strings.TrimSpace(m.composer.Value()); value != "" {
		t.Fatalf("composer value not cleared: %q", value)
	}
	if m.mode != modeNormal {
		t.Fatalf("mode not reset after cancel: %v", m.mode)
	}
}

func TestComposerEscCancelsQuestionMode(t *testing.T) {
	m := newTestModel(t)
	m.stage = stageDisplay
	m.paper = &arxiv.Paper{ID: "1234.56789", Title: "Fixture"}
	m.config.LLM = fakeLLM{}

	if cmd := m.actionAskQuestionCmd(); cmd != nil {
		t.Fatalf("ask question should not trigger a command, got %v", cmd)
	}
	if m.composerMode != composerModeQuestion {
		t.Fatalf("expected question mode, got %v", m.composerMode)
	}

	if _, handled := m.processComposerKey(tea.KeyMsg{Type: tea.KeyEsc}); !handled {
		t.Fatal("esc should cancel question entry")
	}

	if m.composerMode != composerModeNote {
		t.Fatalf("composer should return to note mode, got %v", m.composerMode)
	}
	if m.composer.Focused() {
		t.Fatal("composer should blur after canceling question entry")
	}
}

func TestComposerShiftEnterSubmitsURL(t *testing.T) {
	m := newTestModel(t)
	m.composer.SetValue("https://arxiv.org/abs/1234.5678")

	cmd, handled := m.processComposerKey(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if !handled {
		t.Fatal("shift+enter should submit URL entries")
	}
	if cmd == nil {
		t.Fatal("submit should return a command to start fetch job")
	}
	if m.stage != stageLoading {
		t.Fatalf("stage not updated, got %v want %v", m.stage, stageLoading)
	}
	if got := strings.TrimSpace(m.composer.Value()); got != "" {
		t.Fatalf("composer should clear after submission, got %q", got)
	}
}

func TestComposerEnterSubmitsQuestion(t *testing.T) {
	m := newTestModel(t)
	m.paper = &arxiv.Paper{ID: "1234.56789", Title: "Fixture"}
	m.config.LLM = fakeLLM{}
	m.composer.SetValue("What is the evaluation metric?")

	cmd, handled := m.processComposerKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled {
		t.Fatal("enter should submit question entries")
	}
	if cmd == nil {
		t.Fatal("question submission should trigger a command")
	}
	if !m.questionLoading {
		t.Fatal("question should mark loading state")
	}
	if len(m.qaHistory) != 1 {
		t.Fatalf("qa history not updated, got %d entries", len(m.qaHistory))
	}
	if got := m.qaHistory[0].Question; got != "What is the evaluation metric?" {
		t.Fatalf("question text mismatch, got %q", got)
	}
}

func TestComposerCtrlEnterStoresManualNote(t *testing.T) {
	m := newTestModel(t)
	m.paper = &arxiv.Paper{ID: "1234.56789", Title: "Fixture"}
	m.composer.SetValue("Note body")

	cmd, handled := m.processComposerKey(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if !handled {
		t.Fatal("ctrl+enter should submit manual note entries")
	}
	if cmd != nil {
		t.Fatalf("manual note submission should not trigger a command, got %v", cmd)
	}
	if len(m.manualNotes) != 1 {
		t.Fatalf("expected 1 manual note, got %d", len(m.manualNotes))
	}
	if got := m.manualNotes[0].Body; got != "Note body" {
		t.Fatalf("note body mismatch, got %q", got)
	}
}

func TestHydrateConversationHistoryLoadsSnapshot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "zettel.json")
	now := time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)
	snapshot := notes.ConversationSnapshot{
		PaperID:    "1234",
		PaperTitle: "Fixture",
		CapturedAt: now,
		Messages: []notes.ConversationMessage{
			{Kind: "question", Content: "What is this?", Timestamp: now},
		},
		Notes: []notes.SnapshotNote{
			{Title: "Note", Body: "Manual note", Kind: "manual", CreatedAt: now.Add(time.Minute)},
		},
		Brief: &notes.BriefSnapshot{
			Summary: []string{"Summary bullet"},
		},
	}
	if err := notes.SaveConversationSnapshots(path, []notes.ConversationSnapshot{snapshot}); err != nil {
		t.Fatalf("SaveConversationSnapshots() error = %v", err)
	}

	m := newTestModel(t)
	m.config.KnowledgeBasePath = path
	m.paper = &arxiv.Paper{ID: "1234", Title: "Fixture"}
	m.hydrateConversationHistory()

	if len(m.transcriptEntries) != 2 {
		t.Fatalf("expected 2 transcript entries, got %d", len(m.transcriptEntries))
	}
	if m.transcriptEntries[0].Kind != "question" {
		t.Fatalf("expected first entry to be question, got %q", m.transcriptEntries[0].Kind)
	}
	if len(m.brief.Summary) != 1 || m.brief.Summary[0] != "Summary bullet" {
		t.Fatalf("expected brief summary to load, got %#v", m.brief.Summary)
	}
}

func TestHydrateConversationHistoryReportsInvalidJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "zettel.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	m := newTestModel(t)
	m.config.KnowledgeBasePath = path
	m.paper = &arxiv.Paper{ID: "1234", Title: "Fixture"}
	m.hydrateConversationHistory()

	if m.errorMessage == "" {
		t.Fatal("expected knowledge base error message")
	}
}

func TestBriefSectionResultUpdatesState(t *testing.T) {
	m := newTestModel(t)
	m.paper = &arxiv.Paper{ID: "1234.56789", Title: "Fixture"}

	msg := briefSectionMsg{
		paperID: "1234.56789",
		kind:    llm.BriefSummary,
		bullets: []string{"Bullet"},
	}
	if cmd := m.handleBriefSectionResult(msg); cmd != nil {
		t.Fatalf("brief handler should not return a command, got %v", cmd)
	}
	if len(m.brief.Summary) != 1 || m.brief.Summary[0] != "Bullet" {
		t.Fatalf("summary not stored: %#v", m.brief.Summary)
	}
	state := m.sectionState(llm.BriefSummary)
	if state.Loading {
		t.Fatal("section should not be marked loading after result")
	}
	if !state.Completed {
		t.Fatal("section should be marked completed after success")
	}
}

func TestBriefSectionResultSetsError(t *testing.T) {
	m := newTestModel(t)
	m.paper = &arxiv.Paper{ID: "1234.56789", Title: "Fixture"}
	m.markBriefSectionRunning(llm.BriefTechnical)

	msg := briefSectionMsg{
		paperID: "1234.56789",
		kind:    llm.BriefTechnical,
		err:     errors.New("timeout"),
	}
	if cmd := m.handleBriefSectionResult(msg); cmd != nil {
		t.Fatalf("brief handler should not return a command, got %v", cmd)
	}
	state := m.sectionState(llm.BriefTechnical)
	if state.Error == "" {
		t.Fatal("expected section error to be recorded")
	}
	if m.errorMessage == "" {
		t.Fatal("expected model to surface error message")
	}
	if m.briefLoading {
		t.Fatal("error should clear loading flag")
	}
}

func TestBriefSectionStreamUpdatesState(t *testing.T) {
	m := newTestModel(t)
	m.paper = &arxiv.Paper{ID: "1234.56789", Title: "Fixture"}
	msg := briefSectionStreamMsg{
		paperID: "1234.56789",
		kind:    llm.BriefSummary,
		bullets: []string{"partial"},
		done:    true,
	}
	if cmd := m.handleBriefSectionStream(msg); cmd != nil {
		t.Fatalf("expected no follow-up cmd when stream done")
	}
	if len(m.brief.Summary) != 1 || m.brief.Summary[0] != "partial" {
		t.Fatalf("stream did not update summary: %#v", m.brief.Summary)
	}
}

func TestBriefSectionStreamRequestsNextDelta(t *testing.T) {
	m := newTestModel(t)
	m.paper = &arxiv.Paper{ID: "1234.56789", Title: "Fixture"}
	updates := make(chan llm.BriefSectionDelta)
	msg := briefSectionStreamMsg{
		paperID: "1234.56789",
		kind:    llm.BriefTechnical,
		bullets: []string{"partial"},
		done:    false,
		updates: updates,
	}
	if cmd := m.handleBriefSectionStream(msg); cmd == nil {
		t.Fatal("expected command to wait for next stream delta")
	}
	close(updates)
}

func TestPrepareBriefFallbacks(t *testing.T) {
	m := newTestModel(t)
	m.paper = &arxiv.Paper{
		ID:               "1234.56789",
		Title:            "Fixture",
		Abstract:         "Sentence one. Sentence two. Sentence three?",
		KeyContributions: []string{"Contribution A", "Contribution B"},
		Subjects:         []string{"cs.LG", "stat.ML"},
		Authors:          []string{"Alice", "Bob", "Carol"},
		PDFURL:           "https://arxiv.org/pdf/1234.56789.pdf",
	}

	m.prepareBriefFallbacks()

	summary := m.fallbackForSection(llm.BriefSummary)
	if len(summary) == 0 || !strings.Contains(summary[0], "Sentence one") {
		t.Fatalf("missing summary fallback, got %#v", summary)
	}

	technical := m.fallbackForSection(llm.BriefTechnical)
	if len(technical) == 0 || technical[0] != "Contribution A" {
		t.Fatalf("missing technical fallback, got %#v", technical)
	}

	deepDive := m.fallbackForSection(llm.BriefDeepDive)
	if len(deepDive) == 0 || !strings.Contains(deepDive[0], "Focus areas") {
		t.Fatalf("missing deep dive fallback, got %#v", deepDive)
	}
}

func TestDisplayContentIncludesFallbackWhileLoading(t *testing.T) {
	m := newTestModel(t)
	m.config.LLM = fakeLLM{}
	m.paper = &arxiv.Paper{
		ID:               "1234.56789",
		Title:            "Fixture",
		Abstract:         "Sentence one. Sentence two. Sentence three.",
		KeyContributions: []string{"Contribution A"},
		Subjects:         []string{"cs.LG"},
		Authors:          []string{"Alice", "Bob"},
	}

	m.prepareBriefFallbacks()
	m.ensureBriefSections()
	m.briefSections[llm.BriefSummary] = briefSectionState{Loading: true}

	view := m.buildDisplayContent()
	if !strings.Contains(view.content, "Provisional summary from the arXiv abstract.") {
		t.Fatalf("fallback notice missing in view:\n%s", view.content)
	}
	if !strings.Contains(view.content, "Sentence one.") {
		t.Fatalf("summary fallback text missing in view:\n%s", view.content)
	}
}

func TestNavigationCheatsheetToggle(t *testing.T) {
	m := newTestModel(t)
	m.paper = &arxiv.Paper{ID: "1234.56789", Title: "Fixture"}

	view := m.renderStackedDisplay()
	if strings.Contains(view, "Navigation Cheatsheet") {
		t.Fatal("navigation cheatsheet should be hidden by default")
	}

	m.actionToggleHelpCmd()
	view = m.renderStackedDisplay()
	if !strings.Contains(view, "Navigation Cheatsheet") {
		t.Fatal("navigation cheatsheet did not appear after toggling help")
	}

	m.actionToggleHelpCmd()
	view = m.renderStackedDisplay()
	if strings.Contains(view, "Navigation Cheatsheet") {
		t.Fatal("navigation cheatsheet should hide again after second toggle")
	}
}
