package tui

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/muesli/reflow/wordwrap"
)

var (
	markdownLinkPattern           = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	markdownBoldPattern           = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	markdownBoldUnderscorePattern = regexp.MustCompile(`__([^_]+)__`)
	markdownItalicPattern         = regexp.MustCompile(`\*([^*]+)\*`)
	markdownItalicUnderscore      = regexp.MustCompile(`_([^_]+)_`)
	markdownBulletPattern         = regexp.MustCompile(`^(\s*)[-*+]\s+`)
	markdownOrderedListPattern    = regexp.MustCompile(`^(\s*)(\d+[.)])\s+`)
	markdownHeadingPattern        = regexp.MustCompile(`^\s*#{1,6}\s+`)
	markdownCodeFencePattern      = regexp.MustCompile("^\\s*```")
	markdownInlineCodePattern     = regexp.MustCompile("`([^`]+)`")
	markdownQuotePattern          = regexp.MustCompile(`^\s*>\s+`)
	markdownStrikethroughPattern  = regexp.MustCompile(`~~([^~]+)~~`)
	latexInlineDoublePattern      = regexp.MustCompile(`\$\$([^$]+)\$\$`)
	latexInlineSinglePattern      = regexp.MustCompile(`\$([^$]+)\$`)
	plainURLPattern               = regexp.MustCompile(`https?://[^\s\)\]\}]+`)
)

type pageLayout struct {
	windowWidth      int
	windowHeight     int
	viewportWidth    int
	viewportHeight   int
	transcriptHeight int
	composerHeight   int
	heroHeight       int
}

func newPageLayout() pageLayout {
	return pageLayout{
		viewportWidth:    80,
		viewportHeight:   20,
		transcriptHeight: 10,
		composerHeight:   1,
		heroHeight:       0,
	}
}

func (l *pageLayout) Update(width, height int) {
	l.windowWidth = width
	l.windowHeight = height
	if l.composerHeight < 1 {
		l.composerHeight = 1
	}
	l.reflow()
}

func (l *pageLayout) SetComposerHeight(height int) {
	if height < 1 {
		height = 1
	}
	l.composerHeight = height
	l.reflow()
}

func (l *pageLayout) SetHeroHeight(height int) {
	if height < 0 {
		height = 0
	}
	if l.heroHeight == height {
		return
	}
	l.heroHeight = height
	l.reflow()
}

func (l *pageLayout) reflow() {
	innerWidth := l.windowWidth - viewportHorizontalPadding
	if innerWidth < minViewportWidth {
		innerWidth = minViewportWidth
	}
	l.viewportWidth = innerWidth
	const chrome = 8
	const footerStatusHeight = 1
	usable := l.windowHeight - chrome - l.heroHeight
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
	body            string
	suggestionLines map[int]int
	anchors         map[string]int
}

type contentBuilder struct {
	builder strings.Builder
	lines   int
}

type markdownLineKind int

const (
	markdownLinePlain markdownLineKind = iota
	markdownLineHeading
	markdownLineBullet
	markdownLineOrdered
	markdownLineTable
	markdownLineQuote
	markdownLineCode
)

type markdownLine struct {
	text   string
	kind   markdownLineKind
	prefix string
}

func (m *model) writeConversationStream(cb *contentBuilder) {
	if len(m.transcriptEntries) == 0 {
		return
	}
	wrap := m.wrapWidth(4)
	for idx, entry := range m.transcriptEntries {
		label := transcriptLabel(entry.Kind)
		if label != "" {
			cb.WriteString(helperStyle.Render(label))
			cb.WriteRune('\n')
		}
		body := formatConversationEntry(entry.Content, wrap)
		cb.WriteString(indentMultiline(body, "  "))
		if idx < len(m.transcriptEntries)-1 {
			cb.WriteRune('\n')
			cb.WriteRune('\n')
		} else {
			cb.WriteRune('\n')
		}
	}
}

func (m *model) writeComposerBlock(cb *contentBuilder) {
	cb.WriteRune('\n')
	cb.WriteString(helperStyle.Render("Command"))
	cb.WriteRune('\n')
	cb.WriteString(indentMultiline(m.composer.View(), "  "))
	cb.WriteRune('\n')
	cb.WriteString(m.footerTickerView())
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
	m.writeConversationStream(cb)
	m.writeComposerBlock(cb)

	return displayView{
		body:            cb.String(),
		suggestionLines: map[int]int{},
		anchors:         map[string]int{},
	}
}

func (m *model) buildIdleContent() displayView {
	cb := &contentBuilder{}
	cb.WriteString(sectionHeaderStyle.Render("Paste an arXiv URL in the Composer"))
	cb.WriteRune('\n')
	cb.WriteString(helperStyle.Render("Type an arXiv URL or identifier below and press Alt+Enter to fetch metadata."))
	cb.WriteRune('\n')
	cb.WriteString(helperStyle.Render("Enter loads the paper; Ctrl+Enter saves a note; Esc clears the composer."))
	if len(m.transcriptEntries) > 0 {
		cb.WriteRune('\n')
		cb.WriteRune('\n')
		m.writeConversationStream(cb)
	}

	m.writeComposerBlock(cb)

	return displayView{
		body:            cb.String(),
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

func formatConversationEntry(content string, wrap int) string {
	if content == "" {
		return ""
	}
	lines := splitLinesPreserve(content)
	rendered := make([]string, 0, len(lines))
	inCodeBlock := false
	lastNonBlank := markdownLinePlain
	lastRenderedBlank := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if markdownCodeFencePattern.MatchString(trimmed) {
			inCodeBlock = !inCodeBlock
			continue
		}
		if trimmed == "" {
			if inCodeBlock {
				rendered = append(rendered, "")
				lastRenderedBlank = true
				continue
			}
			nextKind := nextNonBlankKind(lines, i+1)
			if lastRenderedBlank {
				continue
			}
			if isListOrTableKind(lastNonBlank) && isListOrTableKind(nextKind) {
				continue
			}
			if isListOrTableKind(nextKind) {
				continue
			}
			rendered = append(rendered, "")
			lastRenderedBlank = true
			continue
		}
		lastRenderedBlank = false
		if inCodeBlock {
			rendered = append(rendered, markdownCodeStyle.Render(line))
			continue
		}
		if isMarkdownTableLine(trimmed) {
			block := []string{line}
			for j := i + 1; j < len(lines); j++ {
				next := strings.TrimSpace(lines[j])
				if !isMarkdownTableLine(next) {
					break
				}
				block = append(block, lines[j])
				i = j
			}
			rendered = append(rendered, renderMarkdownTable(block)...)
			continue
		}
		formatted := formatMarkdownLineWithKind(line)
		lastNonBlank = formatted.kind
		content := formatted.text
		if wrap > 0 && formatted.kind != markdownLineTable {
			if formatted.prefix != "" {
				content = wrapWithPrefix(content, formatted.prefix, wrap)
			} else {
				content = wordwrap.String(content, wrap)
			}
		}
		if formatted.kind != markdownLineTable {
			content = renderInlineLinks(content)
		}
		rendered = append(rendered, styleMarkdownLine(content, formatted.kind))
	}
	return strings.Join(rendered, "\n")
}

func formatMarkdownLineWithKind(line string) markdownLine {
	if line == "" {
		return markdownLine{text: "", kind: markdownLinePlain}
	}
	rawIndent := leadingIndent(line)
	normalizedIndent := normalizeIndent(rawIndent)
	trimmed := strings.TrimSpace(line)
	kind := markdownLinePlain
	prefix := ""
	switch {
	case markdownHeadingPattern.MatchString(trimmed):
		kind = markdownLineHeading
	case markdownBulletPattern.MatchString(line):
		kind = markdownLineBullet
	case markdownOrderedListPattern.MatchString(line):
		kind = markdownLineOrdered
	case isMarkdownTableLine(trimmed):
		kind = markdownLineTable
	case markdownQuotePattern.MatchString(trimmed):
		kind = markdownLineQuote
	}
	line = markdownHeadingPattern.ReplaceAllString(line, "")
	line = markdownQuotePattern.ReplaceAllString(line, "")
	if matches := markdownBulletPattern.FindStringSubmatch(line); matches != nil {
		rest := strings.TrimSpace(line[len(matches[0]):])
		prefix = normalizedIndent + "• "
		line = prefix + rest
	} else if matches := markdownOrderedListPattern.FindStringSubmatch(line); matches != nil {
		rest := strings.TrimSpace(line[len(matches[0]):])
		prefix = normalizedIndent + matches[2] + " "
		line = prefix + rest
	} else if normalizedIndent != "" && kind == markdownLinePlain {
		rest := strings.TrimLeft(line, " \t")
		prefix = normalizedIndent
		line = prefix + strings.TrimSpace(rest)
	}
	line = stylizeInlineElements(line)
	return markdownLine{text: line, kind: kind, prefix: prefix}
}

func nextNonBlankKind(lines []string, start int) markdownLineKind {
	for i := start; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		return markdownKindForRaw(lines[i])
	}
	return markdownLinePlain
}

func markdownKindForRaw(line string) markdownLineKind {
	trimmed := strings.TrimSpace(line)
	switch {
	case markdownHeadingPattern.MatchString(trimmed):
		return markdownLineHeading
	case markdownBulletPattern.MatchString(line):
		return markdownLineBullet
	case markdownOrderedListPattern.MatchString(line):
		return markdownLineOrdered
	case isMarkdownTableLine(trimmed):
		return markdownLineTable
	case markdownQuotePattern.MatchString(trimmed):
		return markdownLineQuote
	default:
		return markdownLinePlain
	}
}

func isListOrTableKind(kind markdownLineKind) bool {
	return kind == markdownLineBullet || kind == markdownLineOrdered || kind == markdownLineTable
}

func leadingIndent(line string) string {
	for i, r := range line {
		if r != ' ' && r != '\t' {
			return line[:i]
		}
	}
	return line
}

func normalizeIndent(indent string) string {
	if indent == "" {
		return ""
	}
	normalized := strings.ReplaceAll(indent, "\t", "  ")
	return normalized
}

func isMarkdownTableLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.Count(trimmed, "|") >= 2
}

func renderMarkdownTable(lines []string) []string {
	rows := make([][]string, 0, len(lines))
	colCount := 0
	for _, line := range lines {
		cells := splitTableLine(line)
		if isMarkdownTableDivider(cells) {
			continue
		}
		cleaned := make([]string, 0, len(cells))
		for _, cell := range cells {
			cleaned = append(cleaned, formatMarkdownInline(cell))
		}
		if len(cleaned) > colCount {
			colCount = len(cleaned)
		}
		rows = append(rows, cleaned)
	}
	if len(rows) == 0 || colCount == 0 {
		return nil
	}
	widths := make([]int, colCount)
	for _, row := range rows {
		for idx := 0; idx < colCount; idx++ {
			cell := ""
			if idx < len(row) {
				cell = row[idx]
			}
			size := utf8.RuneCountInString(stripANSI(cell))
			if size > widths[idx] {
				widths[idx] = size
			}
		}
	}
	rendered := make([]string, 0, len(rows))
	for rowIdx, row := range rows {
		parts := make([]string, colCount)
		for idx := 0; idx < colCount; idx++ {
			cell := ""
			if idx < len(row) {
				cell = row[idx]
			}
			pad := widths[idx] - utf8.RuneCountInString(stripANSI(cell))
			if pad < 0 {
				pad = 0
			}
			parts[idx] = " " + cell + strings.Repeat(" ", pad+1)
		}
		line := "|" + strings.Join(parts, "|") + "|"
		if rowIdx == 0 && len(rows) > 1 {
			rendered = append(rendered, markdownTableHeaderStyle.Render(line))
		} else {
			rendered = append(rendered, markdownTableStyle.Render(line))
		}
	}
	return rendered
}

func splitTableLine(line string) []string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "|") {
		trimmed = strings.TrimPrefix(trimmed, "|")
	}
	if strings.HasSuffix(trimmed, "|") {
		trimmed = strings.TrimSuffix(trimmed, "|")
	}
	raw := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(raw))
	for _, cell := range raw {
		cells = append(cells, strings.TrimSpace(cell))
	}
	return cells
}

func isMarkdownTableDivider(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		trimmed := strings.TrimSpace(cell)
		if trimmed == "" {
			return false
		}
		for _, r := range trimmed {
			switch r {
			case '-', ':':
				continue
			default:
				return false
			}
		}
	}
	return true
}

func formatMarkdownInline(text string) string {
	line := strings.TrimSpace(text)
	line = markdownInlineCodePattern.ReplaceAllString(line, "$1")
	line = markdownStrikethroughPattern.ReplaceAllString(line, "$1")
	line = markdownBoldPattern.ReplaceAllString(line, "$1")
	line = markdownBoldUnderscorePattern.ReplaceAllString(line, "$1")
	line = markdownItalicPattern.ReplaceAllString(line, "$1")
	line = markdownItalicUnderscore.ReplaceAllString(line, "$1")
	line = strings.ReplaceAll(line, "**", "")
	line = strings.ReplaceAll(line, "__", "")
	line = strings.ReplaceAll(line, "~~", "")
	line = strings.ReplaceAll(line, "`", "")
	line = renderInlineLinks(line)
	return line
}

func styleMarkdownLine(line string, kind markdownLineKind) string {
	switch kind {
	case markdownLineHeading:
		return markdownHeadingStyle.Render(line)
	case markdownLineBullet:
		return styleBulletLine(line)
	case markdownLineOrdered:
		return styleOrderedLine(line)
	case markdownLineTable:
		return markdownTableStyle.Render(line)
	case markdownLineQuote:
		return markdownQuoteStyle.Render(line)
	default:
		return line
	}
}

func stylizeInlineElements(line string) string {
	line = markdownInlineCodePattern.ReplaceAllString(line, markdownInlineCodeStyle.Render("$1"))
	line = latexInlineDoublePattern.ReplaceAllString(line, latexStyle.Render("$1"))
	line = latexInlineSinglePattern.ReplaceAllString(line, latexStyle.Render("$1"))
	line = markdownBoldPattern.ReplaceAllString(line, markdownBoldStyle.Render("$1"))
	line = markdownBoldUnderscorePattern.ReplaceAllString(line, markdownBoldStyle.Render("$1"))
	line = markdownItalicPattern.ReplaceAllString(line, markdownItalicStyle.Render("$1"))
	line = markdownItalicUnderscore.ReplaceAllString(line, markdownItalicStyle.Render("$1"))
	line = markdownStrikethroughPattern.ReplaceAllString(line, markdownStrikethroughStyle.Render("$1"))
	return line
}

func renderInlineLinks(line string) string {
	line = renderMarkdownLinks(line)
	return renderPlainURLs(line)
}

func renderMarkdownLinks(line string) string {
	return markdownLinkPattern.ReplaceAllStringFunc(line, func(match string) string {
		matches := markdownLinkPattern.FindStringSubmatch(match)
		if len(matches) != 3 {
			return match
		}
		label := strings.TrimSpace(matches[1])
		url := strings.TrimSpace(matches[2])
		if url == "" {
			return label
		}
		if label == "" {
			label = url
		}
		if label == url {
			return url
		}
		return fmt.Sprintf("%s (%s)", label, url)
	})
}

func renderPlainURLs(line string) string {
	return plainURLPattern.ReplaceAllStringFunc(line, func(found string) string {
		url, suffix := splitURLSuffix(found)
		if url == "" {
			return found
		}
		return renderClickableURL(url) + suffix
	})
}

func splitURLSuffix(raw string) (string, string) {
	suffix := ""
	for len(raw) > 0 {
		r, size := utf8.DecodeLastRuneInString(raw)
		if strings.ContainsRune(".,;:!?", r) {
			suffix = string(r) + suffix
			raw = raw[:len(raw)-size]
			continue
		}
		break
	}
	return raw, suffix
}

func renderClickableURL(url string) string {
	const (
		hyperlinkPrefix = "\x1b]8;;"
		hyperlinkTerm   = "\x1b\\"
	)
	styled := linkStyle.Render(url)
	return fmt.Sprintf("%s%s%s%s%s%s", hyperlinkPrefix, url, hyperlinkTerm, styled, hyperlinkPrefix, hyperlinkTerm)
}

func styleBulletLine(line string) string {
	idx := strings.Index(line, "•")
	if idx == -1 {
		return markdownBulletStyle.Render(line)
	}
	return line[:idx] + markdownBulletStyle.Render("•") + line[idx+len("•"):]
}

func styleOrderedLine(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	prefixLen := len(line) - len(trimmed)
	for i, r := range trimmed {
		if r == ' ' {
			number := trimmed[:i]
			rest := trimmed[i:]
			return line[:prefixLen] + markdownBulletStyle.Render(number) + rest
		}
	}
	return line
}

func wrapWithPrefix(text, prefix string, width int) string {
	rest := strings.TrimPrefix(text, prefix)
	prefixWidth := utf8.RuneCountInString(prefix)
	available := width - prefixWidth
	if available < 10 {
		available = 10
	}
	wrapped := wordwrap.String(rest, available)
	lines := splitLinesPreserve(wrapped)
	indent := strings.Repeat(" ", prefixWidth)
	for i, line := range lines {
		if i == 0 {
			lines[i] = prefix + line
		} else {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
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
	case "answer_draft":
		return "Scout (draft)"
	case briefTranscriptKindSummary, briefTranscriptKindTechnical, briefTranscriptKindDeepDive:
		if label, ok := briefSectionLabelForTranscriptKind(kind); ok {
			return fmt.Sprintf("Scout (%s)", label)
		}
		return "Scout (brief)"
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
