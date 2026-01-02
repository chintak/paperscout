package arxiv

import (
	"strings"
	"testing"
)

func TestExtractIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"abs url", "https://arxiv.org/abs/2101.00001", "2101.00001"},
		{"pdf url", "https://arxiv.org/pdf/2205.12345.pdf", "2205.12345"},
		{"prefixed", "arXiv:2101.00001", "2101.00001"},
		{"bare", "2308.01234v2", "2308.01234v2"},
		{"bare pdf suffix", "2308.01234v2.pdf", "2308.01234v2"},
		{"invalid", "https://example.com/foo", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := extractIdentifier(tt.in); got != tt.want {
				t.Fatalf("extractIdentifier(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExtractKeyContributionsPrefersKeywordSentences(t *testing.T) {
	t.Parallel()

	abstract := strings.Join([]string{
		"We investigate transformers for speech recognition.",
		"We propose a dual-stream architecture that models local and global context.",
		"The method introduces a curriculum learning schedule to stabilize convergence.",
		"We demonstrate state-of-the-art performance on LibriSpeech.",
		"Ablations show each component matters.",
	}, " ")

	got := extractKeyContributions(abstract)
	if len(got) == 0 {
		t.Fatalf("expected at least one contribution, got none")
	}
	if len(got) > 4 {
		t.Fatalf("expected at most four contributions, got %d", len(got))
	}

	want := []string{
		"We propose a dual-stream architecture that models local and global context.",
		"The method introduces a curriculum learning schedule to stabilize convergence.",
		"We demonstrate state-of-the-art performance on LibriSpeech.",
	}

	for _, sentence := range want {
		if !contains(got, sentence) {
			t.Fatalf("expected contributions to contain sentence %q; got %#v", sentence, got)
		}
	}
}

func TestExtractKeyContributionsFallsBackToAbstract(t *testing.T) {
	t.Parallel()

	abstract := "Random filler that lacks keywords but still needs a fallback."
	got := extractKeyContributions(abstract)
	if len(got) == 0 || got[0] != abstract {
		t.Fatalf("expected fallback abstract, got %#v", got)
	}
}

func contains(slice []string, needle string) bool {
	for _, item := range slice {
		if strings.TrimSpace(item) == strings.TrimSpace(needle) {
			return true
		}
	}
	return false
}
