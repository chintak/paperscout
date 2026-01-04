package arxiv

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// Paper represents a subset of metadata returned by the arXiv API.
type Paper struct {
	ID               string
	Title            string
	Authors          []string
	Abstract         string
	Subjects         []string
	KeyContributions []string
	PDFURL           string
	FullText         string
}

var (
	idRegexp             = regexp.MustCompile(`(?i)arxiv\.org/(?:abs|pdf)/([0-9a-z.\-]+)(?:\.pdf)?`)
	extraneousWhitespace = regexp.MustCompile(`\s+`)
)

// FetchPaper fetches metadata for a given arXiv URL or identifier and derives key contributions.
func FetchPaper(ctx context.Context, input string) (*Paper, error) {
	id := extractIdentifier(input)
	if id == "" {
		return nil, fmt.Errorf("unable to extract arXiv identifier from %q", input)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://export.arxiv.org/api/query?id_list=%s", id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("arxiv API error: %s (%s)", resp.Status, string(body))
	}

	entry, err := decodeEntry(resp.Body)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, errors.New("paper not found")
	}

	authors := make([]string, 0, len(entry.Authors))
	for _, a := range entry.Authors {
		authors = append(authors, strings.TrimSpace(a.Name))
	}

	abstract := normalizeWhitespace(entry.Summary)
	contributions := extractKeyContributions(abstract)

	subjects := make([]string, 0, len(entry.Categories))
	for _, cat := range entry.Categories {
		subjects = append(subjects, strings.TrimSpace(cat.Term))
	}

	pdfURL := fmt.Sprintf("https://arxiv.org/pdf/%s.pdf", id)
	fullText, err := fetchPDFText(ctx, pdfURL)
	if err != nil {
		return nil, fmt.Errorf("failed to process paper PDF: %w", err)
	}

	return &Paper{
		ID:               id,
		Title:            normalizeWhitespace(entry.Title),
		Authors:          authors,
		Abstract:         abstract,
		Subjects:         subjects,
		KeyContributions: contributions,
		PDFURL:           pdfURL,
		FullText:         fullText,
	}, nil
}

func extractIdentifier(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	if len(input) > 4 && strings.EqualFold(input[len(input)-4:], ".pdf") {
		input = input[:len(input)-4]
	}
	if matches := idRegexp.FindStringSubmatch(input); len(matches) > 1 {
		return matches[1]
	}
	// Accept bare identifiers such as 2101.00001
	if len(input) >= len("arxiv:") && strings.EqualFold(input[:len("arxiv:")], "arxiv:") {
		input = input[len("arxiv:"):]
	}
	input = strings.TrimSpace(input)
	re := regexp.MustCompile(`^[0-9a-z.\-]+$`)
	if re.MatchString(input) {
		return input
	}
	return ""
}

type apiFeed struct {
	Entries []apiEntry `xml:"entry"`
}

type apiEntry struct {
	ID         string        `xml:"id"`
	Title      string        `xml:"title"`
	Summary    string        `xml:"summary"`
	Authors    []apiAuthor   `xml:"author"`
	Categories []apiCategory `xml:"category"`
}

type apiAuthor struct {
	Name string `xml:"name"`
}

type apiCategory struct {
	Term string `xml:"term,attr"`
}

func decodeEntry(reader io.Reader) (*apiEntry, error) {
	var feed apiFeed
	if err := xml.NewDecoder(reader).Decode(&feed); err != nil {
		return nil, fmt.Errorf("failed to decode arxiv response: %w", err)
	}
	if len(feed.Entries) == 0 {
		return nil, nil
	}
	return &feed.Entries[0], nil
}

func normalizeWhitespace(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return extraneousWhitespace.ReplaceAllString(strings.TrimSpace(s), " ")
}

func extractKeyContributions(abstract string) []string {
	if abstract == "" {
		return []string{"Abstract missing from arXiv payload."}
	}
	sentences := splitSentences(abstract)
	if len(sentences) == 0 {
		return []string{abstract}
	}

	type candidate struct {
		text  string
		score int
		idx   int
	}

	keywordWeights := map[string]int{
		"propose": 4, "introduce": 4, "present": 3, "demonstrate": 3, "show": 2,
		"evaluate": 2, "achieve": 3, "model": 2, "framework": 3, "method": 3,
		"state-of-the-art": 4, "outperform": 4, "improv": 2, "approach": 2,
		"architecture": 2, "pipeline": 2, "result": 1, "experiment": 1,
	}

	candidates := make([]candidate, 0, len(sentences))
	for idx, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		lower := strings.ToLower(sentence)
		score := 0
		for keyword, weight := range keywordWeights {
			if strings.Contains(lower, keyword) {
				score += weight
			}
		}
		if strings.Contains(lower, "we ") || strings.Contains(lower, "our ") {
			score++
		}
		if idx == 0 {
			score++
		}
		if len(sentence) < 40 {
			score--
		}
		candidates = append(candidates, candidate{text: sentence, score: score, idx: idx})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].idx < candidates[j].idx
		}
		return candidates[i].score > candidates[j].score
	})

	results := []string{}
	for _, cand := range candidates {
		if len(results) == 4 {
			break
		}
		if cand.score <= 0 && len(results) >= 2 {
			continue
		}
		if !containsString(results, cand.text) {
			results = append(results, cand.text)
		}
	}

	if len(results) < 3 {
		for _, sentence := range sentences {
			sentence = strings.TrimSpace(sentence)
			if sentence == "" {
				continue
			}
			if !containsString(results, sentence) {
				results = append(results, sentence)
			}
			if len(results) == 4 {
				break
			}
		}
	}

	if len(results) == 0 {
		return []string{abstract}
	}
	return results
}

func containsString(haystack []string, needle string) bool {
	for _, existing := range haystack {
		if existing == needle {
			return true
		}
	}
	return false
}

func splitSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var sentences []string
	start := 0
	for idx, r := range text {
		if r == '.' || r == '!' || r == '?' {
			end := idx + utf8.RuneLen(r)
			segment := strings.TrimSpace(text[start:end])
			if segment != "" {
				sentences = append(sentences, segment)
			}
			start = end
			for start < len(text) {
				nextRune, size := utf8.DecodeRuneInString(text[start:])
				if unicode.IsSpace(nextRune) {
					start += size
					continue
				}
				break
			}
		}
	}
	if start < len(text) {
		if segment := strings.TrimSpace(text[start:]); segment != "" {
			sentences = append(sentences, segment)
		}
	}
	return sentences
}

func fetchPDFText(ctx context.Context, pdfURL string) (string, error) {
	cache, err := newPDFCache(nil)
	if err != nil {
		return "", err
	}
	path, err := cache.Fetch(ctx, pdfURL)
	if err != nil {
		return "", err
	}

	file, reader, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open pdf: %w", err)
	}
	defer file.Close()

	content, err := reader.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("failed to extract pdf text: %w", err)
	}

	var builder strings.Builder
	if _, err := io.Copy(&builder, content); err != nil {
		return "", err
	}

	fullText := extraneousWhitespace.ReplaceAllString(builder.String(), " ")
	return strings.TrimSpace(fullText), nil
}
