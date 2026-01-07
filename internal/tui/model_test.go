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

	if !m.composer.Focused() || m.composerMode != composerModeNote {
		t.Fatalf("composer should be focused in note mode (mode=%v)", m.composerMode)
	}

	if _, handled := m.processComposerKey(tea.KeyMsg{Type: tea.KeyEsc}); !handled {
		t.Fatal("esc should cancel manual note entry")
	}

	if !m.composer.Focused() {
		t.Fatal("composer should remain focused after canceling manual note")
	}
	if value := strings.TrimSpace(m.composer.Value()); value != "" {
		t.Fatalf("composer value not cleared: %q", value)
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
	if !m.composer.Focused() {
		t.Fatal("composer should remain focused after canceling question entry")
	}
}

func TestComposerAltEnterSubmitsURL(t *testing.T) {
	m := newTestModel(t)
	m.composer.SetValue("https://arxiv.org/abs/1234.5678")

	cmd, handled := m.processComposerKey(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if !handled {
		t.Fatal("alt+enter should submit URL entries")
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

func TestComposerEnterSubmitsURLInURLMode(t *testing.T) {
	m := newTestModel(t)
	m.composer.SetValue("https://arxiv.org/abs/1234.5678")

	cmd, handled := m.processComposerKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled {
		t.Fatal("enter should submit URL entries in URL mode")
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
	m.setComposerMode(composerModeNote, composerNotePlaceholder, true)
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

func TestQuestionDraftUpdatedWithRefinedAnswer(t *testing.T) {
	m := newTestModel(t)
	m.paper = &arxiv.Paper{
		ID:       "1234.56789",
		Title:    "Fixture",
		Abstract: "Sentence one. Sentence two.",
	}
	m.config.LLM = fakeLLM{}
	m.setComposerMode(composerModeNote, composerNotePlaceholder, true)
	m.composer.SetValue("What is the evaluation metric?")

	if _, handled := m.processComposerKey(tea.KeyMsg{Type: tea.KeyEnter}); !handled {
		t.Fatal("enter should submit question entries")
	}
	if len(m.transcriptEntries) != 2 {
		t.Fatalf("expected 2 transcript entries, got %d", len(m.transcriptEntries))
	}
	draft := draftAnswerForQuestion(m.paper)
	if draft == "" {
		t.Fatal("expected non-empty draft response")
	}
	entry := m.qaHistory[0]
	if entry.TranscriptIndex < 0 || entry.TranscriptIndex >= len(m.transcriptEntries) {
		t.Fatalf("draft index out of range: %d", entry.TranscriptIndex)
	}
	draftEntry := m.transcriptEntries[entry.TranscriptIndex]
	if draftEntry.Kind != "answer_draft" || draftEntry.Content != draft {
		t.Fatalf("draft entry mismatch: %+v", draftEntry)
	}

	m.handleQuestionResult(questionResultMsg{
		paperID: m.paper.ID,
		index:   0,
		answer:  "Refined answer",
	})
	if len(m.transcriptEntries) != 2 {
		t.Fatalf("expected draft to be updated in place, got %d entries", len(m.transcriptEntries))
	}
	refined := m.transcriptEntries[entry.TranscriptIndex]
	if refined.Kind != "answer" || refined.Content != "Refined answer" {
		t.Fatalf("refined entry mismatch: %+v", refined)
	}
}

func TestQuestionDraftPreservedOnError(t *testing.T) {
	m := newTestModel(t)
	m.paper = &arxiv.Paper{
		ID:       "1234.56789",
		Title:    "Fixture",
		Abstract: "Sentence one. Sentence two.",
	}
	m.config.LLM = fakeLLM{}
	m.setComposerMode(composerModeNote, composerNotePlaceholder, true)
	m.composer.SetValue("What is the evaluation metric?")

	if _, handled := m.processComposerKey(tea.KeyMsg{Type: tea.KeyEnter}); !handled {
		t.Fatal("enter should submit question entries")
	}
	draft := draftAnswerForQuestion(m.paper)
	entry := m.qaHistory[0]

	m.handleQuestionResult(questionResultMsg{
		paperID: m.paper.ID,
		index:   0,
		err:     errors.New("llm down"),
	})
	if len(m.transcriptEntries) != 3 {
		t.Fatalf("expected error transcript append, got %d entries", len(m.transcriptEntries))
	}
	unchanged := m.transcriptEntries[entry.TranscriptIndex]
	if unchanged.Kind != "answer_draft" || unchanged.Content != draft {
		t.Fatalf("draft entry should remain on error: %+v", unchanged)
	}
	if m.errorMessage == "" {
		t.Fatal("expected error message on failure")
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

func TestAppendTranscriptMarksViewportDirty(t *testing.T) {
	m := newTestModel(t)
	m.viewportDirty = false

	m.appendTranscript("note", "hello")

	if !m.viewportDirty {
		t.Fatal("expected viewport to be marked dirty after transcript append")
	}
}

func TestMouseScrollInInputStageUpdatesViewport(t *testing.T) {
	m := newTestModel(t)
	m.stage = stageInput
	m.viewport.Width = 20
	m.viewport.Height = 3
	m.viewport.SetContent(strings.Join([]string{"one", "two", "three", "four", "five"}, "\n"))
	m.viewport.YOffset = 0

	updated, _ := m.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	next, ok := updated.(*model)
	if !ok {
		t.Fatalf("expected *model, got %T", updated)
	}
	if next.viewport.YOffset == 0 {
		t.Fatal("expected mouse wheel to scroll in input stage")
	}
}

func TestMouseScrollIgnoredOutsideDisplayInput(t *testing.T) {
	m := newTestModel(t)
	m.stage = stageLoading
	m.viewport.Width = 20
	m.viewport.Height = 3
	m.viewport.SetContent(strings.Join([]string{"one", "two", "three", "four", "five"}, "\n"))
	m.viewport.YOffset = 0

	updated, _ := m.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	next, ok := updated.(*model)
	if !ok {
		t.Fatalf("expected *model, got %T", updated)
	}
	if next.viewport.YOffset != 0 {
		t.Fatal("mouse scroll should be ignored outside input/display stages")
	}
}

func TestMouseSelectionCopiesToClipboard(t *testing.T) {
	m := newTestModel(t)
	m.stage = stageInput
	m.viewport.SetYOffset(0)
	m.refreshViewport()
	if len(m.viewportLines) < 3 {
		t.Fatalf("expected at least 3 viewport lines, got %d", len(m.viewportLines))
	}
	expected := strings.TrimSpace(stripANSI(strings.Join(m.viewportLines[:3], "\n")))

	var copied string
	originalClipboard := clipboardWrite
	clipboardWrite = func(text string) error {
		copied = text
		return nil
	}
	t.Cleanup(func() { clipboardWrite = originalClipboard })

	top := m.viewportStartRow()
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, Y: top})
	m.Update(tea.MouseMsg{Type: tea.MouseMotion, Y: top + 2})
	m.Update(tea.MouseMsg{Type: tea.MouseRelease, Y: top + 2})

	if copied != expected {
		t.Fatalf("copied text mismatch: %q", copied)
	}
	if m.infoMessage != "Selection copied to clipboard." {
		t.Fatalf("unexpected info message: %q", m.infoMessage)
	}
}

func TestMouseSelectionClipboardFailureSetsError(t *testing.T) {
	m := newTestModel(t)
	m.stage = stageInput
	m.viewport.SetYOffset(0)
	m.refreshViewport()

	originalClipboard := clipboardWrite
	clipboardWrite = func(text string) error {
		return errors.New("clipboard unavailable")
	}
	t.Cleanup(func() { clipboardWrite = originalClipboard })

	top := m.viewportStartRow()
	m.Update(tea.MouseMsg{Type: tea.MouseLeft, Y: top})
	m.Update(tea.MouseMsg{Type: tea.MouseRelease, Y: top})

	if m.errorMessage == "" {
		t.Fatal("expected clipboard error message")
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
	m.viewportDirty = false
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
	if !m.viewportDirty {
		t.Fatal("expected viewport dirty after loading conversation history")
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

func TestSeedBriefMessagesUsesFallbacks(t *testing.T) {
	m := newTestModel(t)
	m.paper = &arxiv.Paper{
		ID:               "1234.56789",
		Title:            "Fixture",
		Abstract:         "Sentence one. Sentence two.",
		KeyContributions: []string{"Contribution A"},
		Subjects:         []string{"cs.LG"},
		Authors:          []string{"Alice"},
		PDFURL:           "https://arxiv.org/pdf/1234.56789.pdf",
	}
	m.prepareBriefFallbacks()

	m.seedBriefMessages()

	if len(m.transcriptEntries) != len(briefSectionKinds) {
		t.Fatalf("expected %d brief messages, got %d", len(briefSectionKinds), len(m.transcriptEntries))
	}
	summaryIdx, ok := m.briefMessageIndex[llm.BriefSummary]
	if !ok {
		t.Fatal("expected summary brief message to be indexed")
	}
	content := m.transcriptEntries[summaryIdx].Content
	if !strings.Contains(content, "### Summary") {
		t.Fatalf("expected summary heading, got %q", content)
	}
	if !strings.Contains(content, fallbackNotice(llm.BriefSummary)) {
		t.Fatalf("expected fallback notice, got %q", content)
	}
}

func TestBriefSectionResultUpdatesSeededMessage(t *testing.T) {
	m := newTestModel(t)
	m.paper = &arxiv.Paper{
		ID:       "1234.56789",
		Title:    "Fixture",
		Abstract: "Sentence one. Sentence two.",
	}
	m.prepareBriefFallbacks()
	m.seedBriefMessages()
	summaryIdx := m.briefMessageIndex[llm.BriefSummary]
	initialCount := len(m.transcriptEntries)

	msg := briefSectionMsg{
		paperID: m.paper.ID,
		kind:    llm.BriefSummary,
		bullets: []string{"Bullet"},
	}
	m.handleBriefSectionResult(msg)

	if len(m.transcriptEntries) != initialCount {
		t.Fatalf("expected brief message update in place, got %d entries", len(m.transcriptEntries))
	}
	if m.briefMessageIndex[llm.BriefSummary] != summaryIdx {
		t.Fatal("expected summary message index to remain stable")
	}
	if !strings.Contains(m.transcriptEntries[summaryIdx].Content, "Bullet") {
		t.Fatalf("expected summary message to be updated, got %q", m.transcriptEntries[summaryIdx].Content)
	}
}

func TestQuestionQueuedUntilBriefReady(t *testing.T) {
	m := newTestModel(t)
	m.config.LLM = fakeLLM{}
	m.paper = &arxiv.Paper{ID: "1234.56789", Title: "Fixture", FullText: "content"}
	m.ensureBriefSections()
	m.setComposerMode(composerModeQuestion, composerQuestionPlaceholder, true)
	m.composer.SetValue("What is this?")

	m.submitComposer()

	if len(m.queuedQuestions) != 1 {
		t.Fatalf("expected queued question, got %d", len(m.queuedQuestions))
	}
	if m.questionLoading {
		t.Fatal("question loading should be false while queued")
	}
	if len(m.qaHistory) != 1 {
		t.Fatalf("expected qa history entry, got %d", len(m.qaHistory))
	}
	if m.infoMessage != "Question queued; waiting for the brief to finish." {
		t.Fatalf("unexpected info message: %q", m.infoMessage)
	}
}

func TestQueuedQuestionStartsAfterBriefCompletes(t *testing.T) {
	m := newTestModel(t)
	m.config.LLM = fakeLLM{}
	m.paper = &arxiv.Paper{ID: "1234.56789", Title: "Fixture", FullText: "content"}
	m.ensureBriefSections()
	m.setComposerMode(composerModeQuestion, composerQuestionPlaceholder, true)
	m.composer.SetValue("What is this?")
	m.submitComposer()

	var cmd tea.Cmd
	for _, kind := range briefSectionKinds {
		cmd = m.handleBriefSectionResult(briefSectionMsg{
			paperID: m.paper.ID,
			kind:    kind,
			bullets: []string{"Bullet"},
		})
	}

	if len(m.queuedQuestions) != 0 {
		t.Fatalf("expected queued questions to be flushed, got %d", len(m.queuedQuestions))
	}
	if !m.questionLoading {
		t.Fatal("expected queued question to start loading after brief completes")
	}
	if cmd == nil {
		t.Fatal("expected command to launch queued question")
	}
}

func TestComposerHeightWrapsInput(t *testing.T) {
	m := newTestModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 30})
	m = updated.(*model)

	m.composer.SetValue(strings.Repeat("word ", 60))
	m.updateComposerHeight()

	if m.composer.Height() <= 1 {
		t.Fatalf("expected composer height to grow, got %d", m.composer.Height())
	}
	if m.composer.Height() > maxComposerHeight {
		t.Fatalf("expected composer height <= %d, got %d", maxComposerHeight, m.composer.Height())
	}
}

func TestBriefMessageContentLimitsSummaryBullets(t *testing.T) {
	bullets := []string{"one", "two", "three", "four", "five", "six"}
	content := briefMessageContent(llm.BriefSummary, bullets)
	lines := strings.Split(content, "\n")
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "###") || strings.HasPrefix(trimmed, ">") {
			continue
		}
		count++
	}
	if count > 5 {
		t.Fatalf("expected at most 5 summary lines, got %d", count)
	}
}

func TestDisplayContentOmitsStaticBriefSections(t *testing.T) {
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
	if strings.Contains(view.content, "Summary Pass") || strings.Contains(view.content, "Technical Details") || strings.Contains(view.content, "Deep Dive References") {
		t.Fatalf("static brief sections should be omitted in view:\n%s", view.content)
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
