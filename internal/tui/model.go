package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"

	"github.com/csheth/browse/internal/arxiv"
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
	urlInput := textinput.New()
	urlInput.Placeholder = "https://arxiv.org/abs/2101.00001"
	urlInput.Focus()
	urlInput.CharLimit = 120
	urlInput.Width = 70

	noteInput := textinput.New()
	noteInput.CharLimit = 280
	noteInput.Width = 70

	searchInput := textinput.New()
	searchInput.Placeholder = "Search within the current paper…"
	searchInput.CharLimit = 120
	searchInput.Width = 60

	questionInput := textinput.New()
	questionInput.Placeholder = "Ask a question about the paper…"
	questionInput.CharLimit = 200
	questionInput.Width = 70

	spin := spinner.New()
	spin.Spinner = spinner.Dot

	vp := viewport.New(80, 20)
	vp.MouseWheelEnabled = true

	return &model{
		config:             config,
		stage:              stageInput,
		mode:               modeNormal,
		urlInput:           urlInput,
		noteInput:          noteInput,
		searchInput:        searchInput,
		questionInput:      questionInput,
		spinner:            spin,
		viewport:           vp,
		selected:           map[int]bool{},
		persisted:          map[int]bool{},
		suggestionLines:    map[int]int{},
		cursorLine:         0,
		searchMatchIdx:     -1,
		viewportDirty:      true,
		infoMessage:        "Paste an arXiv url or identifier to begin.",
		sectionAnchors:     map[string]int{},
		pendingFocusAnchor: "",
	}
}

type stage int

const (
	stageInput stage = iota
	stageLoading
	stageDisplay
	stageNoteEntry
	stageSearch
	stageQuestion
	stageSaving
)

const (
	anchorContributions = "contributions"
	anchorGuide         = "guide"
	anchorSummary       = "summary"
	anchorSuggestions   = "suggestions"
	anchorSaved         = "saved"
	anchorManual        = "manual"
	anchorQA            = "qa"
)

var sectionSequence = []string{
	anchorContributions,
	anchorGuide,
	anchorSummary,
	anchorSuggestions,
	anchorSaved,
	anchorManual,
	anchorQA,
}

const heroTagline = "Navigate arXiv findings with PaperScout."

const (
	minViewportWidth          = 40
	viewportHorizontalPadding = 4
)

type interactionMode int

const (
	modeNormal interactionMode = iota
	modeInsert
	modeHighlight
)

type model struct {
	config Config
	stage  stage

	urlInput      textinput.Model
	noteInput     textinput.Model
	searchInput   textinput.Model
	questionInput textinput.Model
	spinner       spinner.Model
	viewport      viewport.Model

	paper              *arxiv.Paper
	guide              []guide.Step
	suggestions        []notes.Candidate
	selected           map[int]bool
	persisted          map[int]bool
	mode               interactionMode
	cursorLine         int
	lineCount          int
	manualNotes        []notes.Note
	persistedNotes     []notes.Note
	suggestionLines    map[int]int
	viewportLines      []string
	viewportContent    string
	viewportDirty      bool
	searchQuery        string
	searchMatches      []matchRange
	searchMatchIdx     int
	infoMessage        string
	errorMessage       string
	helpVisible        bool
	sectionAnchors     map[string]int
	summary            string
	summaryLoading     bool
	suggestionLoading  bool
	qaHistory          []qaExchange
	questionLoading    bool
	selectionAnchor    int
	selectionActive    bool
	pendingFocusAnchor string
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

type summaryResultMsg struct {
	paperID string
	summary string
	err     error
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

type qaExchange struct {
	Question string
	Answer   string
	Error    string
	Pending  bool
	AskedAt  time.Time
}

func (m *model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.stage == stageLoading || m.stage == stageSaving || m.summaryLoading || m.questionLoading || m.suggestionLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			switch m.stage {
			case stageNoteEntry:
				m.stage = stageDisplay
				m.mode = modeNormal
				m.selectionActive = false
				m.noteInput.SetValue("")
				m.infoMessage = "Insert mode canceled."
				m.markViewportDirty()
				return m, nil
			case stageSearch:
				m.stage = stageDisplay
				m.searchInput.Blur()
				return m, nil
			case stageQuestion:
				m.stage = stageDisplay
				m.questionInput.SetValue("")
				m.questionInput.Blur()
				m.markViewportDirty()
				return m, nil
			case stageDisplay:
				if m.mode == modeHighlight {
					m.mode = modeNormal
					m.selectionActive = false
					m.infoMessage = "Highlight mode disabled."
					m.markViewportDirty()
					return m, nil
				}
				return m, tea.Quit
			default:
				return m, tea.Quit
			}
		}
		return m.handleKey(msg)
	case tea.MouseMsg:
		if m.stage == stageDisplay {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		return m, nil
	case paperResultMsg:
		if msg.err != nil {
			m.stage = stageInput
			m.errorMessage = msg.err.Error()
			m.infoMessage = "Try another arXiv identifier."
			return m, nil
		}
		m.paper = msg.paper
		m.guide = msg.guide
		m.suggestions = msg.suggestions
		m.stage = stageDisplay
		m.mode = modeNormal
		m.cursorLine = 0
		m.selected = map[int]bool{}
		m.persisted = map[int]bool{}
		m.manualNotes = []notes.Note{}
		m.persistedNotes = nil
		m.suggestionLines = map[int]int{}
		m.sectionAnchors = map[string]int{}
		m.summary = ""
		m.summaryLoading = false
		m.suggestionLoading = false
		m.qaHistory = nil
		m.questionLoading = false
		m.viewport.SetYOffset(0)
		m.selectionActive = false
		m.pendingFocusAnchor = anchorSummary
		m.clearSearch()
		m.errorMessage = ""
		m.infoMessage = fmt.Sprintf("Loaded %s. Press space to toggle note suggestions.", m.paper.Title)
		m.refreshPersistedState()
		m.markViewportDirty()
		if m.config.LLM != nil {
			var cmds []tea.Cmd
			needsSpinner := false
			if strings.TrimSpace(m.paper.FullText) != "" {
				m.summaryLoading = true
				cmds = append(cmds, summarizePaperCmd(m.config.LLM, m.paper))
				needsSpinner = true
			}
			m.suggestionLoading = true
			cmds = append(cmds, suggestNotesCmd(m.config.LLM, m.paper))
			needsSpinner = true
			if m.summaryLoading {
				m.infoMessage = fmt.Sprintf("Loaded %s. Summarizing & drafting LLM notes…", m.paper.Title)
			} else {
				m.infoMessage = fmt.Sprintf("Loaded %s. Drafting LLM note suggestions…", m.paper.Title)
			}
			if needsSpinner {
				cmds = append(cmds, m.spinner.Tick)
				return m, tea.Batch(cmds...)
			}
		}
		return m, nil
	case saveResultMsg:
		m.stage = stageDisplay
		if msg.err != nil {
			m.errorMessage = msg.err.Error()
			m.infoMessage = "Saving failed. Retry with s."
			return m, nil
		}
		if msg.count == 0 {
			m.infoMessage = "No notes selected. Toggle suggestions or add manual notes."
			return m, nil
		}
		m.infoMessage = fmt.Sprintf("Saved %d note(s) to %s", msg.count, m.config.KnowledgeBasePath)
		m.errorMessage = ""
		m.selected = map[int]bool{}
		m.persisted = map[int]bool{}
		m.manualNotes = []notes.Note{}
		m.refreshPersistedState()
		m.markViewportDirty()
		return m, nil
	case summaryResultMsg:
		if m.paper == nil || m.paper.ID != msg.paperID {
			return m, nil
		}
		m.summaryLoading = false
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("summary error: %v", msg.err)
			m.infoMessage = "Press a to retry summary generation."
		} else {
			m.summary = msg.summary
			m.errorMessage = ""
			m.infoMessage = "LLM summary ready."
		}
		m.markViewportDirty()
		return m, nil
	case questionResultMsg:
		if m.paper == nil || m.paper.ID != msg.paperID {
			return m, nil
		}
		m.questionLoading = false
		if msg.index >= 0 && msg.index < len(m.qaHistory) {
			entry := &m.qaHistory[msg.index]
			entry.Pending = false
			if msg.err != nil {
				entry.Error = msg.err.Error()
				entry.Answer = ""
				m.errorMessage = entry.Error
				m.infoMessage = "Question failed. Press q to retry."
			} else {
				entry.Answer = msg.answer
				entry.Error = ""
				m.errorMessage = ""
				m.infoMessage = "Answer stored. Ask another with q."
			}
		}
		m.markViewportDirty()
		return m, nil
	case suggestionResultMsg:
		if m.paper == nil || m.paper.ID != msg.paperID {
			return m, nil
		}
		m.suggestionLoading = false
		if msg.err != nil {
			m.errorMessage = fmt.Sprintf("suggestion error: %v", msg.err)
			m.infoMessage = "LLM suggestions failed; showing heuristics."
			m.markViewportDirty()
			return m, nil
		}
		m.errorMessage = ""
		m.infoMessage = "LLM suggestions ready."
		m.suggestions = msg.suggestions
		m.selected = map[int]bool{}
		m.persisted = map[int]bool{}
		m.refreshPersistedState()
		m.markViewportDirty()
		return m, nil
	case tea.WindowSizeMsg:
		newWidth := msg.Width - viewportHorizontalPadding
		if newWidth < minViewportWidth {
			newWidth = minViewportWidth
		}
		m.viewport.Width = newWidth
		height := msg.Height - 6
		if height < 5 {
			height = 5
		}
		m.viewport.Height = height
		m.markViewportDirty()
		return m, nil
	}
	return m, nil
}

func (m *model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.stage {
	case stageInput:
		var cmd tea.Cmd
		m.urlInput, cmd = m.urlInput.Update(key)
		if key.Type == tea.KeyEnter {
			url := strings.TrimSpace(m.urlInput.Value())
			if url == "" {
				m.errorMessage = "Enter an arXiv url or identifier."
				return m, cmd
			}
			m.stage = stageLoading
			m.errorMessage = ""
			m.infoMessage = "Fetching metadata…"
			return m, tea.Batch(cmd, m.spinner.Tick, fetchPaperCmd(url))
		}
		return m, cmd
	case stageLoading:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(key)
		return m, cmd
	case stageDisplay:
		return m.handleDisplayKey(key)
	case stageNoteEntry:
		var cmd tea.Cmd
		m.noteInput, cmd = m.noteInput.Update(key)
		if key.Type == tea.KeyEnter {
			value := strings.TrimSpace(m.noteInput.Value())
			m.noteInput.SetValue("")
			m.stage = stageDisplay
			m.mode = modeNormal
			m.selectionActive = false
			if value != "" {
				m.manualNotes = append(m.manualNotes, notes.Note{
					PaperID:    m.paper.ID,
					PaperTitle: m.paper.Title,
					Title:      trimmedTitle(value),
					Body:       value,
					Kind:       "manual",
					CreatedAt:  time.Now(),
				})
				m.infoMessage = fmt.Sprintf("Manual note added (%d total).", len(m.manualNotes))
				m.markViewportDirty()
			}
			return m, cmd
		}
		return m, cmd
	case stageSearch:
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(key)
		if key.Type == tea.KeyEnter {
			value := strings.TrimSpace(m.searchInput.Value())
			m.stage = stageDisplay
			m.applySearch(value)
			return m, cmd
		}
		return m, cmd
	case stageQuestion:
		var cmd tea.Cmd
		m.questionInput, cmd = m.questionInput.Update(key)
		if key.Type == tea.KeyEnter {
			value := strings.TrimSpace(m.questionInput.Value())
			m.questionInput.SetValue("")
			m.stage = stageDisplay
			if value == "" {
				m.infoMessage = "Ask a question or press Esc to cancel."
				return m, cmd
			}
			if m.config.LLM == nil || m.paper == nil {
				m.infoMessage = "Configure OpenAI or Ollama support to ask questions."
				return m, cmd
			}
			entry := qaExchange{
				Question: value,
				Pending:  true,
				AskedAt:  time.Now(),
			}
			m.qaHistory = append(m.qaHistory, entry)
			m.questionLoading = true
			m.infoMessage = "Answering question via LLM…"
			m.markViewportDirty()
			idx := len(m.qaHistory) - 1
			return m, tea.Batch(cmd, m.spinner.Tick, questionAnswerCmd(idx, m.config.LLM, m.paper, value))
		}
		return m, cmd
	case stageSaving:
		return m, nil
	default:
		return m, nil
	}
}

func (m *model) handleDisplayKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	handled := true
	switch key.String() {
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case " ":
		if idx, ok := m.suggestionAtCursor(); ok {
			if m.persisted[idx] {
				m.infoMessage = "Suggestion already saved; add a manual note for new thoughts."
			} else {
				m.selected[idx] = !m.selected[idx]
				m.markViewportDirty()
				m.refreshViewportIfDirty()
			}
		} else {
			m.infoMessage = "Move to a suggestion line to toggle selection."
		}
	case "v":
		m.toggleHighlightMode()
	case "i":
		return m.enterInsertModeFromCursor()
	case "a":
		if m.paper != nil {
			if m.config.LLM == nil {
				m.infoMessage = "Configure OpenAI or Ollama via flags to enable summaries."
				return m, nil
			}
			if m.summaryLoading {
				m.infoMessage = "Summary already running."
				return m, nil
			}
			m.summary = ""
			m.summaryLoading = true
			m.infoMessage = "Generating LLM summary…"
			m.markViewportDirty()
			return m, tea.Batch(m.spinner.Tick, summarizePaperCmd(m.config.LLM, m.paper))
		}
	case "q":
		if m.paper != nil {
			if m.config.LLM == nil {
				m.infoMessage = "Configure OpenAI or Ollama to unlock questions."
				return m, nil
			}
			m.stage = stageQuestion
			m.questionInput.Placeholder = "Ask about the loaded PDF…"
			m.questionInput.SetValue("")
			m.questionInput.Focus()
			return m, nil
		}
	case "m":
		if m.paper != nil {
			m.startNoteEntry("")
			m.infoMessage = "Insert mode active. Press Enter to store the note."
			return m, nil
		}
	case "/":
		if m.paper != nil {
			m.stage = stageSearch
			m.searchInput.Placeholder = "Search within the current paper…"
			m.searchInput.SetValue(m.searchQuery)
			m.searchInput.Focus()
			return m, nil
		}
	case "n":
		m.advanceSearch(1)
	case "N":
		m.advanceSearch(-1)
	case "g":
		m.scrollToTop()
	case "G":
		m.scrollToBottom()
	case "]":
		m.jumpToRelativeSection(1)
	case "[":
		m.jumpToRelativeSection(-1)
	case "r":
		m.stage = stageInput
		m.paper = nil
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
		m.clearSearch()
		m.infoMessage = "Ready for another paper."
		return m, nil
	case "s":
		notesToSave := m.collectSelectedNotes()
		if len(notesToSave) == 0 {
			m.infoMessage = "No notes selected. Toggle suggestions or add manual notes before saving."
			return m, nil
		}
		m.stage = stageSaving
		m.infoMessage = "Persisting notes…"
		return m, tea.Batch(m.spinner.Tick, saveNotesCmd(m.config.KnowledgeBasePath, notesToSave))
	case "?":
		m.helpVisible = !m.helpVisible
		if m.helpVisible {
			m.infoMessage = "Help overlay open. Press ? to hide."
		} else {
			m.infoMessage = "Help overlay hidden."
		}
		m.markViewportDirty()
		return m, nil
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

func (m *model) View() string {
	switch m.stage {
	case stageInput:
		return m.viewInput()
	case stageLoading:
		return m.viewLoading()
	case stageDisplay:
		return m.viewDisplay()
	case stageNoteEntry:
		return m.viewNoteEntry()
	case stageSearch:
		return m.viewSearch()
	case stageQuestion:
		return m.viewQuestion()
	case stageSaving:
		return m.viewSaving()
	default:
		return ""
	}
}

func (m *model) viewInput() string {
	var sections []string
	sections = append(sections, m.heroView())

	form := strings.Builder{}
	form.WriteString(sectionHeaderStyle.Render("Paste an arXiv URL or identifier"))
	form.WriteRune('\n')
	form.WriteString(m.urlInput.View())
	form.WriteRune('\n')
	form.WriteString(helperStyle.Render("Press Enter to fetch the paper metadata."))
	form.WriteRune('\n')
	form.WriteString(helperStyle.Render(m.infoMessage))
	if m.errorMessage != "" {
		form.WriteRune('\n')
		form.WriteString(errorStyle.Render(m.errorMessage))
	}
	sections = append(sections, form.String())
	return strings.Join(sections, "\n\n")
}

func (m *model) viewLoading() string {
	body := fmt.Sprintf("%s Fetching paper metadata…", m.spinner.View())
	return m.frameWithHero(body)
}

func (m *model) viewDisplay() string {
	if m.paper == nil {
		return m.viewInput()
	}
	m.refreshViewportIfDirty()
	parts := []string{m.heroView()}
	if meter := m.sessionMeterView(); meter != "" {
		parts = append(parts, meter)
	}
	parts = append(parts, m.viewport.View())
	if status := m.searchStatusLine(); status != "" {
		parts = append(parts, helperStyle.Render(status))
	}
	if m.errorMessage != "" {
		parts = append(parts, errorStyle.Render(m.errorMessage))
	}
	if m.infoMessage != "" {
		parts = append(parts, helperStyle.Render(m.infoMessage))
	}
	if legend := m.keyLegendView(); legend != "" {
		parts = append(parts, legend)
	}
	if m.helpVisible {
		parts = append(parts, m.helpView())
	}
	return joinNonEmpty(parts)
}

func (m *model) viewNoteEntry() string {
	b := strings.Builder{}
	b.WriteString(sectionHeaderStyle.Render("Add Manual Note"))
	b.WriteRune('\n')
	b.WriteString(m.noteInput.View())
	b.WriteRune('\n')
	b.WriteString(helperStyle.Render("Press Enter to store note, Esc to cancel."))
	return m.frameWithHero(b.String())
}

func (m *model) viewSaving() string {
	body := fmt.Sprintf("%s Saving notes to %s…", m.spinner.View(), m.config.KnowledgeBasePath)
	return m.frameWithHero(body)
}

func (m *model) viewSearch() string {
	var b strings.Builder
	b.WriteString(sectionHeaderStyle.Render("Search Current Session"))
	b.WriteRune('\n')
	b.WriteString(m.searchInput.View())
	b.WriteRune('\n')
	b.WriteString(helperStyle.Render("Press Enter to apply search, Esc to cancel."))
	return m.frameWithHero(b.String())
}

func (m *model) viewQuestion() string {
	var b strings.Builder
	b.WriteString(sectionHeaderStyle.Render("Ask the Paper"))
	b.WriteRune('\n')
	b.WriteString(m.questionInput.View())
	b.WriteRune('\n')
	if m.config.LLM == nil {
		b.WriteString(errorStyle.Render("Configure OpenAI or Ollama support to ask questions."))
	} else {
		b.WriteString(helperStyle.Render("Press Enter to submit, Esc to cancel."))
	}
	return m.frameWithHero(b.String())
}

func (m *model) heroView() string {
	logo := renderLogo()
	if m.paper == nil {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			logo,
			taglineStyle.Render(heroTagline),
		)
	}

	title := heroTitleStyle.Render(wordwrap.String(m.paper.Title, 48))
	meta := []string{helperStyle.Render(fmt.Sprintf("arXiv: %s", m.paper.ID))}
	if len(m.paper.Authors) > 0 {
		meta = append(meta, helperStyle.Render("Authors: "+shortenList(m.paper.Authors, 3)))
	}
	if len(m.paper.Subjects) > 0 {
		meta = append(meta, helperStyle.Render("Subjects: "+shortenList(m.paper.Subjects, 3)))
	}
	content := strings.Join(append([]string{title}, meta...), "\n")
	summary := heroBoxStyle.Render(content)
	panel := lipgloss.JoinHorizontal(lipgloss.Top, logo, heroSummaryStyle.Render(summary))
	return lipgloss.JoinVertical(lipgloss.Left, panel, taglineStyle.Render(heroTagline))
}

func (m *model) frameWithHero(body string) string {
	return joinNonEmpty([]string{m.heroView(), body})
}

func joinNonEmpty(parts []string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, "\n\n")
}

func (m *model) modeLabel() string {
	switch m.mode {
	case modeInsert:
		return "INSERT"
	case modeHighlight:
		return "HIGHLIGHT"
	default:
		return "NORMAL"
	}
}

func (m *model) sessionMeterView() string {
	stats := []string{
		fmt.Sprintf("Mode %s", m.modeLabel()),
		fmt.Sprintf("Suggestions %d", len(m.suggestions)),
		fmt.Sprintf("Selected %d", m.selectedCount()),
		fmt.Sprintf("Manual %d", len(m.manualNotes)),
		fmt.Sprintf("Saved %d", len(m.persistedNotes)),
	}
	if m.config.LLM != nil {
		stats = append(stats, fmt.Sprintf("Q&A %d", len(m.qaHistory)))
		switch {
		case m.summaryLoading || m.questionLoading:
			stats = append(stats, "LLM working…")
		case m.summary != "":
			stats = append(stats, "LLM summary ready")
		default:
			stats = append(stats, "LLM idle")
		}
	}
	return statusBarStyle.Render(strings.Join(stats, "  •  "))
}

type keyHint struct {
	Key         string
	Description string
}

func (m *model) keyLegendView() string {
	hints := []keyHint{
		{"↑/↓", "Move line cursor"},
		{"v", "Toggle highlight mode"},
		{"i", "Insert note / capture selection"},
		{"space", "Toggle suggestion"},
		{"[/]", "Prev/next section"},
		{"m", "Manual note"},
		{"a", "Regenerate summary"},
		{"q", "Ask question"},
		{"/", "Search"},
		{"n/N", "Next match"},
		{"g/G", "Top or bottom"},
		{"s", "Save notes"},
		{"?", "Toggle help"},
		{"r", "Load new URL"},
	}
	rows := []string{sectionHeaderStyle.Render("Navigation Cheatsheet")}
	const columns = 3
	for i := 0; i < len(hints); i += columns {
		end := i + columns
		if end > len(hints) {
			end = len(hints)
		}
		var cells []string
		for _, hint := range hints[i:end] {
			key := keyStyle.Render(hint.Key)
			desc := keyDescStyle.Render(" " + hint.Description)
			cells = append(cells, lipgloss.JoinHorizontal(lipgloss.Top, key, desc))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}
	return legendBoxStyle.Render(strings.Join(rows, "\n"))
}

func (m *model) helpView() string {
	lines := []string{
		sectionHeaderStyle.Render("Command Palette"),
		helperStyle.Render("• space toggles the suggestion that shares the highlighted line; ✓ items already live in your zettelkasten."),
		helperStyle.Render("• press v to enter highlight mode, move to expand the selection, then press i to draft a note you can edit before saving."),
		helperStyle.Render("• use [ and ] to jump to the previous or next section, and g / G to fly to the top or bottom."),
		helperStyle.Render("• / opens search, n / N cycles matches, and Esc exits overlays."),
		helperStyle.Render("• press a to regenerate the LLM summary and q to ask questions once OpenAI or Ollama is configured."),
		helperStyle.Render("• press i or m for manual notes, s to persist, r to paste a new URL, Ctrl+C to quit."),
	}
	return helpBoxStyle.Render(strings.Join(lines, "\n"))
}

func renderLogo() string {
	if len(logoArtLines) == 0 {
		return ""
	}
	width := 0
	lineRunes := make([][]rune, len(logoArtLines))
	for i, line := range logoArtLines {
		runes := []rune(line)
		lineRunes[i] = runes
		if len(runes) > width {
			width = len(runes)
		}
	}
	width += 1 // allow horizontal shadow shift
	height := len(logoArtLines) + 1

	type cell struct {
		r     rune
		style lipgloss.Style
	}

	grid := make([][]cell, height)
	for i := range grid {
		grid[i] = make([]cell, width)
	}

	// draw shadow first (offset down/right)
	for y, runes := range lineRunes {
		for x, r := range runes {
			if r == ' ' {
				continue
			}
			if y+1 < height && x+1 < width {
				grid[y+1][x+1] = cell{r: r, style: logoShadowStyle}
			}
		}
	}

	// draw face on top
	for y, runes := range lineRunes {
		for x, r := range runes {
			if r == ' ' {
				continue
			}
			grid[y][x] = cell{r: r, style: logoFaceStyle}
		}
	}

	lines := make([]string, height)
	for y, row := range grid {
		var b strings.Builder
		for _, c := range row {
			if c.r == 0 {
				b.WriteRune(' ')
				continue
			}
			b.WriteString(c.style.Render(string(c.r)))
		}
		lines[y] = b.String()
	}
	return logoContainerStyle.Render(strings.Join(lines, "\n"))
}

func shortenList(items []string, limit int) string {
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s…", strings.Join(items[:limit], ", "))
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

func indentMultiline(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
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

func (m *model) markViewportDirty() {
	m.viewportDirty = true
}

func (m *model) refreshViewportIfDirty() {
	if m.viewportDirty {
		m.refreshViewport()
	}
}

func (m *model) refreshViewport() {
	m.viewportDirty = false
	prevYOffset := m.viewport.YOffset
	if m.paper == nil {
		m.viewportContent = ""
		m.viewport.SetContent("")
		m.suggestionLines = map[int]int{}
		m.sectionAnchors = map[string]int{}
		m.viewportLines = nil
		m.lineCount = 0
		return
	}
	view := m.buildDisplayContent()
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
			m.selectionActive = false
			forcedYOffset = line
			m.pendingFocusAnchor = ""
		}
	}

	content := view.content
	if m.searchQuery != "" {
		m.searchMatches = findMatches(content, m.searchQuery)
		if len(m.searchMatches) == 0 {
			m.searchMatchIdx = -1
		} else if m.searchMatchIdx < 0 || m.searchMatchIdx >= len(m.searchMatches) {
			m.searchMatchIdx = 0
		}
		content = highlightMatches(content, m.searchMatches, m.searchMatchIdx)
	} else {
		m.searchMatches = nil
		m.searchMatchIdx = -1
	}
	start, end, hasSelection := m.selectionRange()
	content = applyLineHighlights(content, m.cursorLine, start, end, hasSelection)
	m.viewport.SetContent(content)
	targetYOffset := prevYOffset
	if forcedYOffset >= 0 {
		targetYOffset = forcedYOffset
	}
	m.viewport.SetYOffset(m.clampYOffset(targetYOffset))
	if m.searchQuery != "" && len(m.searchMatches) > 0 && m.searchMatchIdx >= 0 {
		m.scrollToCurrentMatch()
	}
}

func (m *model) buildDisplayContent() displayView {
	cb := &contentBuilder{}
	anchors := map[string]int{}
	baseWrap := m.wrapWidth(0)
	bulletWrap := m.wrapWidth(4)
	indentWrap := m.wrapWidth(6)
	reasonWrap := m.wrapWidth(10)

	cb.WriteString(titleStyle.Render(m.paper.Title))
	cb.WriteRune('\n')
	if len(m.paper.Authors) > 0 {
		cb.WriteString(subtitleStyle.Render(strings.Join(m.paper.Authors, ", ")))
		cb.WriteRune('\n')
	}
	if len(m.paper.Subjects) > 0 {
		cb.WriteString(subjectStyle.Render("Subjects: " + strings.Join(m.paper.Subjects, ", ")))
		cb.WriteRune('\n')
	}

	cb.WriteRune('\n')
	anchors[anchorContributions] = cb.Line()
	cb.WriteString(sectionHeaderStyle.Render("Key Contributions"))
	cb.WriteRune('\n')
	for _, c := range m.paper.KeyContributions {
		cb.WriteString(" • ")
		cb.WriteString(wordwrap.String(c, bulletWrap))
		cb.WriteRune('\n')
	}

	cb.WriteRune('\n')
	anchors[anchorGuide] = cb.Line()
	cb.WriteString(sectionHeaderStyle.Render("Three-Pass Reading Plan"))
	cb.WriteRune('\n')
	for i, step := range m.guide {
		cb.WriteString(fmt.Sprintf("%d. %s\n", i+1, step.Title))
		cb.WriteString("   ")
		cb.WriteString(wordwrap.String(step.Description, bulletWrap))
		cb.WriteRune('\n')
	}

	cb.WriteRune('\n')
	anchors[anchorSummary] = cb.Line()
	cb.WriteString(sectionHeaderStyle.Render("LLM Summary (press a to refresh)"))
	cb.WriteRune('\n')
	switch {
	case m.config.LLM == nil:
		cb.WriteString(helperStyle.Render("Launch PaperScout with --llm-provider openai|ollama to enable summaries and Q&A."))
		cb.WriteRune('\n')
	case m.summaryLoading:
		cb.WriteString(helperStyle.Render(fmt.Sprintf("%s Summarizing the parsed PDF…", m.spinner.View())))
		cb.WriteRune('\n')
	case strings.TrimSpace(m.summary) != "":
		cb.WriteString(wordwrap.String(m.summary, baseWrap))
		cb.WriteRune('\n')
	default:
		cb.WriteString(helperStyle.Render("Summary unavailable. Press a to generate one using the parsed PDF."))
		cb.WriteRune('\n')
	}

	cb.WriteRune('\n')
	anchors[anchorSuggestions] = cb.Line()
	cb.WriteString(sectionHeaderStyle.Render("Suggested Notes (space to toggle, m to add manual)"))
	cb.WriteRune('\n')
	suggestionLines := make(map[int]int, len(m.suggestions))
	if m.suggestionLoading {
		cb.WriteString(helperStyle.Render(fmt.Sprintf("%s Generating LLM note ideas…", m.spinner.View())))
		cb.WriteRune('\n')
	}
	if len(m.suggestions) == 0 {
		if !m.suggestionLoading {
			cb.WriteString(helperStyle.Render("No automatic suggestions. Use m to add your own notes."))
			cb.WriteRune('\n')
		}
	} else {
		for idx, suggestion := range m.suggestions {
			lineNumber := cb.Line()
			cursor := " "
			if m.cursorLine == lineNumber {
				cursor = ">"
			}

			check := " "
			switch {
			case m.persisted[idx]:
				check = "✓"
			case m.selected[idx]:
				check = "x"
			}
			suggestionLines[idx] = lineNumber
			row := fmt.Sprintf(" %s [%s] %s", cursor, check, suggestion.Title)
			if m.persisted[idx] {
				row = persistedSuggestionStyle.Render(row)
			}
			cb.WriteString(row)
			cb.WriteRune('\n')

			body := indentMultiline(wordwrap.String(suggestion.Body, indentWrap), "     ")
			if m.persisted[idx] {
				body = persistedSuggestionStyle.Render(body)
			}
			cb.WriteString(body)
			cb.WriteRune('\n')

			if suggestion.Reason != "" {
				reason := indentMultiline(wordwrap.String(suggestion.Reason, reasonWrap), "     ⮑ ")
				cb.WriteString(helperStyle.Render(reason))
				cb.WriteRune('\n')
			}
			if m.persisted[idx] {
				cb.WriteString(helperStyle.Render("     ✓ Saved in knowledge base"))
				cb.WriteRune('\n')
			}
		}
	}

	if len(m.persistedNotes) > 0 {
		cb.WriteRune('\n')
		anchors[anchorSaved] = cb.Line()
		cb.WriteString(sectionHeaderStyle.Render("Saved Notes"))
		cb.WriteRune('\n')
		for i, note := range m.persistedNotes {
			cb.WriteString(fmt.Sprintf(" %d) %s (%s)\n", i+1, note.Title, note.Kind))
			cb.WriteString(indentMultiline(wordwrap.String(note.Body, indentWrap), "     "))
			cb.WriteRune('\n')
		}
	}

	if len(m.manualNotes) > 0 {
		cb.WriteRune('\n')
		anchors[anchorManual] = cb.Line()
		cb.WriteString(sectionHeaderStyle.Render("Manual Notes"))
		cb.WriteRune('\n')
		for i, note := range m.manualNotes {
			cb.WriteString(fmt.Sprintf(" %d) %s\n", i+1, note.Title))
			cb.WriteString(indentMultiline(wordwrap.String(note.Body, indentWrap), "     "))
			cb.WriteRune('\n')
		}
	}

	if m.config.LLM != nil {
		cb.WriteRune('\n')
		anchors[anchorQA] = cb.Line()
		cb.WriteString(sectionHeaderStyle.Render("Questions & Answers (press q to ask)"))
		cb.WriteRune('\n')
		if len(m.qaHistory) == 0 {
			cb.WriteString(helperStyle.Render("Use q to ask about the PDF text. Answers will cite the parsed content."))
			cb.WriteRune('\n')
		} else {
			for idx, exchange := range m.qaHistory {
				cb.WriteString(fmt.Sprintf(" %d) Q: %s\n", idx+1, wordwrap.String(exchange.Question, indentWrap)))
				switch {
				case exchange.Pending:
					cb.WriteString(helperStyle.Render(fmt.Sprintf("     %s Awaiting response…", m.spinner.View())))
				case exchange.Error != "":
					cb.WriteString(errorStyle.Render("     " + exchange.Error))
				default:
					cb.WriteString("     A: ")
					cb.WriteString(wordwrap.String(exchange.Answer, indentWrap))
				}
				cb.WriteRune('\n')
			}
		}
	}

	return displayView{
		content:         cb.String(),
		suggestionLines: suggestionLines,
		anchors:         anchors,
	}
}

func (m *model) ensureCursorVisible() {
	if m.lineCount == 0 {
		return
	}
	line := m.cursorLine
	if line < 0 {
		line = 0
	}
	if line < m.viewport.YOffset {
		m.viewport.SetYOffset(line)
		return
	}
	lowerBound := m.viewport.YOffset + m.viewport.Height - 1
	if line > lowerBound {
		target := line - m.viewport.Height + 1
		if target < 0 {
			target = 0
		}
		m.viewport.SetYOffset(target)
	}
}

func (m *model) moveCursor(delta int) {
	if m.lineCount == 0 {
		return
	}
	target := m.cursorLine + delta
	if target < 0 {
		target = 0
	}
	if target >= m.lineCount {
		target = m.lineCount - 1
	}
	if target == m.cursorLine {
		if m.mode != modeHighlight {
			m.selectionActive = false
		}
		return
	}
	m.cursorLine = target
	if m.mode != modeHighlight {
		m.selectionActive = false
	}
	m.markViewportDirty()
	m.refreshViewportIfDirty()
	m.ensureCursorVisible()
}

func (m *model) setCursorLine(line int) {
	if m.lineCount == 0 {
		return
	}
	if line < 0 {
		line = 0
	}
	if line >= m.lineCount {
		line = m.lineCount - 1
	}
	if line == m.cursorLine {
		return
	}
	m.cursorLine = line
	if m.mode != modeHighlight {
		m.selectionActive = false
	}
	m.markViewportDirty()
	m.refreshViewportIfDirty()
	m.ensureCursorVisible()
}

func (m *model) suggestionAtCursor() (int, bool) {
	if len(m.suggestions) == 0 {
		return 0, false
	}
	for idx := range m.suggestions {
		if line, ok := m.suggestionLines[idx]; ok && line == m.cursorLine {
			return idx, true
		}
	}
	return 0, false
}

func (m *model) toggleHighlightMode() {
	switch m.mode {
	case modeHighlight:
		m.mode = modeNormal
		m.selectionActive = false
		m.infoMessage = "Highlight mode disabled."
	default:
		if m.lineCount == 0 {
			return
		}
		m.mode = modeHighlight
		m.selectionAnchor = m.cursorLine
		m.selectionActive = true
		m.infoMessage = "Highlight mode enabled. Move to expand selection, press i to capture."
	}
	m.markViewportDirty()
	m.refreshViewportIfDirty()
}

func (m *model) enterInsertModeFromCursor() (tea.Model, tea.Cmd) {
	if m.paper == nil {
		m.infoMessage = "Load a paper before drafting notes."
		return m, nil
	}
	prefill := ""
	if m.mode == modeHighlight && m.selectionActive {
		prefill = strings.TrimSpace(m.selectedText())
		if prefill == "" {
			m.infoMessage = "Select non-empty text before inserting."
			return m, nil
		}
	}
	m.startNoteEntry(prefill)
	if prefill != "" {
		m.infoMessage = "Editing highlighted selection. Press Enter to store the note."
	} else {
		m.infoMessage = "Insert mode active. Press Enter to store the note."
	}
	return m, nil
}

func (m *model) startNoteEntry(prefill string) {
	m.stage = stageNoteEntry
	m.mode = modeInsert
	m.noteInput.Placeholder = "Write an atomic note and press Enter…"
	m.noteInput.SetValue(prefill)
	m.noteInput.Focus()
	m.selectionActive = false
}

func (m *model) jumpToRelativeSection(delta int) {
	anchors := m.availableSections()
	if len(anchors) == 0 {
		m.infoMessage = "No sections available yet."
		return
	}
	currentLine := m.cursorLine
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

func (m *model) selectionRange() (int, int, bool) {
	if !m.selectionActive || m.mode != modeHighlight || m.lineCount == 0 {
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

var ansiEscapeCodes = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(text string) string {
	return ansiEscapeCodes.ReplaceAllString(text, "")
}

func (m *model) scrollToTop() {
	m.viewport.SetYOffset(0)
	if m.lineCount > 0 {
		m.cursorLine = 0
		if m.mode != modeHighlight {
			m.selectionActive = false
		}
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
		if m.mode != modeHighlight {
			m.selectionActive = false
		}
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
		case anchorManual:
			m.infoMessage = "No manual notes yet."
		case anchorSaved:
			m.infoMessage = "No saved notes yet."
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
	if m.mode != modeHighlight {
		m.selectionActive = false
	}
	m.markViewportDirty()
	m.refreshViewportIfDirty()
	m.infoMessage = fmt.Sprintf("Jumped to %s.", sectionLabel(anchor))
}

func sectionLabel(anchor string) string {
	switch anchor {
	case anchorContributions:
		return "Key Contributions"
	case anchorGuide:
		return "Reading Plan"
	case anchorSuggestions:
		return "Suggestions"
	case anchorSaved:
		return "Saved Notes"
	case anchorManual:
		return "Manual Notes"
	default:
		return "section"
	}
}

func (m *model) applySearch(query string) {
	query = strings.TrimSpace(query)
	m.searchInput.Blur()
	m.searchQuery = query
	if query == "" {
		m.searchMatches = nil
		m.searchMatchIdx = -1
		m.searchInput.SetValue("")
	} else {
		m.searchMatchIdx = 0
	}
	m.markViewportDirty()
	m.refreshViewportIfDirty()
	if query == "" {
		m.infoMessage = "Cleared search filter."
	} else if len(m.searchMatches) == 0 {
		m.infoMessage = fmt.Sprintf("No matches for %q.", query)
	} else {
		m.infoMessage = fmt.Sprintf("Search ready for %q.", query)
	}
}

func (m *model) clearSearch() {
	m.searchQuery = ""
	m.searchMatches = nil
	m.searchMatchIdx = -1
	m.searchInput.SetValue("")
	m.searchInput.Blur()
	m.markViewportDirty()
}

func (m *model) advanceSearch(delta int) {
	if m.searchQuery == "" {
		m.infoMessage = "Start a search with / first."
		return
	}
	if len(m.searchMatches) == 0 {
		m.infoMessage = fmt.Sprintf("No matches for %q.", m.searchQuery)
		return
	}
	count := len(m.searchMatches)
	m.searchMatchIdx = (m.searchMatchIdx + delta) % count
	if m.searchMatchIdx < 0 {
		m.searchMatchIdx += count
	}
	m.infoMessage = fmt.Sprintf("Match %d/%d for %q.", m.searchMatchIdx+1, count, m.searchQuery)
	m.markViewportDirty()
	m.refreshViewportIfDirty()
}

func (m *model) scrollToCurrentMatch() {
	if len(m.searchMatches) == 0 || m.searchMatchIdx < 0 || m.searchMatchIdx >= len(m.searchMatches) {
		return
	}
	match := m.searchMatches[m.searchMatchIdx]
	line := lineNumberAtOffset(m.viewportContent, match.start)
	target := line - 1
	if target < 0 {
		target = 0
	}
	m.viewport.SetYOffset(target)
}

func (m *model) searchStatusLine() string {
	if m.searchQuery == "" {
		return ""
	}
	if len(m.searchMatches) == 0 {
		return fmt.Sprintf("Search %q — no matches", m.searchQuery)
	}
	return fmt.Sprintf("Search %q — match %d/%d", m.searchQuery, m.searchMatchIdx+1, len(m.searchMatches))
}

type displayView struct {
	content         string
	suggestionLines map[int]int
	anchors         map[string]int
}

type contentBuilder struct {
	builder strings.Builder
	lines   int
}

func (cb *contentBuilder) WriteString(s string) {
	cb.builder.WriteString(s)
	cb.lines += strings.Count(s, "\n")
}

func (cb *contentBuilder) WriteRune(r rune) {
	cb.builder.WriteRune(r)
	if r == '\n' {
		cb.lines++
	}
}

func (cb *contentBuilder) String() string {
	return cb.builder.String()
}

func (cb *contentBuilder) Line() int {
	return cb.lines
}

func (m *model) wrapWidth(padding int) int {
	width := m.viewport.Width
	if width <= 0 {
		width = 80
	}
	if padding < 0 {
		padding = 0
	}
	available := width - padding
	if available < 20 {
		available = 20
	}
	return available
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

func splitLinesPreserve(content string) []string {
	if content == "" {
		return []string{""}
	}
	return strings.Split(content, "\n")
}

type matchRange struct {
	start int
	end   int
}

func findMatches(content, query string) []matchRange {
	lowerContent := strings.ToLower(content)
	lowerQuery := strings.ToLower(query)
	if lowerQuery == "" {
		return nil
	}
	var matches []matchRange
	searchIdx := 0
	for {
		idx := strings.Index(lowerContent[searchIdx:], lowerQuery)
		if idx == -1 {
			break
		}
		start := searchIdx + idx
		end := start + len(lowerQuery)
		matches = append(matches, matchRange{start: start, end: end})
		searchIdx = end
		if searchIdx >= len(content) {
			break
		}
	}
	return matches
}

func highlightMatches(content string, matches []matchRange, current int) string {
	if len(matches) == 0 {
		return content
	}
	var b strings.Builder
	pos := 0
	for idx, match := range matches {
		if match.start > len(content) {
			break
		}
		if match.start > pos {
			b.WriteString(content[pos:match.start])
		}
		segmentEnd := match.end
		if segmentEnd > len(content) {
			segmentEnd = len(content)
		}
		segment := content[match.start:segmentEnd]
		if idx == current {
			b.WriteString(searchCurrentStyle.Render(segment))
		} else {
			b.WriteString(searchHighlightStyle.Render(segment))
		}
		pos = segmentEnd
	}
	if pos < len(content) {
		b.WriteString(content[pos:])
	}
	return b.String()
}

func applyLineHighlights(content string, cursor int, selectionStart, selectionEnd int, hasSelection bool) string {
	if content == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for idx, line := range lines {
		inSelection := hasSelection && idx >= selectionStart && idx <= selectionEnd
		switch {
		case idx == cursor && inSelection:
			lines[idx] = currentLineStyle.Render(line)
		case idx == cursor:
			lines[idx] = currentLineStyle.Render(line)
		case inSelection:
			lines[idx] = selectionLineStyle.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

func lineNumberAtOffset(content string, offset int) int {
	if offset <= 0 {
		return 0
	}
	if offset > len(content) {
		offset = len(content)
	}
	return strings.Count(content[:offset], "\n")
}

func fetchPaperCmd(url string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		paper, err := arxiv.FetchPaper(ctx, url)
		if err != nil {
			return paperResultMsg{err: err}
		}
		steps := guide.Build(guide.Metadata{Title: paper.Title, Authors: paper.Authors})
		suggestions := notes.SuggestCandidates(paper.Title, paper.Abstract, paper.KeyContributions)
		return paperResultMsg{
			paper:       paper,
			guide:       steps,
			suggestions: suggestions,
		}
	}
}

func saveNotesCmd(path string, entries []notes.Note) tea.Cmd {
	return func() tea.Msg {
		if err := notes.Save(path, entries); err != nil {
			return saveResultMsg{err: err}
		}
		return saveResultMsg{count: len(entries)}
	}
}

func summarizePaperCmd(client llm.Client, paper *arxiv.Paper) tea.Cmd {
	title := paper.Title
	content := paper.FullText
	paperID := paper.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		summary, err := client.Summarize(ctx, title, content)
		return summaryResultMsg{paperID: paperID, summary: summary, err: err}
	}
}

func suggestNotesCmd(client llm.Client, paper *arxiv.Paper) tea.Cmd {
	title := paper.Title
	abstract := paper.Abstract
	contributions := append([]string{}, paper.KeyContributions...)
	content := paper.FullText
	paperID := paper.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		suggestions, err := client.SuggestNotes(ctx, title, abstract, contributions, content)
		if err != nil {
			return suggestionResultMsg{paperID: paperID, err: err}
		}
		return suggestionResultMsg{paperID: paperID, suggestions: mapSuggestedNotes(suggestions), err: nil}
	}
}

func questionAnswerCmd(index int, client llm.Client, paper *arxiv.Paper, question string) tea.Cmd {
	title := paper.Title
	content := paper.FullText
	paperID := paper.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		answer, err := client.Answer(ctx, title, question, content)
		return questionResultMsg{paperID: paperID, index: index, answer: answer, err: err}
	}
}

func trimmedTitle(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 60 {
		return value
	}
	return fmt.Sprintf("%s…", strings.TrimSpace(value[:57]))
}

func candidateMatchesNotes(candidate notes.Candidate, saved []notes.Note) bool {
	for _, note := range saved {
		if note.Title == candidate.Title && note.Body == candidate.Body && note.Kind == candidate.Kind {
			return true
		}
	}
	return false
}

func mapSuggestedNotes(entries []llm.SuggestedNote) []notes.Candidate {
	results := make([]notes.Candidate, 0, len(entries))
	for _, suggestion := range entries {
		kind := suggestion.Kind
		if kind == "" {
			kind = "llm"
		}
		results = append(results, notes.Candidate{
			Title:  suggestion.Title,
			Body:   suggestion.Body,
			Kind:   kind,
			Reason: suggestion.Reason,
		})
	}
	return results
}

var (
	titleStyle           = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Underline(true)
	subtitleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("147"))
	sectionHeaderStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	subjectStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("110"))
	errorStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	helperStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	searchHighlightStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("190"))
	searchCurrentStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("229"))

	heroAccentColor        = lipgloss.Color("#ff8c00")
	heroEmberColor         = lipgloss.Color("#2b1400")
	heroTextColor          = lipgloss.Color("#fff4d0")
	heroSecondaryTextColor = lipgloss.Color("#ffb347")

	heroTitleStyle           = lipgloss.NewStyle().Bold(true).Foreground(heroAccentColor)
	heroBoxStyle             = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(heroAccentColor).Foreground(heroTextColor).Background(heroEmberColor).Padding(1, 2)
	heroSummaryStyle         = lipgloss.NewStyle().PaddingLeft(2)
	taglineStyle             = lipgloss.NewStyle().Foreground(heroSecondaryTextColor).Italic(true)
	statusBarStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("#0f0f0f")).Background(lipgloss.Color("#8ecae6")).Padding(0, 1)
	keyStyle                 = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#0f0f0f")).Background(lipgloss.Color("#ffd166")).Padding(0, 1)
	keyDescStyle             = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0def4"))
	legendBoxStyle           = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#56526e")).Padding(1, 2)
	helpBoxStyle             = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("#7f5af0")).Padding(1, 2)
	currentLineStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#0f0f0f")).Background(lipgloss.Color("#8ecae6"))
	selectionLineStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#0f0f0f")).Background(lipgloss.Color("#bde0fe"))
	persistedSuggestionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#a3be8c")).Italic(true)
	logoFaceStyle            = lipgloss.NewStyle().Bold(true).Foreground(heroTextColor).Background(heroEmberColor)
	logoShadowStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("#110600"))
	logoContainerStyle       = lipgloss.NewStyle().Padding(0, 1)
	logoArtLines             = []string{
		"██████╗    █████╗   ██████╗   ███████╗  ██████╗   ███████╗   ██████╗   ██████╗   ██╗   ██╗  ████████╗  ",
		"██╔══██╗  ██╔══██╗  ██╔══██╗  ██╔════╝  ██╔══██╗  ██╔════╝  ██╔════╝  ██╔═══██╗  ██║   ██║  ╚══██╔══╝  ",
		"██████╔╝  ███████║  ██████╔╝  █████╗    ██████╔╝  ███████╗  ██║       ██║   ██║  ██║   ██║     ██║     ",
		"██╔═══╝   ██╔══██║  ██╔═══╝   ██╔══╝    ██╔══██╗  ╚════██║  ██║       ██║   ██║  ██║   ██║     ██║     ",
		"██║       ██║  ██║  ██║       ███████╗  ██║  ██║  ███████║  ╚██████╗  ╚██████╔╝  ╚██████╔╝     ██║     ",
		"╚═╝       ╚═╝  ╚═╝  ╚═╝       ╚══════╝  ╚═╝  ╚═╝  ╚══════╝   ╚═════╝   ╚═════╝    ╚═════╝      ╚═╝     ",
	}
)
