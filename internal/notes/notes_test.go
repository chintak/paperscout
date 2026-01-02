package notes

import (
	"path/filepath"
	"testing"
)

func TestSuggestCandidatesUsesContributionsAndHeuristics(t *testing.T) {
	t.Parallel()

	abstract := "We introduce a framework. The approach improves performance. Experiments achieve strong results."
	contribs := []string{
		"Contribution A",
		"Contribution B",
		"Contribution C",
	}

	got := SuggestCandidates("Great Paper", abstract, contribs)
	if len(got) < 4 {
		t.Fatalf("expected at least four suggestions, got %d", len(got))
	}

	if got[0].Title != "Contribution #1" || got[0].Body != "Contribution A" {
		t.Fatalf("expected first suggestion to be contribution, got %+v", got[0])
	}

	var hasProblem, hasMethod, hasResult bool
	for _, candidate := range got {
		switch candidate.Title {
		case "Problem Framing":
			hasProblem = true
		case "Method Snapshot":
			hasMethod = true
		case "Result Highlight":
			hasResult = true
		}
	}

	if !hasProblem || !hasMethod || !hasResult {
		t.Fatalf("expected heuristic suggestions, got %#v", got)
	}
}

func TestSuggestCandidatesFallsBackToOverview(t *testing.T) {
	t.Parallel()

	got := SuggestCandidates("Untitled", "", nil)
	if len(got) != 1 || got[0].Title != "Paper Overview" {
		t.Fatalf("expected fallback overview suggestion, got %#v", got)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "zettel.json")

	payload := []Note{
		{
			PaperID:    "1234",
			PaperTitle: "Sample",
			Title:      "Contribution #1",
			Body:       "We propose something.",
			Kind:       "contribution",
		},
	}

	if err := Save(path, payload); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(got) != 1 || got[0].Title != payload[0].Title || got[0].Body != payload[0].Body {
		t.Fatalf("unexpected notes payload: %#v", got)
	}
}
