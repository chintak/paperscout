package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/csheth/browse/internal/arxiv"
	"github.com/csheth/browse/internal/llm"
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

func TestComposerEnterSubmitsURL(t *testing.T) {
	m := newTestModel(t)
	m.composer.SetValue("https://arxiv.org/abs/1234.5678")

	cmd, handled := m.processComposerKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled {
		t.Fatal("enter should submit URL mode entries")
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
	if cmd := m.actionAskQuestionCmd(); cmd != nil {
		t.Fatalf("ask question should not trigger a command, got %v", cmd)
	}
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
