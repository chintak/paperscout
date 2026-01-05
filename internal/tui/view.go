package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

func (m *model) View() string {
	switch m.stage {
	case stageInput:
		return m.viewInput()
	case stageLoading, stageDisplay:
		return m.viewDisplay()
	case stageSearch:
		return m.viewSearch()
	case stageSaving:
		return m.viewDisplay()
	case stagePalette:
		return m.viewPalette()
	default:
		return ""
	}
}

func (m *model) viewInput() string {
	m.refreshViewportIfDirty()
	body := m.renderStackedDisplay()
	return joinNonEmpty([]string{body, m.composerPanel(), m.footerView()})
}

func (m *model) viewDisplay() string {
	if m.paper == nil {
		return m.viewInput()
	}
	m.refreshTranscriptIfDirty()
	m.refreshViewportIfDirty()
	body := m.renderStackedDisplay()
	return joinNonEmpty([]string{body, m.composerPanel(), m.footerView()})
}

func (m *model) renderStackedDisplay() string {
	parts := []string{m.heroView()}
	parts = append(parts, m.viewport.View())
	if status := m.searchStatusLine(); status != "" {
		parts = append(parts, helperStyle.Render(status))
	}
	if m.errorMessage != "" {
		parts = append(parts, errorStyle.Render(m.errorMessage))
	}
	if m.infoMessage != "" {
		message := m.infoMessage
		if m.stage == stageLoading || m.stage == stageSaving {
			message = fmt.Sprintf("%s %s", m.spinner.View(), message)
		}
		parts = append(parts, helperStyle.Render(message))
	}
	if m.helpVisible {
		if legend := m.keyLegendView(); legend != "" {
			parts = append(parts, legend)
		}
		parts = append(parts, m.helpView())
	}
	return joinNonEmpty(parts)
}

func (m *model) composerPanel() string {
	return joinNonEmpty([]string{
		sectionHeaderStyle.Render("Composer"),
		m.composer.View(),
		helperStyle.Render(m.composerHelpText()),
	})
}

func (m *model) footerView() string {
	m.refreshTranscriptIfDirty()
	logBody := strings.TrimSpace(m.transcriptViewport.View())
	if logBody == "" {
		logBody = helperStyle.Render("Interactions will appear here once you load a paper.")
	}
	footer := []string{}
	if meter := m.sessionMeterView(); meter != "" {
		footer = append(footer, meter)
	}
	footer = append(footer, joinNonEmpty([]string{
		sectionHeaderStyle.Render("Session Log"),
		logBody,
	}))
	return joinNonEmpty(footer)
}

func (m *model) composerHelpText() string {
	return "Enter: ask • Ctrl+Enter: note • Shift+Enter: URL • Esc: clear"
}

func (m *model) viewSearch() string {
	var b strings.Builder
	b.WriteString(sectionHeaderStyle.Render("Search Current Session"))
	b.WriteRune('\n')
	b.WriteString(m.searchInput.View())
	b.WriteRune('\n')
	b.WriteString(helperStyle.Render("Press Enter to apply search, Esc to cancel."))
	return joinNonEmpty([]string{m.frameWithHero(b.String()), m.composerPanel(), m.footerView()})
}

func (m *model) viewPalette() string {
	var b strings.Builder
	b.WriteString(sectionHeaderStyle.Render("Command Palette"))
	b.WriteRune('\n')
	b.WriteString(m.paletteInput.View())
	b.WriteRune('\n')
	b.WriteString(helperStyle.Render("Enter to run, Esc to cancel."))
	b.WriteRune('\n')
	b.WriteRune('\n')
	if len(m.paletteMatches) == 0 {
		b.WriteString(helperStyle.Render("No commands match this filter."))
	} else {
		for idx, cmd := range m.paletteMatches {
			label := fmt.Sprintf("  %s  [%s]", cmd.title, cmd.shortcut)
			desc := helperStyle.Render("   " + cmd.description)
			if idx == m.paletteCursor {
				label = currentLineStyle.Render("▸ " + cmd.title + "  [" + cmd.shortcut + "]")
				desc = helperStyle.Render("   " + cmd.description)
			}
			b.WriteString(label)
			b.WriteRune('\n')
			b.WriteString(desc)
			b.WriteRune('\n')
		}
	}
	return joinNonEmpty([]string{m.frameWithHero(b.String()), m.composerPanel(), m.footerView()})
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
		fmt.Sprintf("Summary %d", len(m.brief.Summary)),
		fmt.Sprintf("Technical %d", len(m.brief.Technical)),
		fmt.Sprintf("DeepDive %d", len(m.brief.DeepDive)),
	}
	if m.config.LLM != nil {
		stats = append(stats, fmt.Sprintf("Q&A %d", len(m.qaHistory)))
		switch {
		case m.briefLoading || m.questionLoading:
			stats = append(stats, "LLM working…")
		case len(m.brief.Summary) > 0:
			stats = append(stats, "Brief ready")
		default:
			stats = append(stats, "Brief pending")
		}
	}
	if jobBadges := m.jobStatusBadges(); len(jobBadges) > 0 {
		stats = append(stats, jobBadges...)
	}
	return statusBarStyle.Render(strings.Join(stats, "  •  "))
}

type keyHint struct {
	Key         string
	Description string
}

func (m *model) keyLegendView() string {
	hints := []keyHint{
		{"↑/↓", "Scroll"},
		{"[/]", "Jump sections"},
		{"a", "Regenerate brief"},
		{"q", "Ask question"},
		{"/", "Search"},
		{"n/N", "Next/prev match"},
		{"g/G", "Top or bottom"},
		{"r", "Load new URL"},
		{"?", "Toggle cheatsheet"},
		{"Ctrl+K", "Command palette"},
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
		helperStyle.Render("• use [ and ] to jump between Summary, Technical, and Deep Dive sections; g / G flies to the top or bottom."),
		helperStyle.Render("• / opens search, n / N cycles matches, and Esc exits overlays."),
		helperStyle.Render("• press a to regenerate the LLM reading brief and q to ask questions once OpenAI or Ollama is configured."),
		helperStyle.Render("• press Ctrl+K to open the command palette, then type to filter actions and hit Enter to run them."),
		helperStyle.Render("• press r to paste a new URL, Ctrl+C to quit."),
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
	width += 1
	height := len(logoArtLines) + 1

	type cell struct {
		r     rune
		style lipgloss.Style
	}

	grid := make([][]cell, height)
	for i := range grid {
		grid[i] = make([]cell, width)
	}

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
