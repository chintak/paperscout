package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"

	"github.com/csheth/browse/internal/arxiv"
	briefctx "github.com/csheth/browse/internal/brief/context"
	"github.com/csheth/browse/internal/guide"
	"github.com/csheth/browse/internal/llm"
	"github.com/csheth/browse/internal/notes"
)

// Config wires runtime options into the TUI program.
type Config struct {
	KnowledgeBasePath string
	LLM               llm.Client
}

// New returns a tea.Model ready to be mounted into a Program.
func New(config Config) tea.Model {
	paletteInput := textinput.New()
	paletteInput.Placeholder = "Filter commands…"
	paletteInput.CharLimit = 80
	paletteInput.Width = 50

	composer := textarea.New()
	composer.Placeholder = composerNotePlaceholder
	composer.CharLimit = 2000
	composer.ShowLineNumbers = false
	composer.Prompt = "> "
	composer.SetPromptFunc(lipgloss.Width(composer.Prompt), func(line int) string {
		if line == 0 {
			return composer.Prompt
		}
		return strings.Repeat(" ", len(composer.Prompt))
	})
	composer.SetWidth(80)
	composer.SetHeight(1)
	composer.EndOfBufferCharacter = ' '
	composer.FocusedStyle.Base = composerFocusedBaseStyle
	composer.FocusedStyle.CursorLine = composerCursorLineFocusedStyle
	composer.FocusedStyle.CursorLineNumber = lipgloss.NewStyle()
	composer.FocusedStyle.LineNumber = lipgloss.NewStyle()
	composer.FocusedStyle.Placeholder = composerPlaceholderStyle
	composer.FocusedStyle.Prompt = composerPromptStyle
	composer.FocusedStyle.Text = composerFocusedTextStyle
	composer.BlurredStyle.Base = composerBlurredBaseStyle
	composer.BlurredStyle.CursorLine = composerCursorLineBlurredStyle
	composer.BlurredStyle.CursorLineNumber = lipgloss.NewStyle()
	composer.BlurredStyle.LineNumber = lipgloss.NewStyle()
	composer.BlurredStyle.Placeholder = composerPlaceholderStyle
	composer.BlurredStyle.Prompt = composerPromptStyle
	composer.BlurredStyle.Text = composerBlurredTextStyle

	spin := spinner.New()
	spin.Spinner = spinner.Dot

	vp := viewport.New(80, 20)
	vp.MouseWheelEnabled = true

	logViewport := viewport.New(80, 10)
	logViewport.MouseWheelEnabled = true

	m := &model{
		config:                  config,
		stage:                   stageInput,
		spinner:                 spin,
		viewport:                vp,
		transcriptViewport:      logViewport,
		composer:                composer,
		selected:                map[int]bool{},
		persisted:               map[int]bool{},
		suggestionLines:         map[int]int{},
		cursorLine:              0,
		viewportDirty:           true,
		infoMessage:             "Paste an arXiv url or identifier to begin.",
		sectionAnchors:          map[string]int{},
		pendingFocusAnchor:      "",
		jobBus:                  newJobBus(),
		jobSnapshots:            map[jobKind]jobSnapshot{},
		layout:                  newPageLayout(),
		paletteInput:            paletteInput,
		transcriptViewportDirty: true,
	}

	m.setComposerMode(composerModeURL, composerURLPlaceholder, true)
	m.resetBriefState()
	return m
}

type model struct {
	config Config
	stage  stage

	spinner            spinner.Model
	viewport           viewport.Model
	transcriptViewport viewport.Model
	composer           textarea.Model

	paper                   *arxiv.Paper
	guide                   []guide.Step
	suggestions             []notes.Candidate
	selected                map[int]bool
	persisted               map[int]bool
	cursorLine              int
	lineCount               int
	manualNotes             []notes.Note
	persistedNotes          []notes.Note
	suggestionLines         map[int]int
	viewportLines           []string
	viewportContent         string
	viewportDirty           bool
	infoMessage             string
	errorMessage            string
	helpVisible             bool
	sectionAnchors          map[string]int
	brief                   llm.ReadingBrief
	briefSections           map[llm.BriefSectionKind]briefSectionState
	briefFallbacks          map[llm.BriefSectionKind][]string
	briefContexts           map[llm.BriefSectionKind]string
	briefMessageIndex       map[llm.BriefSectionKind]int
	briefChunks             []briefctx.Chunk
	briefStreamCancels      map[llm.BriefSectionKind]context.CancelFunc
	briefLoading            bool
	suggestionLoading       bool
	qaHistory               []qaExchange
	queuedQuestions         []int
	questionLoading         bool
	selectionAnchor         int
	selectionActive         bool
	mouseSelectionActive    bool
	pendingFocusAnchor      string
	jobBus                  *jobBus
	jobSnapshots            map[jobKind]jobSnapshot
	layout                  pageLayout
	paletteInput            textinput.Model
	paletteMatches          []uiCommand
	paletteCursor           int
	paletteReturnStage      stage
	transcriptEntries       []transcriptEntry
	transcriptViewportDirty bool
	composerMode            composerMode
}

type paperResultMsg struct {
	paper       *arxiv.Paper
	guide       []guide.Step
	suggestions []notes.Candidate
	err         error
}

type saveResultMsg struct {
	count int
	err   error
}

type briefSectionMsg struct {
	paperID string
	kind    llm.BriefSectionKind
	bullets []string
	err     error
}

type briefSectionStreamMsg struct {
	paperID string
	kind    llm.BriefSectionKind
	bullets []string
	done    bool
	updates <-chan llm.BriefSectionDelta
}

type questionResultMsg struct {
	paperID string
	index   int
	answer  string
	err     error
}

type suggestionResultMsg struct {
	paperID     string
	suggestions []notes.Candidate
	err         error
}

type actionID string

type uiCommand struct {
	id          actionID
	title       string
	description string
	shortcut    string
}

type transcriptEntry struct {
	Kind      string
	Content   string
	Timestamp time.Time
}

type briefSectionState struct {
	Loading   bool
	Completed bool
	Error     string
}

const (
	actionSummarize   actionID = "summarize"
	actionAskQuestion actionID = "ask_question"
	actionManualNote  actionID = "manual_note"
	actionSaveNotes   actionID = "save_notes"
	actionToggleHelp  actionID = "toggle_help"
	actionLoadNew     actionID = "load_new"
)

var paletteCommands = []uiCommand{
	{id: actionSummarize, title: "Summarize paper", description: "Regenerate the LLM reading brief for the loaded PDF"},
	{id: actionAskQuestion, title: "Ask a question", description: "Open the Q&A prompt"},
	{id: actionManualNote, title: "Add manual note", description: "Draft a manual note", shortcut: "m"},
	{id: actionSaveNotes, title: "Save manual notes", description: "Persist any manual notes you drafted this session", shortcut: "s"},
	{id: actionToggleHelp, title: "Toggle help overlay", description: "Show or hide the command cheatsheet", shortcut: "?"},
	{id: actionLoadNew, title: "Load another paper", description: "Return to the URL prompt", shortcut: "r"},
}

var briefSectionKinds = []llm.BriefSectionKind{
	llm.BriefSummary,
	llm.BriefTechnical,
	llm.BriefDeepDive,
}

func (m *model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case jobSignalMsg:
		m.recordJobSnapshot(msg.Snapshot)
		return m, nil
	case jobResultEnvelope:
		m.recordJobSnapshot(msg.Snapshot)
		if msg.Payload == nil {
			return m, nil
		}
		return m.handleJobPayload(msg.Payload)
	case spinner.TickMsg:
		if m.stage == stageLoading || m.stage == stageSaving || m.briefLoading || m.questionLoading || m.suggestionLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlK {
			if m.stage == stagePalette {
				m.closeCommandPalette()
			} else {
				m.openCommandPalette()
			}
			return m, nil
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			switch m.stage {
			case stagePalette:
				m.closeCommandPalette()
				return m, nil
			case stageDisplay:
				return m, tea.Quit
			default:
				return m, tea.Quit
			}
		}
		return m.handleKey(msg)
	case tea.MouseMsg:
		if m.stage == stageDisplay || m.stage == stageInput {
			if m.handleMouseSelection(msg) {
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		return m, nil
	case paperResultMsg:
		return m, m.handlePaperResult(msg)
	case saveResultMsg:
		return m, m.handleSaveResult(msg)
	case briefSectionMsg:
		return m, m.handleBriefSectionResult(msg)
	case briefSectionStreamMsg:
		return m, m.handleBriefSectionStream(msg)
	case questionResultMsg:
		return m, m.handleQuestionResult(msg)
	case suggestionResultMsg:
		return m, m.handleSuggestionResult(msg)
	case tea.WindowSizeMsg:
		m.layout.Update(msg.Width, msg.Height)
		composerWidth := m.layout.windowWidth - viewportHorizontalPadding
		if composerWidth > 72 {
			composerWidth = 72
		}
		if composerWidth < minViewportWidth {
			composerWidth = minViewportWidth
		}
		m.composer.SetWidth(composerWidth)
		m.syncLayout()
		m.updateComposerHeight()
		m.markTranscriptDirty()
		m.markViewportDirty()
		return m, nil
	}
	return m, nil
}

func (m *model) handleMouseSelection(msg tea.MouseMsg) bool {
	switch msg.Type {
	case tea.MouseLeft, tea.MouseMotion, tea.MouseRelease:
	default:
		return false
	}

	line, ok := m.viewportLineForMouse(msg)
	switch msg.Type {
	case tea.MouseLeft:
		if !ok {
			return false
		}
		m.mouseSelectionActive = true
		m.selectionActive = true
		m.selectionAnchor = line
		m.cursorLine = line
		m.markViewportDirty()
		return true
	case tea.MouseMotion:
		if !m.mouseSelectionActive || !ok {
			return false
		}
		if line != m.cursorLine {
			m.cursorLine = line
			m.markViewportDirty()
		}
		return true
	case tea.MouseRelease:
		if !m.mouseSelectionActive {
			return false
		}
		if ok {
			m.cursorLine = line
		}
		m.copySelectionToClipboard()
		m.clearSelection()
		m.markViewportDirty()
		return true
	default:
		return false
	}
}

func (m *model) viewportLineForMouse(msg tea.MouseMsg) (int, bool) {
	m.refreshViewportIfDirty()
	if m.viewport.Height <= 0 {
		return 0, false
	}
	top := m.viewportStartRow()
	if msg.Y < top || msg.Y >= top+m.viewport.Height {
		return 0, false
	}
	line := m.viewport.YOffset + (msg.Y - top)
	if line < 0 || line >= m.lineCount {
		return 0, false
	}
	return line, true
}

func (m *model) viewportStartRow() int {
	return 0
}

func (m *model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.stage {
	case stageInput:
		if cmd, handled := m.processComposerKey(key); handled {
			return m, cmd
		}
		return m, nil
	case stageLoading:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(key)
		return m, cmd
	case stageDisplay:
		return m.handleDisplayKey(key)
	case stageSaving:
		return m, nil
	case stagePalette:
		return m.handlePaletteKey(key)
	default:
		return m, nil
	}
}

func (m *model) handleDisplayKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if cmd, handled := m.processComposerKey(key); handled {
		return m, cmd
	}
	handled := true
	switch key.String() {
	case "m":
		return m, m.actionManualNoteCmd()
	case "g":
		m.scrollToTop()
	case "G":
		m.scrollToBottom()
	case "]":
		m.jumpToRelativeSection(1)
	case "[":
		m.jumpToRelativeSection(-1)
	case "r":
		return m, m.actionLoadNewCmd()
	case "s":
		return m, m.actionSaveCmd()
	case "?":
		return m, m.actionToggleHelpCmd()
	default:
		handled = false
	}
	if handled {
		return m, nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(key)
	return m, cmd
}

func (m *model) handlePaletteKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEnter:
		if len(m.paletteMatches) == 0 {
			return m, nil
		}
		cmd := m.runCommand(m.paletteMatches[m.paletteCursor].id)
		m.closeCommandPalette()
		return m, cmd
	case tea.KeyUp, tea.KeyCtrlP:
		if len(m.paletteMatches) > 0 && m.paletteCursor > 0 {
			m.paletteCursor--
			m.markViewportDirty()
		}
		return m, nil
	case tea.KeyDown, tea.KeyCtrlN:
		if len(m.paletteMatches) > 0 && m.paletteCursor < len(m.paletteMatches)-1 {
			m.paletteCursor++
			m.markViewportDirty()
		}
		return m, nil
	}
	var inputCmd tea.Cmd
	m.paletteInput, inputCmd = m.paletteInput.Update(key)
	m.refreshPaletteMatches()
	return m, inputCmd
}

func (m *model) processComposerKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if !m.composer.Focused() {
		return nil, false
	}
	switch key.Type {
	case tea.KeyCtrlC:
		return tea.Quit, true
	case tea.KeyEsc:
		m.cancelComposerEntry()
		return nil, true
	}
	switch {
	case isCtrlEnter(key):
		m.composerMode = composerModeNote
		return m.submitComposer(), true
	case isAltEnter(key):
		m.composerMode = composerModeURL
		return m.submitComposer(), true
	case key.Type == tea.KeyEnter:
		if m.composerMode == composerModeURL {
			return m.submitComposer(), true
		}
		m.composerMode = composerModeQuestion
		return m.submitComposer(), true
	}
	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(key)
	m.updateComposerHeight()
	return cmd, true
}

func (m *model) collectSelectedNotes() []notes.Note {
	if m.paper == nil {
		return nil
	}
	result := []notes.Note{}
	for index, candidate := range m.suggestions {
		if m.selected[index] && !m.persisted[index] {
			result = append(result, candidate.ToNote(m.paper.ID, m.paper.Title))
		}
	}
	result = append(result, m.manualNotes...)
	return result
}

func (m *model) selectedCount() int {
	count := 0
	for idx, selected := range m.selected {
		if selected && !m.persisted[idx] {
			count++
		}
	}
	return count
}

func (m *model) refreshPersistedState() {
	if m.paper == nil || m.config.KnowledgeBasePath == "" {
		m.persistedNotes = nil
		m.persisted = map[int]bool{}
		m.markViewportDirty()
		return
	}
	records, err := notes.Load(m.config.KnowledgeBasePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			m.persistedNotes = nil
			m.persisted = map[int]bool{}
			m.markViewportDirty()
			return
		}
		m.errorMessage = fmt.Sprintf("knowledge base error: %v", err)
		return
	}

	results := []notes.Note{}
	for _, n := range records {
		if n.PaperID == m.paper.ID {
			results = append(results, n)
		}
	}
	m.persistedNotes = results
	m.persisted = map[int]bool{}
	for idx, suggestion := range m.suggestions {
		if candidateMatchesNotes(suggestion, results) {
			m.persisted[idx] = true
			m.selected[idx] = true
		}
	}
	m.markViewportDirty()
}

func (m *model) hydrateConversationHistory() {
	m.transcriptEntries = nil
	if m.paper == nil || m.config.KnowledgeBasePath == "" {
		return
	}
	snapshots, err := notes.LoadConversationSnapshots(m.config.KnowledgeBasePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		m.errorMessage = fmt.Sprintf("knowledge base error: %v", err)
		return
	}
	var snapshot *notes.ConversationSnapshot
	for i := range snapshots {
		if snapshots[i].PaperID == m.paper.ID {
			snapshot = &snapshots[i]
			break
		}
	}
	if snapshot == nil {
		return
	}
	if snapshot.Brief != nil {
		m.brief = llm.ReadingBrief{
			Summary:   append([]string(nil), snapshot.Brief.Summary...),
			Technical: append([]string(nil), snapshot.Brief.Technical...),
			DeepDive:  append([]string(nil), snapshot.Brief.DeepDive...),
		}
	}
	entries := make([]transcriptEntry, 0, len(snapshot.Messages)+len(snapshot.Notes))
	for _, msg := range snapshot.Messages {
		entries = append(entries, transcriptEntry{
			Kind:      msg.Kind,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		})
	}
	for _, note := range snapshot.Notes {
		content := strings.TrimSpace(note.Body)
		if content == "" {
			content = note.Title
		}
		entries = append(entries, transcriptEntry{
			Kind:      "note",
			Content:   content,
			Timestamp: note.CreatedAt,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})
	m.transcriptEntries = entries
	m.mapBriefMessages()
	m.markTranscriptDirty()
	m.markViewportDirty()
}

func (m *model) markViewportDirty() {
	m.viewportDirty = true
}

func (m *model) refreshViewportIfDirty() {
	if m.viewportDirty {
		m.refreshViewport()
	}
}

func (m *model) markTranscriptDirty() {
	m.transcriptViewportDirty = true
}

func (m *model) refreshTranscriptIfDirty() {
	if m.transcriptViewportDirty {
		m.refreshTranscript()
	}
}

func (m *model) syncLayout() {
	m.viewport.Width = m.layout.viewportWidth
	m.viewport.Height = m.layout.viewportHeight
	m.transcriptViewport.Width = m.layout.viewportWidth
	m.transcriptViewport.Height = m.layout.transcriptHeight
}

func (m *model) updateComposerHeight() {
	if m.layout.windowWidth == 0 || m.layout.windowHeight == 0 {
		return
	}
	desired := m.desiredComposerHeight()
	changed := false
	if desired != m.layout.composerHeight {
		m.layout.SetComposerHeight(desired)
		m.syncLayout()
		changed = true
	}
	if m.composer.Height() != m.layout.composerHeight {
		m.composer.SetHeight(m.layout.composerHeight)
		changed = true
	}
	if changed {
		m.markTranscriptDirty()
		m.markViewportDirty()
	}
}

func (m *model) desiredComposerHeight() int {
	width := m.composer.Width()
	if width <= 0 {
		return 1
	}
	value := m.composer.Value()
	if value == "" {
		return 1
	}
	lines := strings.Split(value, "\n")
	height := 0
	for _, line := range lines {
		if line == "" {
			height++
			continue
		}
		wrapped := wordwrap.String(line, width)
		if wrapped == "" {
			height++
			continue
		}
		height += strings.Count(wrapped, "\n") + 1
	}
	if height < 1 {
		height = 1
	}
	if height > maxComposerHeight {
		height = maxComposerHeight
	}
	return height
}

func (m *model) refreshViewport() {
	m.viewportDirty = false
	prevYOffset := m.viewport.YOffset
	var view displayView
	if m.paper == nil {
		m.viewport.Height = m.layout.viewportHeight
		view = m.buildIdleContent()
	} else {
		m.viewport.Height = m.layout.viewportHeight
		view = m.buildDisplayContent()
	}
	m.viewportContent = view.content
	m.suggestionLines = view.suggestionLines
	m.sectionAnchors = view.anchors
	m.viewportLines = splitLinesPreserve(view.content)
	m.lineCount = len(m.viewportLines)
	if m.lineCount == 0 {
		m.viewportLines = []string{""}
		m.lineCount = 1
	}
	if m.cursorLine >= m.lineCount {
		m.cursorLine = m.lineCount - 1
	}
	if m.cursorLine < 0 {
		m.cursorLine = 0
	}

	forcedYOffset := -1
	if m.pendingFocusAnchor != "" {
		if line, ok := view.anchors[m.pendingFocusAnchor]; ok {
			if line < 0 {
				line = 0
			} else if line >= m.lineCount {
				line = m.lineCount - 1
			}
			if line < 0 {
				line = 0
			}
			m.cursorLine = line
			m.clearSelection()
			forcedYOffset = line
			m.pendingFocusAnchor = ""
		}
	}

	m.viewport.SetContent(view.content)
	targetYOffset := prevYOffset
	if forcedYOffset >= 0 {
		targetYOffset = forcedYOffset
	}
	m.viewport.SetYOffset(m.clampYOffset(targetYOffset))
}

func (m *model) refreshTranscript() {
	m.transcriptViewportDirty = false
	wrap := m.layout.viewportWidth - 4
	if wrap < 20 {
		wrap = 20
	}
	var b strings.Builder
	for _, entry := range m.transcriptEntries {
		header := fmt.Sprintf("[%s] %s", entry.Timestamp.Format("15:04:05"), entry.Kind)
		b.WriteString(helperStyle.Render(header))
		b.WriteRune('\n')
		b.WriteString(wordwrap.String(entry.Content, wrap))
		b.WriteRune('\n')
		b.WriteRune('\n')
	}
	content := strings.TrimSpace(b.String())
	m.transcriptViewport.SetContent(content)
	lines := splitLinesPreserve(content)
	if len(lines) > m.transcriptViewport.Height {
		offset := len(lines) - m.transcriptViewport.Height
		if offset < 0 {
			offset = 0
		}
		m.transcriptViewport.SetYOffset(offset)
	}
}

func (m *model) startNoteEntry(prefill string) {
	m.clearSelection()
	m.composer.SetValue(prefill)
	m.setComposerMode(composerModeNote, composerNotePlaceholder, true)
}

func (m *model) setComposerMode(mode composerMode, placeholder string, focus bool) {
	m.composerMode = mode
	if placeholder != "" {
		m.composer.Placeholder = placeholder
	}
	m.composer.Focus()
	if m.layout.windowWidth > 0 && m.layout.windowHeight > 0 {
		m.updateComposerHeight()
	}
}

func (m *model) cancelComposerEntry() {
	switch m.composerMode {
	case composerModeURL:
		m.composer.SetValue("")
		m.setComposerMode(composerModeURL, composerURLPlaceholder, true)
		m.infoMessage = "Composer cleared."
	case composerModeNote:
		m.composer.SetValue("")
		m.setComposerMode(composerModeNote, composerNotePlaceholder, false)
		m.clearSelection()
		m.infoMessage = "Manual note canceled."
	case composerModeQuestion:
		m.composer.SetValue("")
		m.setComposerMode(composerModeNote, composerNotePlaceholder, false)
		m.infoMessage = "Question canceled."
	default:
		m.composer.SetValue("")
		m.setComposerMode(composerModeNote, composerNotePlaceholder, true)
	}
}

func (m *model) submitComposer() tea.Cmd {
	value := strings.TrimSpace(m.composer.Value())
	if value == "" {
		m.infoMessage = "Type something before submitting."
		return nil
	}
	switch m.composerMode {
	case composerModeURL:
		m.stage = stageLoading
		m.errorMessage = ""
		m.infoMessage = "Fetching metadata…"
		m.appendTranscript("fetch", fmt.Sprintf("Fetching %s", value))
		m.composer.SetValue("")
		m.setComposerMode(composerModeURL, composerURLPlaceholder, false)
		return tea.Batch(m.spinner.Tick, m.jobBus.Start(jobKindFetch, fetchPaperJob(value)))
	case composerModeNote:
		if m.paper == nil {
			m.infoMessage = "Load a paper before drafting notes."
			return nil
		}
		createdAt := time.Now()
		title := trimmedTitle(value)
		m.manualNotes = append(m.manualNotes, notes.Note{
			PaperID:    m.paper.ID,
			PaperTitle: m.paper.Title,
			Title:      title,
			Body:       value,
			Kind:       "manual",
			CreatedAt:  createdAt,
		})
		m.infoMessage = fmt.Sprintf("Manual note added (%d total).", len(m.manualNotes))
		m.markViewportDirty()
		m.appendTranscript("note", value)
		m.composer.SetValue("")
		m.setComposerMode(composerModeNote, composerNotePlaceholder, false)
		snapshotCmd := m.appendConversationSnapshotCmd(notes.SnapshotUpdate{
			Notes: []notes.SnapshotNote{
				{
					Title:     title,
					Body:      value,
					Kind:      "manual",
					CreatedAt: createdAt,
				},
			},
		})
		return snapshotCmd
	case composerModeQuestion:
		if m.paper == nil {
			m.infoMessage = "Load a paper before asking questions."
			return nil
		}
		if m.config.LLM == nil {
			m.infoMessage = "Configure Ollama to unlock questions."
			return nil
		}
		askedAt := time.Now()
		entry := qaExchange{
			Question:        value,
			Pending:         true,
			AskedAt:         askedAt,
			TranscriptIndex: -1,
		}
		m.appendTranscript("question", value)
		m.qaHistory = append(m.qaHistory, entry)
		idx := len(m.qaHistory) - 1
		m.composer.SetValue("")
		m.setComposerMode(composerModeNote, composerNotePlaceholder, false)
		snapshotCmd := m.appendConversationSnapshotCmd(notes.SnapshotUpdate{
			Messages: []notes.ConversationMessage{
				{
					Kind:      "question",
					Content:   value,
					Timestamp: askedAt,
				},
			},
		})
		if !m.briefReadyForQuestions() {
			m.enqueueQuestion(idx)
			m.infoMessage = "Question queued; waiting for the brief to finish."
			return snapshotCmd
		}
		questionCmd := m.launchQuestion(idx, true, "")
		if snapshotCmd != nil && questionCmd != nil {
			return tea.Batch(snapshotCmd, questionCmd)
		}
		if snapshotCmd != nil {
			return snapshotCmd
		}
		return questionCmd
	default:
		m.infoMessage = "Composer inactive. Press m or q to begin."
		return nil
	}
}

func (m *model) briefReadyForQuestions() bool {
	if m.paper == nil || m.config.LLM == nil {
		return true
	}
	if strings.TrimSpace(m.paper.FullText) == "" {
		return true
	}
	if m.briefSections == nil {
		return false
	}
	for _, kind := range briefSectionKinds {
		state, ok := m.briefSections[kind]
		if !ok {
			return false
		}
		if !state.Completed && state.Error == "" {
			return false
		}
	}
	return true
}

func (m *model) enqueueQuestion(index int) {
	m.queuedQuestions = append(m.queuedQuestions, index)
}

func (m *model) launchQuestion(index int, allowDraft bool, infoMessage string) tea.Cmd {
	if m.paper == nil || m.config.LLM == nil {
		return nil
	}
	if index < 0 || index >= len(m.qaHistory) {
		return nil
	}
	entry := &m.qaHistory[index]
	setInfo := true
	if allowDraft && entry.Answer == "" && entry.TranscriptIndex < 0 {
		if draft := draftAnswerForQuestion(m.paper); draft != "" {
			entry.Answer = draft
			entry.TranscriptIndex = m.appendTranscriptEntry("answer_draft", draft)
			m.infoMessage = "Draft response ready; refining via LLM…"
			setInfo = false
		}
	}
	if setInfo {
		if infoMessage != "" {
			m.infoMessage = infoMessage
		} else {
			m.infoMessage = "Answering question via LLM…"
		}
	}
	m.questionLoading = true
	return tea.Batch(m.spinner.Tick, m.jobBus.Start(jobKindQuestion, questionAnswerJob(index, m.config.LLM, m.paper, entry.Question)))
}

func (m *model) maybeStartQueuedQuestion() tea.Cmd {
	if !m.briefReadyForQuestions() || m.questionLoading || len(m.queuedQuestions) == 0 {
		return nil
	}
	index := m.queuedQuestions[0]
	m.queuedQuestions = m.queuedQuestions[1:]
	return m.launchQuestion(index, false, "Answering queued question via LLM…")
}

func (m *model) blurComposer() {
	m.composer.Blur()
	m.composerMode = composerModeIdle
}

func isCtrlEnter(key tea.KeyMsg) bool {
	switch key.String() {
	case "ctrl+enter", "ctrl+m", "ctrl+j":
		return true
	default:
		return false
	}
}

func isAltEnter(key tea.KeyMsg) bool {
	return key.Type == tea.KeyEnter && key.Alt
}

func (m *model) jumpToRelativeSection(delta int) {
	anchors := m.availableSections()
	if len(anchors) == 0 {
		m.infoMessage = "No sections available yet."
		return
	}
	currentLine := m.viewport.YOffset
	if delta > 0 {
		for _, anchor := range anchors {
			line := m.sectionAnchors[anchor]
			if line > currentLine {
				m.jumpToSection(anchor)
				return
			}
		}
		m.infoMessage = "Already at the last section."
		return
	}
	if delta < 0 {
		for i := len(anchors) - 1; i >= 0; i-- {
			anchor := anchors[i]
			line := m.sectionAnchors[anchor]
			if line < currentLine {
				m.jumpToSection(anchor)
				return
			}
		}
		m.infoMessage = "Already at the first section."
		return
	}
}

func (m *model) availableSections() []string {
	if len(m.sectionAnchors) == 0 {
		return nil
	}
	var ordered []string
	for _, anchor := range sectionSequence {
		if _, ok := m.sectionAnchors[anchor]; ok {
			ordered = append(ordered, anchor)
		}
	}
	return ordered
}

func (m *model) clearSelection() {
	m.selectionActive = false
	m.mouseSelectionActive = false
}

func (m *model) selectionRange() (int, int, bool) {
	if !m.selectionActive || !m.mouseSelectionActive || m.lineCount == 0 {
		return 0, 0, false
	}
	start, end := m.selectionAnchor, m.cursorLine
	if start > end {
		start, end = end, start
	}
	if start < 0 {
		start = 0
	}
	if end >= m.lineCount {
		end = m.lineCount - 1
	}
	return start, end, true
}

func (m *model) selectedText() string {
	start, end, ok := m.selectionRange()
	if !ok || len(m.viewportLines) == 0 {
		return ""
	}
	if end >= len(m.viewportLines) {
		end = len(m.viewportLines) - 1
	}
	var lines []string
	for i := start; i <= end; i++ {
		lines = append(lines, m.viewportLines[i])
	}
	return strings.TrimSpace(stripANSI(strings.Join(lines, "\n")))
}

func (m *model) copySelectionToClipboard() {
	text := m.selectedText()
	if text == "" {
		m.infoMessage = "No text selected."
		return
	}
	if err := clipboardWrite(text); err != nil {
		m.errorMessage = fmt.Sprintf("Clipboard copy failed: %v", err)
		return
	}
	m.errorMessage = ""
	m.infoMessage = "Selection copied to clipboard."
}

var ansiEscapeCodes = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(text string) string {
	return ansiEscapeCodes.ReplaceAllString(text, "")
}

var clipboardWrite = clipboard.WriteAll

func (m *model) scrollToTop() {
	m.viewport.SetYOffset(0)
	if m.lineCount > 0 {
		m.cursorLine = 0
		m.clearSelection()
		m.markViewportDirty()
		m.refreshViewportIfDirty()
	}
	m.infoMessage = "Jumped to top."
}

func (m *model) scrollToBottom() {
	totalLines := strings.Count(m.viewportContent, "\n")
	target := totalLines - m.viewport.Height + 1
	if target < 0 {
		target = 0
	}
	m.viewport.SetYOffset(target)
	if m.lineCount > 0 {
		m.cursorLine = m.lineCount - 1
		m.clearSelection()
		m.markViewportDirty()
		m.refreshViewportIfDirty()
	}
	m.infoMessage = "Jumped to bottom."
}

func (m *model) jumpToSection(anchor string) {
	if m.sectionAnchors == nil {
		m.infoMessage = "Load a paper to jump between sections."
		return
	}
	line, ok := m.sectionAnchors[anchor]
	if !ok {
		switch anchor {
		case anchorSummary:
			m.infoMessage = "Summary section unavailable."
		case anchorTechnical:
			m.infoMessage = "Technical section unavailable."
		case anchorDeepDive:
			m.infoMessage = "Deep-dive section unavailable."
		default:
			m.infoMessage = "Section unavailable."
		}
		return
	}
	if line < 0 {
		line = 0
	}
	m.viewport.SetYOffset(line)
	m.cursorLine = line
	m.clearSelection()
	m.markViewportDirty()
	m.refreshViewportIfDirty()
	m.infoMessage = fmt.Sprintf("Jumped to %s.", sectionLabel(anchor))
}

func (m *model) resetBriefState() {
	if len(m.briefStreamCancels) > 0 {
		for _, cancel := range m.briefStreamCancels {
			cancel()
		}
	}
	m.brief = llm.ReadingBrief{}
	m.briefSections = map[llm.BriefSectionKind]briefSectionState{}
	for _, kind := range briefSectionKinds {
		m.briefSections[kind] = briefSectionState{}
	}
	m.briefFallbacks = nil
	m.briefContexts = nil
	m.briefChunks = nil
	m.briefStreamCancels = map[llm.BriefSectionKind]context.CancelFunc{}
	m.briefLoading = false
	m.briefMessageIndex = nil
}

func (m *model) prepareBriefFallbacks() {
	if m.paper == nil {
		m.briefFallbacks = nil
		return
	}
	fallbacks := map[llm.BriefSectionKind][]string{}
	if summary := fallbackSummaryBullets(m.paper.Abstract); len(summary) > 0 {
		fallbacks[llm.BriefSummary] = summary
	}
	if technical := fallbackTechnicalBullets(m.paper.KeyContributions, m.paper.Abstract); len(technical) > 0 {
		fallbacks[llm.BriefTechnical] = technical
	}
	if deepDive := fallbackDeepDiveBullets(m.paper); len(deepDive) > 0 {
		fallbacks[llm.BriefDeepDive] = deepDive
	}
	if len(fallbacks) == 0 {
		m.briefFallbacks = nil
		return
	}
	m.briefFallbacks = fallbacks
}

func (m *model) ensureBriefSections() {
	if m.briefSections == nil {
		m.briefSections = map[llm.BriefSectionKind]briefSectionState{}
	}
	for _, kind := range briefSectionKinds {
		if _, ok := m.briefSections[kind]; !ok {
			m.briefSections[kind] = briefSectionState{}
		}
	}
}

func (m *model) markBriefSectionRunning(kind llm.BriefSectionKind) {
	m.ensureBriefSections()
	state := m.briefSections[kind]
	state.Loading = true
	state.Error = ""
	m.briefSections[kind] = state
	m.briefLoading = true
}

func (m *model) markBriefSectionResult(kind llm.BriefSectionKind, err error) briefSectionState {
	m.ensureBriefSections()
	state := m.briefSections[kind]
	state.Loading = false
	if err != nil {
		state.Error = err.Error()
		state.Completed = false
	} else {
		state.Error = ""
		state.Completed = true
	}
	m.briefSections[kind] = state
	m.briefLoading = m.anyBriefSectionLoading()
	return state
}

func (m *model) anyBriefSectionLoading() bool {
	if m.briefSections == nil {
		return false
	}
	for _, state := range m.briefSections {
		if state.Loading {
			return true
		}
	}
	return false
}

func (m *model) sectionState(kind llm.BriefSectionKind) briefSectionState {
	if m.briefSections == nil {
		return briefSectionState{}
	}
	return m.briefSections[kind]
}

func (m *model) updateBriefContent(kind llm.BriefSectionKind, bullets []string) {
	switch kind {
	case llm.BriefSummary:
		m.brief.Summary = bullets
	case llm.BriefTechnical:
		m.brief.Technical = bullets
	case llm.BriefDeepDive:
		m.brief.DeepDive = bullets
	}
}

var briefListPrefixPattern = regexp.MustCompile(`^\s*(?:[-*+]|[0-9]+[.)])\s+`)

func briefMessageContent(kind llm.BriefSectionKind, bullets []string) string {
	return briefMessageContentWithNotice(kind, bullets, "")
}

func briefMessageContentWithNotice(kind llm.BriefSectionKind, bullets []string, notice string) string {
	title := briefSectionTitle(kind)
	if len(bullets) == 0 && strings.TrimSpace(notice) == "" {
		return fmt.Sprintf("%s ready.", title)
	}
	lines := []string{fmt.Sprintf("### %s", title)}
	if trimmed := strings.TrimSpace(notice); trimmed != "" {
		lines = append(lines, fmt.Sprintf("> %s", trimmed))
	}
	for _, bullet := range bullets {
		trimmed := strings.TrimSpace(briefListPrefixPattern.ReplaceAllString(bullet, ""))
		if trimmed == "" {
			continue
		}
		lines = append(lines, "- "+trimmed)
	}
	return strings.Join(lines, "\n")
}

func briefMessageKind(content string) (llm.BriefSectionKind, bool) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "###") {
			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			switch strings.ToLower(heading) {
			case strings.ToLower(briefSectionTitle(llm.BriefSummary)):
				return llm.BriefSummary, true
			case strings.ToLower(briefSectionTitle(llm.BriefTechnical)):
				return llm.BriefTechnical, true
			case strings.ToLower(briefSectionTitle(llm.BriefDeepDive)):
				return llm.BriefDeepDive, true
			}
		}
	}
	trimmed := strings.TrimSpace(content)
	for _, kind := range briefSectionKinds {
		title := briefSectionTitle(kind)
		if strings.HasPrefix(strings.ToLower(trimmed), strings.ToLower(title+" ready")) {
			return kind, true
		}
	}
	return llm.BriefSummary, false
}

func briefSectionTitle(kind llm.BriefSectionKind) string {
	switch kind {
	case llm.BriefSummary:
		return "Summary"
	case llm.BriefTechnical:
		return "Technical"
	case llm.BriefDeepDive:
		return "Deep Dive"
	default:
		return "Summary"
	}
}

func briefSectionAnchor(kind llm.BriefSectionKind) string {
	switch kind {
	case llm.BriefSummary:
		return anchorSummary
	case llm.BriefTechnical:
		return anchorTechnical
	case llm.BriefDeepDive:
		return anchorDeepDive
	default:
		return anchorSummary
	}
}

func (m *model) briefBullets(kind llm.BriefSectionKind) []string {
	switch kind {
	case llm.BriefSummary:
		return m.brief.Summary
	case llm.BriefTechnical:
		return m.brief.Technical
	case llm.BriefDeepDive:
		return m.brief.DeepDive
	default:
		return nil
	}
}

func (m *model) pendingBriefNotice() string {
	if m.config.LLM == nil {
		return "Configure an LLM provider to generate this section."
	}
	if m.paper != nil && strings.TrimSpace(m.paper.FullText) == "" {
		return "PDF text missing; brief generation skipped."
	}
	return "Awaiting LLM output."
}

func jobKindForSection(kind llm.BriefSectionKind) jobKind {
	switch kind {
	case llm.BriefSummary:
		return jobKindBriefSummary
	case llm.BriefTechnical:
		return jobKindBriefTechnical
	case llm.BriefDeepDive:
		return jobKindBriefDeepDive
	default:
		return jobKindBriefSummary
	}
}

func (m *model) fallbackForSection(kind llm.BriefSectionKind) []string {
	if m.briefFallbacks == nil {
		return nil
	}
	return m.briefFallbacks[kind]
}

func (m *model) mapBriefMessages() {
	if len(m.transcriptEntries) == 0 {
		m.briefMessageIndex = nil
		return
	}
	if m.briefMessageIndex == nil {
		m.briefMessageIndex = map[llm.BriefSectionKind]int{}
	}
	for idx, entry := range m.transcriptEntries {
		if entry.Kind != "brief" {
			continue
		}
		if kind, ok := briefMessageKind(entry.Content); ok {
			m.briefMessageIndex[kind] = idx
		}
	}
}

func (m *model) setBriefMessage(kind llm.BriefSectionKind, content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	if m.briefMessageIndex == nil {
		m.briefMessageIndex = map[llm.BriefSectionKind]int{}
	}
	if idx, ok := m.briefMessageIndex[kind]; ok && idx >= 0 && idx < len(m.transcriptEntries) {
		entry := &m.transcriptEntries[idx]
		entry.Kind = "brief"
		entry.Content = content
		entry.Timestamp = time.Now()
		m.markTranscriptDirty()
		m.markViewportDirty()
		return
	}
	idx := m.appendTranscriptEntry("brief", content)
	m.briefMessageIndex[kind] = idx
}

func (m *model) seedBriefMessages() {
	if m.paper == nil {
		return
	}
	m.mapBriefMessages()
	for _, kind := range briefSectionKinds {
		if m.briefMessageIndex != nil {
			if _, ok := m.briefMessageIndex[kind]; ok {
				continue
			}
		}
		bullets := m.briefBullets(kind)
		notice := ""
		if len(bullets) == 0 {
			if fallback := m.fallbackForSection(kind); len(fallback) > 0 {
				bullets = fallback
				notice = fallbackNotice(kind)
			} else {
				notice = m.pendingBriefNotice()
			}
		}
		m.setBriefMessage(kind, briefMessageContentWithNotice(kind, bullets, notice))
	}
}

func draftAnswerForQuestion(paper *arxiv.Paper) string {
	if paper == nil {
		return ""
	}
	if summary := fallbackSummaryBullets(paper.Abstract); len(summary) > 0 {
		if len(summary) > 2 {
			summary = summary[:2]
		}
		return strings.Join(summary, " ")
	}
	if len(paper.KeyContributions) > 0 {
		limit := 2
		if len(paper.KeyContributions) < limit {
			limit = len(paper.KeyContributions)
		}
		return strings.Join(paper.KeyContributions[:limit], " ")
	}
	return ""
}

func fallbackSummaryBullets(abstract string) []string {
	sentences := abstractSentences(abstract)
	var bullets []string
	for _, sentence := range sentences {
		text := strings.TrimSpace(sentence)
		if text == "" {
			continue
		}
		if !strings.HasSuffix(text, ".") && !strings.HasSuffix(text, "!") && !strings.HasSuffix(text, "?") {
			text += "."
		}
		bullets = append(bullets, text)
		if len(bullets) == 3 {
			break
		}
	}
	if len(bullets) == 0 && strings.TrimSpace(abstract) != "" {
		bullets = []string{strings.TrimSpace(abstract)}
	}
	return bullets
}

func fallbackTechnicalBullets(contributions []string, abstract string) []string {
	var bullets []string
	for _, entry := range contributions {
		text := strings.TrimSpace(entry)
		if text == "" {
			continue
		}
		bullets = append(bullets, text)
		if len(bullets) == 3 {
			break
		}
	}
	if len(bullets) == 0 {
		return fallbackSummaryBullets(abstract)
	}
	return bullets
}

func fallbackDeepDiveBullets(paper *arxiv.Paper) []string {
	if paper == nil {
		return nil
	}
	var bullets []string
	if len(paper.Subjects) > 0 {
		bullets = append(bullets, fmt.Sprintf("Focus areas: %s", strings.Join(paper.Subjects, ", ")))
	}
	if len(paper.Authors) > 0 {
		bullets = append(bullets, fmt.Sprintf("Authors to explore: %s", shortenList(paper.Authors, 4)))
	}
	switch {
	case paper.ID != "":
		bullets = append(bullets, fmt.Sprintf("arXiv entry: https://arxiv.org/abs/%s", paper.ID))
	case paper.PDFURL != "":
		bullets = append(bullets, fmt.Sprintf("Source PDF: %s", paper.PDFURL))
	}
	return bullets
}

func fallbackNotice(kind llm.BriefSectionKind) string {
	switch kind {
	case llm.BriefSummary:
		return "Provisional summary from the arXiv abstract."
	case llm.BriefTechnical:
		return "Provisional technical notes derived from key contributions."
	case llm.BriefDeepDive:
		return "Provisional metadata references while the PDF brief loads."
	default:
		return "Provisional content derived from arXiv metadata."
	}
}

func abstractSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var sentences []string
	start := 0
	for idx, r := range text {
		if r == '.' || r == '!' || r == '?' {
			end := idx + utf8.RuneLen(r)
			segment := strings.TrimSpace(text[start:end])
			if segment != "" {
				sentences = append(sentences, segment)
			}
			start = end
			for start < len(text) {
				nextRune, size := utf8.DecodeRuneInString(text[start:])
				if unicode.IsSpace(nextRune) {
					start += size
					continue
				}
				break
			}
		}
	}
	if start < len(text) {
		if segment := strings.TrimSpace(text[start:]); segment != "" {
			sentences = append(sentences, segment)
		}
	}
	return sentences
}

func waitBriefSectionStream(paperID string, kind llm.BriefSectionKind, updates <-chan llm.BriefSectionDelta) tea.Cmd {
	if updates == nil {
		return nil
	}
	return func() tea.Msg {
		delta, ok := <-updates
		if !ok {
			return nil
		}
		return briefSectionStreamMsg{
			paperID: paperID,
			kind:    kind,
			bullets: append([]string(nil), delta.Bullets...),
			done:    delta.Done,
			updates: updates,
		}
	}
}

func (m *model) ensureBriefContexts() map[llm.BriefSectionKind]string {
	if m.paper == nil {
		return nil
	}
	if len(m.briefContexts) == 0 {
		builder := briefctx.NewBuilder(nil)
		pkg := builder.Build(m.paper.FullText)
		m.briefContexts = pkg.Sections
		m.briefChunks = pkg.Chunks
	}
	return m.briefContexts
}

func (m *model) contextForSection(kind llm.BriefSectionKind) string {
	contexts := m.ensureBriefContexts()
	if contexts == nil {
		return ""
	}
	return contexts[kind]
}

func (m *model) launchBriefSections() tea.Cmd {
	if m.paper == nil || m.config.LLM == nil {
		return nil
	}
	cmds := []tea.Cmd{m.spinner.Tick}
	for _, kind := range briefSectionKinds {
		if m.briefStreamCancels == nil {
			m.briefStreamCancels = map[llm.BriefSectionKind]context.CancelFunc{}
		}
		if cancel, ok := m.briefStreamCancels[kind]; ok {
			cancel()
		}
		streamCtx, cancel := context.WithCancel(context.Background())
		m.briefStreamCancels[kind] = cancel
		m.markBriefSectionRunning(kind)
		ctx := m.contextForSection(kind)
		runner, updates := briefSectionJob(kind, ctx, m.config.LLM, m.paper, streamCtx)
		cmds = append(cmds, m.jobBus.Start(jobKindForSection(kind), runner))
		if streamCmd := waitBriefSectionStream(m.paper.ID, kind, updates); streamCmd != nil {
			cmds = append(cmds, streamCmd)
		}
	}
	m.markViewportDirty()
	return tea.Batch(cmds...)
}

func (m *model) actionSummarizeCmd() tea.Cmd {
	if m.paper == nil {
		m.infoMessage = "Load a paper before summarizing."
		return nil
	}
	if m.config.LLM == nil {
		m.infoMessage = "Configure Ollama via flags to enable summaries."
		return nil
	}
	if strings.TrimSpace(m.paper.FullText) == "" {
		m.infoMessage = "PDF text missing; cannot build the reading brief."
		return nil
	}
	if m.briefLoading {
		m.infoMessage = "Reading brief already running."
		return nil
	}
	m.infoMessage = "Generating LLM reading brief…"
	return m.launchBriefSections()
}

func (m *model) actionAskQuestionCmd() tea.Cmd {
	if m.paper == nil {
		m.infoMessage = "Load a paper before asking questions."
		return nil
	}
	if m.config.LLM == nil {
		m.infoMessage = "Configure Ollama to unlock questions."
		return nil
	}
	m.composer.SetValue("")
	m.setComposerMode(composerModeQuestion, composerQuestionPlaceholder, true)
	m.infoMessage = "Composer ready. Press Enter to submit."
	return nil
}

func (m *model) actionManualNoteCmd() tea.Cmd {
	if m.paper == nil {
		m.infoMessage = "Load a paper before drafting notes."
		return nil
	}
	m.startNoteEntry("")
	m.infoMessage = "Composer active. Press Ctrl+Enter to store the note."
	return nil
}

func (m *model) actionSaveCmd() tea.Cmd {
	notesToSave := m.collectSelectedNotes()
	if len(notesToSave) == 0 {
		m.infoMessage = "No manual notes captured yet."
		return nil
	}
	m.stage = stageSaving
	target := m.config.KnowledgeBasePath
	if strings.TrimSpace(target) == "" {
		target = "zettelkasten.json"
	}
	m.infoMessage = fmt.Sprintf("Saving notes to %s…", target)
	return tea.Batch(m.spinner.Tick, m.jobBus.Start(jobKindSave, saveNotesJob(m.config.KnowledgeBasePath, notesToSave)))
}

func (m *model) actionToggleHelpCmd() tea.Cmd {
	m.helpVisible = !m.helpVisible
	if m.helpVisible {
		m.infoMessage = "Navigation cheatsheet open. Press ? to hide."
	} else {
		m.infoMessage = "Navigation cheatsheet hidden."
	}
	m.markViewportDirty()
	return nil
}

func (m *model) actionLoadNewCmd() tea.Cmd {
	m.stage = stageInput
	m.paper = nil
	m.resetBriefState()
	m.cursorLine = 0
	m.guide = nil
	m.suggestions = nil
	m.manualNotes = nil
	m.persistedNotes = nil
	m.selected = map[int]bool{}
	m.persisted = map[int]bool{}
	m.viewport.SetContent("")
	m.suggestionLines = map[int]int{}
	m.sectionAnchors = map[string]int{}
	m.suggestionLoading = false
	m.pendingFocusAnchor = ""
	m.infoMessage = "Ready for another paper."
	m.markViewportDirty()
	m.composer.SetValue("")
	m.setComposerMode(composerModeURL, composerURLPlaceholder, true)
	m.clearSelection()
	return nil
}

func (m *model) availableCommands() []uiCommand {
	base := paletteCommands
	result := make([]uiCommand, 0, len(base))
	for _, cmd := range base {
		if m.commandAvailable(cmd.id) {
			result = append(result, cmd)
		}
	}
	return result
}

func (m *model) commandAvailable(id actionID) bool {
	switch id {
	case actionSummarize:
		return m.paper != nil && m.config.LLM != nil
	case actionAskQuestion:
		return m.paper != nil && m.config.LLM != nil
	case actionManualNote:
		return m.paper != nil
	case actionSaveNotes:
		return len(m.collectSelectedNotes()) > 0
	case actionToggleHelp:
		return true
	case actionLoadNew:
		return true
	default:
		return false
	}
}

func (m *model) runCommand(id actionID) tea.Cmd {
	switch id {
	case actionSummarize:
		return m.actionSummarizeCmd()
	case actionAskQuestion:
		return m.actionAskQuestionCmd()
	case actionManualNote:
		return m.actionManualNoteCmd()
	case actionSaveNotes:
		return m.actionSaveCmd()
	case actionToggleHelp:
		return m.actionToggleHelpCmd()
	case actionLoadNew:
		return m.actionLoadNewCmd()
	default:
		return nil
	}
}

func (m *model) appendTranscript(kind, content string) {
	m.appendTranscriptEntry(kind, content)
}

func (m *model) appendTranscriptEntry(kind, content string) int {
	entry := transcriptEntry{
		Kind:      kind,
		Content:   content,
		Timestamp: time.Now(),
	}
	m.transcriptEntries = append(m.transcriptEntries, entry)
	m.markTranscriptDirty()
	m.markViewportDirty()
	return len(m.transcriptEntries) - 1
}

func (m *model) openCommandPalette() {
	m.paletteReturnStage = m.stage
	m.stage = stagePalette
	m.paletteInput.SetValue("")
	m.paletteInput.Focus()
	m.paletteCursor = 0
	m.refreshPaletteMatches()
}

func (m *model) closeCommandPalette() {
	target := m.paletteReturnStage
	if target == stagePalette || target == 0 {
		target = stageDisplay
	}
	m.stage = target
	m.paletteInput.Blur()
	m.paletteInput.SetValue("")
	m.paletteMatches = nil
	m.paletteCursor = 0
	if target != stagePalette {
		m.composer.Focus()
	}
}

func (m *model) refreshPaletteMatches() {
	filter := strings.ToLower(strings.TrimSpace(m.paletteInput.Value()))
	commands := m.availableCommands()
	if filter == "" {
		m.paletteMatches = commands
		if m.paletteCursor >= len(m.paletteMatches) {
			m.paletteCursor = len(m.paletteMatches) - 1
		}
		if m.paletteCursor < 0 {
			m.paletteCursor = 0
		}
		return
	}
	var matches []uiCommand
	for _, cmd := range commands {
		title := strings.ToLower(cmd.title)
		desc := strings.ToLower(cmd.description)
		if strings.Contains(title, filter) || strings.Contains(desc, filter) {
			matches = append(matches, cmd)
		}
	}
	m.paletteMatches = matches
	if len(m.paletteMatches) == 0 {
		m.paletteCursor = 0
	} else if m.paletteCursor >= len(m.paletteMatches) {
		m.paletteCursor = len(m.paletteMatches) - 1
	}
}

func (m *model) ensureConversationSnapshotCmd() tea.Cmd {
	if m.paper == nil || m.config.KnowledgeBasePath == "" {
		return nil
	}
	return m.jobBus.Start(jobKindZettel, ensureConversationSnapshotJob(m.config.KnowledgeBasePath, m.paper))
}

func (m *model) appendConversationSnapshotCmd(update notes.SnapshotUpdate) tea.Cmd {
	if m.paper == nil || m.config.KnowledgeBasePath == "" {
		return nil
	}
	if len(update.Messages) == 0 && len(update.Notes) == 0 {
		return nil
	}
	return m.jobBus.Start(jobKindZettel, appendConversationSnapshotJob(m.config.KnowledgeBasePath, m.paper, update))
}

func (m *model) handlePaperResult(msg paperResultMsg) tea.Cmd {
	if msg.err != nil {
		m.stage = stageInput
		m.errorMessage = msg.err.Error()
		m.infoMessage = "Try another arXiv identifier."
		m.composer.SetValue("")
		m.setComposerMode(composerModeURL, composerURLPlaceholder, true)
		m.appendTranscript("error", fmt.Sprintf("Load failed: %v", msg.err))
		return nil
	}
	m.paper = msg.paper
	m.guide = msg.guide
	m.suggestions = nil
	m.stage = stageDisplay
	m.cursorLine = 0
	m.selected = map[int]bool{}
	m.persisted = map[int]bool{}
	m.manualNotes = []notes.Note{}
	m.persistedNotes = nil
	m.suggestionLines = map[int]int{}
	m.sectionAnchors = map[string]int{}
	m.resetBriefState()
	m.prepareBriefFallbacks()
	m.suggestionLoading = false
	m.qaHistory = nil
	m.queuedQuestions = nil
	m.questionLoading = false
	m.viewport.SetYOffset(0)
	m.clearSelection()
	m.pendingFocusAnchor = anchorSummary
	m.errorMessage = ""
	m.infoMessage = fmt.Sprintf("Loaded %s. Generating reading brief…", m.paper.Title)
	m.hydrateConversationHistory()
	m.refreshPersistedState()
	m.markViewportDirty()
	m.composer.SetValue("")
	m.setComposerMode(composerModeNote, composerNotePlaceholder, false)
	m.appendTranscript("paper", fmt.Sprintf("Loaded %s", m.paper.Title))
	m.seedBriefMessages()
	snapshotCmd := m.ensureConversationSnapshotCmd()

	if m.config.LLM == nil {
		m.infoMessage = fmt.Sprintf("Loaded %s. Configure an LLM provider to see the reading brief.", m.paper.Title)
		return snapshotCmd
	}
	if strings.TrimSpace(m.paper.FullText) == "" {
		m.infoMessage = fmt.Sprintf("Loaded %s. PDF text missing; skipping reading brief.", m.paper.Title)
		return snapshotCmd
	}
	m.infoMessage = fmt.Sprintf("Loaded %s. Building reading brief…", m.paper.Title)
	briefCmd := m.launchBriefSections()
	if snapshotCmd != nil {
		return tea.Batch(snapshotCmd, briefCmd)
	}
	return briefCmd
}

func (m *model) handleSaveResult(msg saveResultMsg) tea.Cmd {
	m.stage = stageDisplay
	if msg.err != nil {
		m.errorMessage = msg.err.Error()
		m.infoMessage = "Saving failed. Retry with s."
		m.appendTranscript("error", fmt.Sprintf("Save failed: %v", msg.err))
		return nil
	}
	if msg.count == 0 {
		m.infoMessage = "No manual notes captured yet."
		return nil
	}
	m.infoMessage = fmt.Sprintf("Saved %d note(s) to %s", msg.count, m.config.KnowledgeBasePath)
	m.errorMessage = ""
	m.selected = map[int]bool{}
	m.persisted = map[int]bool{}
	m.manualNotes = []notes.Note{}
	m.refreshPersistedState()
	m.markViewportDirty()
	m.appendTranscript("save", fmt.Sprintf("Saved %d note(s).", msg.count))
	return nil
}

func (m *model) handleBriefSectionResult(msg briefSectionMsg) tea.Cmd {
	if m.paper == nil || m.paper.ID != msg.paperID {
		return nil
	}
	state := m.markBriefSectionResult(msg.kind, msg.err)
	title := briefSectionTitle(msg.kind)
	var snapshotCmd tea.Cmd
	if msg.err != nil {
		state.Error = fmt.Sprintf("%s section error: %v", title, msg.err)
		m.briefSections[msg.kind] = state
		m.errorMessage = state.Error
		m.infoMessage = "Press a to retry the reading brief."
		m.appendTranscript("error", state.Error)
		snapshotCmd = m.appendConversationSnapshotCmd(notes.SnapshotUpdate{
			SectionMetadata: []notes.BriefSectionMetadata{
				{
					Kind:   string(msg.kind),
					Status: "failed",
					Error:  msg.err.Error(),
				},
			},
		})
	} else {
		m.updateBriefContent(msg.kind, msg.bullets)
		m.errorMessage = ""
		if m.briefLoading {
			m.infoMessage = fmt.Sprintf("%s section ready. Waiting on remaining sections…", title)
		} else {
			m.infoMessage = "Reading brief ready."
		}
		m.setBriefMessage(msg.kind, briefMessageContent(msg.kind, msg.bullets))
		update := notes.SnapshotUpdate{
			SectionMetadata: []notes.BriefSectionMetadata{
				{Kind: string(msg.kind), Status: "completed"},
			},
		}
		if len(msg.bullets) > 0 {
			bullets := append([]string(nil), msg.bullets...)
			switch msg.kind {
			case llm.BriefSummary:
				update.Brief = &notes.BriefSnapshot{Summary: bullets}
			case llm.BriefTechnical:
				update.Brief = &notes.BriefSnapshot{Technical: bullets}
			case llm.BriefDeepDive:
				update.Brief = &notes.BriefSnapshot{DeepDive: bullets}
			}
		}
		snapshotCmd = m.appendConversationSnapshotCmd(update)
	}
	m.markViewportDirty()
	queuedCmd := m.maybeStartQueuedQuestion()
	if snapshotCmd != nil && queuedCmd != nil {
		return tea.Batch(snapshotCmd, queuedCmd)
	}
	if snapshotCmd != nil {
		return snapshotCmd
	}
	return queuedCmd
}

func (m *model) handleBriefSectionStream(msg briefSectionStreamMsg) tea.Cmd {
	if m.paper == nil || m.paper.ID != msg.paperID {
		return nil
	}
	if len(msg.bullets) > 0 {
		m.updateBriefContent(msg.kind, msg.bullets)
		m.setBriefMessage(msg.kind, briefMessageContent(msg.kind, msg.bullets))
	} else if msg.done {
		m.setBriefMessage(msg.kind, briefMessageContent(msg.kind, nil))
	}
	if msg.done {
		return nil
	}
	return waitBriefSectionStream(msg.paperID, msg.kind, msg.updates)
}

func (m *model) handleQuestionResult(msg questionResultMsg) tea.Cmd {
	if m.paper == nil || m.paper.ID != msg.paperID {
		return nil
	}
	m.questionLoading = false
	var snapshotCmd tea.Cmd
	if msg.index >= 0 && msg.index < len(m.qaHistory) {
		entry := &m.qaHistory[msg.index]
		entry.Pending = false
		if msg.err != nil {
			entry.Error = msg.err.Error()
			m.errorMessage = entry.Error
			if entry.Answer != "" {
				m.infoMessage = "Question failed; draft may be incomplete."
			} else {
				m.infoMessage = "Question failed. Press q to retry."
			}
			m.appendTranscript("error", fmt.Sprintf("Question failed: %v", msg.err))
		} else {
			entry.Answer = msg.answer
			entry.Error = ""
			m.errorMessage = ""
			m.infoMessage = "Answer refined. Ask another with q."
			if entry.TranscriptIndex >= 0 && entry.TranscriptIndex < len(m.transcriptEntries) {
				transcript := &m.transcriptEntries[entry.TranscriptIndex]
				transcript.Kind = "answer"
				transcript.Content = msg.answer
				transcript.Timestamp = time.Now()
				m.markTranscriptDirty()
				m.markViewportDirty()
			} else {
				m.appendTranscript("answer", msg.answer)
			}
			snapshotCmd = m.appendConversationSnapshotCmd(notes.SnapshotUpdate{
				Messages: []notes.ConversationMessage{
					{
						Kind:      "answer",
						Content:   msg.answer,
						Timestamp: time.Now(),
					},
				},
			})
		}
	}
	m.markViewportDirty()
	queuedCmd := m.maybeStartQueuedQuestion()
	if snapshotCmd != nil && queuedCmd != nil {
		return tea.Batch(snapshotCmd, queuedCmd)
	}
	if snapshotCmd != nil {
		return snapshotCmd
	}
	return queuedCmd
}

func (m *model) handleSuggestionResult(msg suggestionResultMsg) tea.Cmd {
	if m.paper == nil || m.paper.ID != msg.paperID {
		return nil
	}
	m.suggestionLoading = false
	if msg.err != nil {
		m.errorMessage = fmt.Sprintf("suggestion error: %v", msg.err)
		m.infoMessage = "Note suggestions are unavailable in this view."
		m.markViewportDirty()
		m.appendTranscript("error", fmt.Sprintf("Suggestion failed: %v", msg.err))
		return nil
	}
	m.errorMessage = ""
	m.infoMessage = "Note suggestions are unavailable in this view."
	m.suggestions = nil
	m.selected = map[int]bool{}
	m.persisted = map[int]bool{}
	m.refreshPersistedState()
	m.markViewportDirty()
	return nil
}

func (m *model) handleJobPayload(payload tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := payload.(type) {
	case paperResultMsg:
		return m, m.handlePaperResult(msg)
	case saveResultMsg:
		return m, m.handleSaveResult(msg)
	case briefSectionMsg:
		return m, m.handleBriefSectionResult(msg)
	case questionResultMsg:
		return m, m.handleQuestionResult(msg)
	case suggestionResultMsg:
		return m, m.handleSuggestionResult(msg)
	default:
		return m, nil
	}
}

func (m *model) recordJobSnapshot(snapshot jobSnapshot) {
	if m.jobSnapshots == nil {
		m.jobSnapshots = map[jobKind]jobSnapshot{}
	}
	m.jobSnapshots[snapshot.Kind] = snapshot
}

func (m *model) jobStatusBadges() []string {
	if len(m.jobSnapshots) == 0 {
		return nil
	}
	order := []jobKind{
		jobKindFetch,
		jobKindZettel,
		jobKindBriefSummary,
		jobKindBriefTechnical,
		jobKindBriefDeepDive,
		jobKindSuggest,
		jobKindSave,
		jobKindQuestion,
	}
	var badges []string
	for _, kind := range order {
		snapshot, ok := m.jobSnapshots[kind]
		if !ok {
			continue
		}
		switch snapshot.Status {
		case jobStatusRunning:
			elapsed := time.Since(snapshot.StartedAt)
			badges = append(badges, fmt.Sprintf("%s ▶ %s", jobKindLabel(kind), humanDuration(elapsed)))
		case jobStatusSucceeded:
			badges = append(badges, fmt.Sprintf("%s ✓ %s", jobKindLabel(kind), humanDuration(snapshot.Duration)))
		case jobStatusFailed:
			label := fmt.Sprintf("%s ✗ %s", jobKindLabel(kind), humanDuration(snapshot.Duration))
			if snapshot.Err != "" {
				label = fmt.Sprintf("%s (%s)", label, snapshot.Err)
			}
			badges = append(badges, label)
		}
	}
	return badges
}

func jobKindLabel(kind jobKind) string {
	switch kind {
	case jobKindFetch:
		return "fetch"
	case jobKindBriefSummary:
		return "brief:summary"
	case jobKindBriefTechnical:
		return "brief:technical"
	case jobKindBriefDeepDive:
		return "brief:deepdive"
	case jobKindSuggest:
		return "suggest"
	case jobKindSave:
		return "save"
	case jobKindZettel:
		return "zettel"
	case jobKindQuestion:
		return "qa"
	default:
		return string(kind)
	}
}

func humanDuration(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d <= 0 {
		return "0ms"
	}
	ms := d.Milliseconds()
	if ms == 0 {
		ms = 1
	}
	return fmt.Sprintf("%dms", ms)
}

func (m *model) clampYOffset(offset int) int {
	maxOffset := m.lineCount - m.viewport.Height
	if m.viewport.Height <= 0 {
		maxOffset = m.lineCount - 1
	}
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset < 0 {
		return 0
	}
	if offset > maxOffset {
		return maxOffset
	}
	return offset
}

var (
	titleStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Underline(true)
	subtitleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("147"))
	sectionHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	subjectStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("110"))
	errorStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	helperStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	heroAccentColor        = lipgloss.Color("#ff8c00")
	heroEmberColor         = lipgloss.Color("#2b1400")
	heroTextColor          = lipgloss.Color("#fff4d0")
	heroSecondaryTextColor = lipgloss.Color("#ffb347")

	composerFocusedBackgroundColor = lipgloss.Color("#231507")
	composerBlurredBackgroundColor = lipgloss.Color("#170c04")
	composerCursorLineFocusedColor = lipgloss.Color("#3b200b")
	composerCursorLineBlurredColor = lipgloss.Color("#251307")

	heroTitleStyle                 = lipgloss.NewStyle().Bold(true).Foreground(heroAccentColor)
	heroBoxStyle                   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(heroAccentColor).Foreground(heroTextColor).Background(heroEmberColor).Padding(1, 2)
	heroSummaryStyle               = lipgloss.NewStyle().PaddingLeft(2)
	taglineStyle                   = lipgloss.NewStyle().Foreground(heroSecondaryTextColor).Italic(true)
	statusBarStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("#0f0f0f")).Background(lipgloss.Color("#8ecae6")).Padding(0, 1)
	keyStyle                       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#0f0f0f")).Background(lipgloss.Color("#ffd166")).Padding(0, 1)
	keyDescStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0def4"))
	legendBoxStyle                 = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#56526e")).Padding(1, 2)
	helpBoxStyle                   = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("#7f5af0")).Padding(1, 2)
	currentLineStyle               = lipgloss.NewStyle().Foreground(lipgloss.Color("#0f0f0f")).Background(lipgloss.Color("#8ecae6"))
	persistedSuggestionStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#a3be8c")).Italic(true)
	logoFaceStyle                  = lipgloss.NewStyle().Bold(true).Foreground(heroTextColor).Background(heroEmberColor)
	logoShadowStyle                = lipgloss.NewStyle().Foreground(lipgloss.Color("#110600"))
	logoContainerStyle             = lipgloss.NewStyle().Padding(0, 1)
	composerFocusedBaseStyle       = lipgloss.NewStyle().Background(composerFocusedBackgroundColor)
	composerBlurredBaseStyle       = lipgloss.NewStyle().Background(composerBlurredBackgroundColor)
	composerCursorLineFocusedStyle = lipgloss.NewStyle().
					Background(composerCursorLineFocusedColor).
					Foreground(heroTextColor)
	composerCursorLineBlurredStyle = lipgloss.NewStyle().
					Background(composerCursorLineBlurredColor).
					Foreground(heroSecondaryTextColor)
	composerFocusedTextStyle = lipgloss.NewStyle().Foreground(heroTextColor)
	composerBlurredTextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d3b38a"))
	composerPlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f1c27a")).Italic(true)
	composerPromptStyle      = lipgloss.NewStyle().Foreground(heroAccentColor).Bold(true)
	markdownHeadingStyle     = lipgloss.NewStyle().Foreground(heroAccentColor).Bold(true)
	markdownBulletStyle      = lipgloss.NewStyle().Foreground(heroSecondaryTextColor).Bold(true)
	markdownTableStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#d9b56c"))
	markdownTableHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f2c97d")).Bold(true)
	markdownQuoteStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#b0a08a")).Italic(true)
	markdownCodeStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#d3b38a"))

	logoArtLines = []string{
		"██████╗    █████╗   ██████╗   ███████╗  ██████╗   ███████╗   ██████╗   ██████╗   ██╗   ██╗  ████████╗  ",
		"██╔══██╗  ██╔══██╗  ██╔══██╗  ██╔════╝  ██╔══██╗  ██╔════╝  ██╔════╝  ██╔═══██╗  ██║   ██║  ╚══██╔══╝  ",
		"██████╔╝  ███████║  ██████╔╝  █████╗    ██████╔╝  ███████╗  ██║       ██║   ██║  ██║   ██║     ██║     ",
		"██╔═══╝   ██╔══██║  ██╔═══╝   ██╔══╝    ██╔══██╗  ╚════██║  ██║       ██║   ██║  ██║   ██║     ██║     ",
		"██║       ██║  ██║  ██║       ███████╗  ██║  ██║  ███████║  ╚██████╗  ╚██████╔╝  ╚██████╔╝     ██║     ",
		"╚═╝       ╚═╝  ╚═╝  ╚═╝       ╚══════╝  ╚═╝  ╚═╝  ╚══════╝   ╚═════╝   ╚═════╝    ╚═════╝      ╚═╝     ",
	}
)
