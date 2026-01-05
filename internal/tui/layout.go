package tui

import (
	"fmt"
	"strings"

	"github.com/muesli/reflow/wordwrap"

	"github.com/csheth/browse/internal/llm"
)

type pageLayout struct {
	windowWidth      int
	windowHeight     int
	viewportWidth    int
	viewportHeight   int
	transcriptHeight int
	composerHeight   int
}

func newPageLayout() pageLayout {
	return pageLayout{
		viewportWidth:    80,
		viewportHeight:   20,
		transcriptHeight: 10,
		composerHeight:   4,
	}
}

func (l *pageLayout) Update(width, height int) {
	l.windowWidth = width
	l.windowHeight = height
	innerWidth := width - viewportHorizontalPadding
	if innerWidth < minViewportWidth {
		innerWidth = minViewportWidth
	}
	l.viewportWidth = innerWidth
	l.composerHeight = 1
	const chrome = 8
	const footerStatusHeight = 1
	usable := height - chrome - l.composerHeight
	if usable < 12 {
		usable = 12
	}
	l.transcriptHeight = usable / 3
	if l.transcriptHeight < 6 {
		l.transcriptHeight = 6
	}
	contentHeight := usable - footerStatusHeight
	if contentHeight < 6 {
		contentHeight = 6
	}
	l.viewportHeight = contentHeight
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

func (m *model) writeConversationStream(cb *contentBuilder) {
	cb.WriteString(sectionHeaderStyle.Render("Conversation Stream"))
	cb.WriteRune('\n')
	if len(m.transcriptEntries) == 0 {
		cb.WriteString(helperStyle.Render("Interactions will appear here once you load a paper."))
		cb.WriteRune('\n')
		return
	}
	wrap := m.wrapWidth(4)
	for idx, entry := range m.transcriptEntries {
		label := transcriptLabel(entry.Kind)
		if label != "" {
			cb.WriteString(helperStyle.Render(label))
			cb.WriteRune('\n')
		}
		body := wordwrap.String(entry.Content, wrap)
		cb.WriteString(indentMultiline(body, "  "))
		if idx < len(m.transcriptEntries)-1 {
			cb.WriteRune('\n')
			cb.WriteRune('\n')
		} else {
			cb.WriteRune('\n')
		}
	}
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

func (m *model) buildDisplayContent() displayView {
	cb := &contentBuilder{}
	anchors := map[string]int{}
	bulletWrap := m.wrapWidth(4)
	suggestionLines := map[int]int{}
	m.writeConversationStream(cb)

	renderBullets := func(items []string) {
		for _, item := range items {
			cb.WriteString(" • ")
			cb.WriteString(wordwrap.String(item, bulletWrap))
			cb.WriteRune('\n')
		}
	}

	renderSection := func(kind llm.BriefSectionKind, anchor, title string, items []string, state briefSectionState, emptyMsg string) {
		if cb.Line() > 0 {
			cb.WriteRune('\n')
		}
		anchors[anchor] = cb.Line()
		cb.WriteString(sectionHeaderStyle.Render(title))
		cb.WriteRune('\n')
		fallback := m.fallbackForSection(kind)
		renderFallback := func() {
			if len(fallback) == 0 {
				return
			}
			cb.WriteString(helperStyle.Render(fallbackNotice(kind)))
			cb.WriteRune('\n')
			renderBullets(fallback)
		}
		switch {
		case len(items) > 0:
			renderBullets(items)
		case m.config.LLM == nil:
			cb.WriteString(helperStyle.Render("Connect OpenAI or Ollama (flags or env) to unlock this section."))
			cb.WriteRune('\n')
			renderFallback()
		case state.Loading:
			cb.WriteString(helperStyle.Render(fmt.Sprintf("%s Generating…", m.spinner.View())))
			cb.WriteRune('\n')
			renderFallback()
		case state.Error != "":
			cb.WriteString(errorStyle.Render(state.Error))
			cb.WriteRune('\n')
			renderFallback()
		case len(fallback) > 0:
			renderFallback()
		default:
			cb.WriteString(helperStyle.Render(emptyMsg))
			cb.WriteRune('\n')
		}
	}

	renderSection(
		llm.BriefSummary,
		anchorSummary,
		"Summary Pass",
		m.brief.Summary,
		m.sectionState(llm.BriefSummary),
		"Press a to build the 3-bullet executive summary.",
	)
	renderSection(
		llm.BriefTechnical,
		anchorTechnical,
		"Technical Details",
		m.brief.Technical,
		m.sectionState(llm.BriefTechnical),
		"Press a to populate assumptions, data, models, and evaluation specifics.",
	)
	renderSection(
		llm.BriefDeepDive,
		anchorDeepDive,
		"Deep Dive References",
		m.brief.DeepDive,
		m.sectionState(llm.BriefDeepDive),
		"Press a to surface influential citations for further study.",
	)

	return displayView{
		content:         cb.String(),
		suggestionLines: suggestionLines,
		anchors:         anchors,
	}
}

func (m *model) buildIdleContent() displayView {
	cb := &contentBuilder{}
	cb.WriteString(sectionHeaderStyle.Render("Paste an arXiv URL in the Composer"))
	cb.WriteRune('\n')
	cb.WriteString(helperStyle.Render("Type an arXiv URL or identifier below and press Shift+Enter to fetch metadata."))
	cb.WriteRune('\n')
	cb.WriteString(helperStyle.Render("Enter asks a question; Ctrl+Enter saves a note. Esc clears the composer."))
	cb.WriteRune('\n')
	cb.WriteRune('\n')
	m.writeConversationStream(cb)

	return displayView{
		content:         cb.String(),
		suggestionLines: map[int]int{},
		anchors:         map[string]int{},
	}
}

func indentMultiline(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func shortenList(items []string, limit int) string {
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s…", strings.Join(items[:limit], ", "))
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

func previewText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

func sectionLabel(anchor string) string {
	switch anchor {
	case anchorSummary:
		return "Summary Pass"
	case anchorTechnical:
		return "Technical Details"
	case anchorDeepDive:
		return "Deep Dive References"
	default:
		return "section"
	}
}

func transcriptLabel(kind string) string {
	switch kind {
	case "question":
		return "You"
	case "note":
		return "You (note)"
	case "answer":
		return "Scout"
	case "brief":
		return "Scout (brief)"
	case "paper", "fetch", "save":
		return "System"
	case "error":
		return "Error"
	default:
		return kind
	}
}
