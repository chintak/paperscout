package tui

import "time"

type stage int

const (
	stageInput stage = iota
	stageLoading
	stageDisplay
	stageSearch
	stageSaving
	stagePalette
)

const (
	anchorSummary   = "summary"
	anchorTechnical = "technical"
	anchorDeepDive  = "deep_dive"
)

var sectionSequence = []string{
	anchorSummary,
	anchorTechnical,
	anchorDeepDive,
}

const heroTagline = "Navigate arXiv findings with PaperScout."

const (
	minViewportWidth          = 40
	viewportHorizontalPadding = 4
	transcriptPreviewLimit    = 240
)

type interactionMode int

const (
	modeNormal interactionMode = iota
	modeInsert
	modeHighlight
)

type qaExchange struct {
	Question string
	Answer   string
	Error    string
	Pending  bool
	AskedAt  time.Time
}

type composerMode int

const (
	composerModeIdle composerMode = iota
	composerModeURL
	composerModeNote
	composerModeQuestion
)

const (
	composerURLPlaceholder      = "Paste an arXiv URL or identifier…"
	composerNotePlaceholder     = "Ctrl+Enter to save note, Esc to cancel."
	composerQuestionPlaceholder = "Ask about the loaded PDF…"
)
