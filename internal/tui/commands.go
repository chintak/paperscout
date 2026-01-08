package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/csheth/browse/internal/arxiv"
	"github.com/csheth/browse/internal/guide"
	"github.com/csheth/browse/internal/llm"
	"github.com/csheth/browse/internal/notes"
)

const fetchTimeout = 3 * time.Minute

func fetchPaperJob(url string) jobRunner {
	return func(parent context.Context) (tea.Msg, error) {
		ctx, cancel := context.WithTimeout(parent, fetchTimeout)
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

func ensureConversationSnapshotJob(path string, paper *arxiv.Paper) jobRunner {
	paperID := paper.ID
	title := paper.Title
	return func(parent context.Context) (tea.Msg, error) {
		if path == "" || paperID == "" {
			return nil, nil
		}
		snapshots, err := notes.LoadConversationSnapshots(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			snapshots = nil
		}
		for _, snapshot := range snapshots {
			if snapshot.PaperID == paperID {
				return nil, nil
			}
		}
		newSnapshot := notes.ConversationSnapshot{
			PaperID:    paperID,
			PaperTitle: title,
			CapturedAt: time.Now(),
		}
		if err := notes.SaveConversationSnapshots(path, []notes.ConversationSnapshot{newSnapshot}); err != nil {
			return nil, err
		}
		return nil, nil
	}
}

func appendConversationSnapshotJob(path string, paper *arxiv.Paper, update notes.SnapshotUpdate) jobRunner {
	paperID := paper.ID
	title := paper.Title
	messages := append([]notes.ConversationMessage(nil), update.Messages...)
	notesUpdate := append([]notes.SnapshotNote(nil), update.Notes...)
	var briefCopy *notes.BriefSnapshot
	if update.Brief != nil {
		copy := *update.Brief
		copy.Summary = append([]string(nil), update.Brief.Summary...)
		copy.Technical = append([]string(nil), update.Brief.Technical...)
		copy.DeepDive = append([]string(nil), update.Brief.DeepDive...)
		briefCopy = &copy
	}
	metadata := append([]notes.BriefSectionMetadata(nil), update.SectionMetadata...)
	updateCopy := notes.SnapshotUpdate{
		Messages:        messages,
		Notes:           notesUpdate,
		Brief:           briefCopy,
		SectionMetadata: metadata,
	}
	return func(parent context.Context) (tea.Msg, error) {
		if path == "" || paperID == "" {
			return nil, nil
		}
		if len(updateCopy.Messages) == 0 && len(updateCopy.Notes) == 0 && updateCopy.Brief == nil && len(updateCopy.SectionMetadata) == 0 {
			return nil, nil
		}
		if err := notes.AppendConversationSnapshot(path, paperID, title, updateCopy); err != nil {
			return nil, err
		}
		return nil, nil
	}
}

func briefSectionJob(kind llm.BriefSectionKind, contextText string, client llm.Client, paper *arxiv.Paper, streamCtx context.Context) (jobRunner, <-chan llm.BriefSectionDelta) {
	title := paper.Title
	paperID := paper.ID
	updates := make(chan llm.BriefSectionDelta, 4)
	runner := func(parent context.Context) (tea.Msg, error) {
		ctx, cancel := context.WithTimeout(streamCtx, 2*time.Minute)
		defer cancel()
		content := contextText
		if strings.TrimSpace(content) == "" {
			content = paper.FullText
		}
		var final []string
		defer close(updates)
		err := client.StreamBriefSection(ctx, kind, title, content, func(delta llm.BriefSectionDelta) error {
			if len(delta.Bullets) > 0 {
				final = append([]string(nil), delta.Bullets...)
			}
			select {
			case updates <- delta:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		if err != nil {
			return briefSectionMsg{paperID: paperID, kind: kind, err: err}, err
		}
		return briefSectionMsg{paperID: paperID, kind: kind, bullets: final}, nil
	}
	return runner, updates
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
