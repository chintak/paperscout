package context

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/csheth/browse/internal/llm"
)

// Package bundles the deduplicated chunks plus per-section context strings.
type Package struct {
	Sections map[llm.BriefSectionKind]string
	Chunks   []Chunk
}

// Chunk captures a reusable slice of the PDF content that can be shared across sections.
type Chunk struct {
	ID    string
	Text  string
	Start int
	End   int
}

// Builder preprocesses PDF text into trimmed, deduplicated sections within fixed budgets.
type Builder struct {
	budgets map[llm.BriefSectionKind]int
}

var (
	paragraphSplit   = regexp.MustCompile(`\n{2,}`)
	whitespaceSanity = regexp.MustCompile(`\s+`)
)

// NewBuilder returns a Builder configured with the provided section budgets. Passing nil falls back
// to the default llm.BriefSectionLimit values.
func NewBuilder(budgets map[llm.BriefSectionKind]int) *Builder {
	result := map[llm.BriefSectionKind]int{}
	for _, kind := range []llm.BriefSectionKind{llm.BriefSummary, llm.BriefTechnical, llm.BriefDeepDive} {
		if budgets != nil && budgets[kind] > 0 {
			result[kind] = budgets[kind]
			continue
		}
		result[kind] = llm.BriefSectionLimit(kind)
	}
	return &Builder{budgets: result}
}

// Build trims the provided content, removes boilerplate/repeated paragraphs, and emits per-section
// context strings that stay under each budget.
func (b *Builder) Build(content string) Package {
	content = sanitizeDocument(content)
	paragraphs := paragraphSplit.Split(content, -1)
	seen := map[string]bool{}
	var chunks []Chunk
	cursor := 0
	for _, paragraph := range paragraphs {
		trimmed := strings.TrimSpace(paragraph)
		if trimmed == "" {
			continue
		}
		if isBoilerplate(trimmed) {
			continue
		}
		canonical := canonicalParagraph(trimmed)
		hash := hashChunk(canonical)
		if seen[hash] {
			continue
		}
		seen[hash] = true
		length := runeLen(trimmed)
		chunks = append(chunks, Chunk{
			ID:    hash,
			Text:  trimmed,
			Start: cursor,
			End:   cursor + length,
		})
		cursor += length
	}

	sections := map[llm.BriefSectionKind]string{}
	for kind, budget := range b.budgets {
		sectionChunks := chunks
		if kind == llm.BriefTechnical {
			sectionChunks = rankChunksForTechnical(chunks)
		}
		sections[kind] = clipChunks(sectionChunks, budget)
	}

	return Package{
		Sections: sections,
		Chunks:   chunks,
	}
}

func sanitizeDocument(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return content
}

func canonicalParagraph(text string) string {
	text = strings.TrimSpace(text)
	return whitespaceSanity.ReplaceAllString(text, " ")
}

func isBoilerplate(paragraph string) bool {
	lower := strings.ToLower(strings.TrimSpace(paragraph))
	if lower == "" {
		return true
	}
	switch {
	case lower == "abstract":
		return true
	case lower == "introduction":
		return true
	case lower == "keywords":
		return true
	case strings.HasPrefix(lower, "references"):
		return true
	case strings.HasPrefix(lower, "acknowledg"):
		return true
	case strings.HasPrefix(lower, "copyright"):
		return true
	case strings.Contains(lower, "doi"):
		return true
	case strings.Contains(lower, "arxiv:"):
		return true
	case strings.Contains(lower, "license"):
		return true
	}
	alpha := 0
	for _, r := range lower {
		if unicode.IsLetter(r) {
			alpha++
		}
	}
	if len(lower) <= 12 && !strings.Contains(lower, " ") {
		return true
	}
	if alpha*5 < len(lower) {
		return true
	}
	return false
}

func hashChunk(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}

func clipChunks(chunks []Chunk, budget int) string {
	if budget <= 0 {
		return ""
	}
	var builder strings.Builder
	remaining := budget
	for idx, chunk := range chunks {
		if remaining <= 0 {
			break
		}
		if idx > 0 && builder.Len() > 0 {
			if remaining <= 2 {
				break
			}
			builder.WriteString("\n\n")
			remaining -= 2
		}
		runes := []rune(chunk.Text)
		if len(runes) > remaining {
			builder.WriteString(string(runes[:remaining]))
			remaining = 0
			break
		}
		builder.WriteString(chunk.Text)
		remaining -= len(runes)
	}
	return builder.String()
}

func runeLen(text string) int {
	return len([]rune(text))
}

func rankChunksForTechnical(chunks []Chunk) []Chunk {
	if len(chunks) == 0 {
		return chunks
	}
	type scoredChunk struct {
		chunk Chunk
		score int
		index int
	}
	scored := make([]scoredChunk, 0, len(chunks))
	for idx, chunk := range chunks {
		scored = append(scored, scoredChunk{
			chunk: chunk,
			score: scoreTechnicalChunk(chunk.Text),
			index: idx,
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].index < scored[j].index
		}
		return scored[i].score > scored[j].score
	})
	ranked := make([]Chunk, 0, len(scored))
	for _, entry := range scored {
		ranked = append(ranked, entry.chunk)
	}
	return ranked
}

func scoreTechnicalChunk(text string) int {
	lower := strings.ToLower(text)
	keywords := []string{
		"method",
		"architecture",
		"model",
		"training",
		"dataset",
		"evaluation",
		"experiment",
		"loss",
		"optimization",
		"hyperparameter",
		"baseline",
		"ablation",
	}
	score := 0
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			score++
		}
	}
	return score
}
