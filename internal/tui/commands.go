package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/csheth/browse/internal/arxiv"
	"github.com/csheth/browse/internal/guide"
	"github.com/csheth/browse/internal/llm"
	"github.com/csheth/browse/internal/notes"
)

func fetchPaperJob(url string) jobRunner {
	return func(parent context.Context) (tea.Msg, error) {
		ctx, cancel := context.WithTimeout(parent, 35*time.Second)
		defer cancel()
		paper, err := arxiv.FetchPaper(ctx, url)
		if err != nil {
			return paperResultMsg{err: err}, err
		}
		steps := guide.Build(guide.Metadata{Title: paper.Title, Authors: paper.Authors})
		suggestions := notes.SuggestCandidates(paper.Title, paper.Abstract, paper.KeyContributions)
		return paperResultMsg{
			paper:       paper,
			guide:       steps,
			suggestions: suggestions,
		}, nil
	}
}

func saveNotesJob(path string, entries []notes.Note) jobRunner {
	toPersist := append([]notes.Note(nil), entries...)
	return func(parent context.Context) (tea.Msg, error) {
		if err := notes.Save(path, toPersist); err != nil {
			return saveResultMsg{err: err}, err
		}
		return saveResultMsg{count: len(toPersist)}, nil
	}
}

func briefSectionJob(kind llm.BriefSectionKind, contextText string, client llm.Client, paper *arxiv.Paper) jobRunner {
	title := paper.Title
	paperID := paper.ID
	return func(parent context.Context) (tea.Msg, error) {
		ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
		defer cancel()
		content := contextText
		if strings.TrimSpace(content) == "" {
			content = paper.FullText
		}
		bullets, err := client.BriefSection(ctx, kind, title, content)
		return briefSectionMsg{paperID: paperID, kind: kind, bullets: bullets, err: err}, err
	}
}

func suggestNotesJob(client llm.Client, paper *arxiv.Paper) jobRunner {
	title := paper.Title
	abstract := paper.Abstract
	contributions := append([]string{}, paper.KeyContributions...)
	content := paper.FullText
	paperID := paper.ID
	return func(parent context.Context) (tea.Msg, error) {
		ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
		defer cancel()
		suggestions, err := client.SuggestNotes(ctx, title, abstract, contributions, content)
		if err != nil {
			return suggestionResultMsg{paperID: paperID, err: err}, err
		}
		return suggestionResultMsg{paperID: paperID, suggestions: mapSuggestedNotes(suggestions), err: nil}, nil
	}
}

func questionAnswerJob(index int, client llm.Client, paper *arxiv.Paper, question string) jobRunner {
	title := paper.Title
	content := paper.FullText
	paperID := paper.ID
	return func(parent context.Context) (tea.Msg, error) {
		ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
		defer cancel()
		answer, err := client.Answer(ctx, title, question, content)
		return questionResultMsg{paperID: paperID, index: index, answer: answer, err: err}, err
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
