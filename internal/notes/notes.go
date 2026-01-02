package notes

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Note represents a stored knowledge entry in the lightweight zettelkasten.
type Note struct {
	PaperID    string    `json:"paperId"`
	PaperTitle string    `json:"paperTitle"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	Kind       string    `json:"kind"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Candidate is a suggested note derived automatically from a paper.
type Candidate struct {
	Title string
	Body  string
	Kind  string
	Reason string
}

// ToNote converts a candidate into a persistent note object.
func (c Candidate) ToNote(paperID, paperTitle string) Note {
	return Note{
		PaperID:    paperID,
		PaperTitle: paperTitle,
		Title:      c.Title,
		Body:       c.Body,
		Kind:       c.Kind,
		CreatedAt:  time.Now(),
	}
}

// SuggestCandidates builds a set of highlight-worthy note candidates using heuristics.
func SuggestCandidates(title, abstract string, contributions []string) []Candidate {
	suggestions := []Candidate{}
	abstract = strings.TrimSpace(abstract)
	if len(contributions) > 0 {
		max := 3
		if len(contributions) < max {
			max = len(contributions)
		}
		for i := 0; i < max; i++ {
			body := contributions[i]
			suggestions = append(suggestions, Candidate{
				Title: fmt.Sprintf("Contribution #%d", i+1),
				Body:  body,
				Kind:  "contribution",
				Reason: "Key insight extracted from the abstract.",
			})
		}
	}

	if abstract != "" {
		firstSentence := firstSentence(abstract)
		if firstSentence != "" {
			suggestions = append(suggestions, Candidate{
				Title:  "Problem Framing",
				Body:   firstSentence,
				Kind:   "problem",
				Reason: "Opening sentence usually states the problem statement.",
			})
		}

		method := pickSentenceByKeywords(abstract, []string{"approach", "method", "framework", "pipeline"})
		if method != "" {
			suggestions = append(suggestions, Candidate{
				Title:  "Method Snapshot",
				Body:   method,
				Kind:   "method",
				Reason: "Sentence referencing the proposed technique.",
			})
		}

		result := pickSentenceByKeywords(abstract, []string{"outperform", "state-of-the-art", "improve", "result", "achieve"})
		if result != "" {
			suggestions = append(suggestions, Candidate{
				Title:  "Result Highlight",
				Body:   result,
				Kind:   "result",
				Reason: "Sentence referencing evaluation outcomes.",
			})
		}
	}

	if len(suggestions) == 0 && title != "" {
		suggestions = append(suggestions, Candidate{
			Title:  "Paper Overview",
			Body:   fmt.Sprintf("%s — %s", title, abstract),
			Kind:   "overview",
			Reason: "Fallback overview when heuristics fail.",
		})
	}
	return suggestions
}

func firstSentence(text string) string {
	segments := strings.SplitAfter(text, ".")
	if len(segments) == 0 {
		return ""
	}
	return strings.TrimSpace(segments[0])
}

func pickSentenceByKeywords(text string, keywords []string) string {
	lowerText := strings.ToLower(text)
	for _, keyword := range keywords {
		idx := strings.Index(lowerText, keyword)
		if idx >= 0 {
			before := text[:idx]
			after := text[idx:]
			sentenceStart := strings.LastIndex(before, ".")
			start := 0
			if sentenceStart >= 0 {
				start = sentenceStart + 1
			}
			sentenceEnd := strings.Index(after, ".")
			if sentenceEnd >= 0 {
				return strings.TrimSpace(text[start : idx+sentenceEnd+1])
			}
			return strings.TrimSpace(text[start:])
		}
	}
	return ""
}

// Save appends notes to the knowledge base file, creating it if necessary.
func Save(path string, newNotes []Note) error {
	if len(newNotes) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	existing, err := Load(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	payload := append(existing, newNotes...)
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Load returns all stored notes from the knowledge base.
func Load(path string) ([]Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var notes []Note
	if err := json.Unmarshal(data, &notes); err != nil {
		return nil, err
	}
	return notes, nil
}
