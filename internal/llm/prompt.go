package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var whitespaceRe = regexp.MustCompile(`\s+`)

func clipText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func buildSummaryPrompt(title, context string) string {
	if title == "" {
		title = "the paper"
	}
	return "You are an expert research assistant. " +
		"Write a concise 5-bullet summary covering the core problem, method, results, and limitations.\n" +
		"Each bullet should be <=20 words.\n\n" +
		"Paper title: " + title + "\n\n" +
		"Content:\n" + context
}

func buildAnswerPrompt(title, context, question string) string {
	builder := strings.Builder{}
	builder.WriteString("You are an expert research assistant. Use ONLY the provided context to answer the question.\n")
	builder.WriteString("If the answer isn't present, say you couldn't find it.\n\n")
	if title != "" {
		builder.WriteString("Paper title: " + title + "\n\n")
	}
	builder.WriteString("Context:\n")
	builder.WriteString(context)
	builder.WriteString("\n\nQuestion: " + question + "\nAnswer:")
	return builder.String()
}

func buildSuggestionContext(abstract string, contributions []string, content string, limit int) string {
	var b strings.Builder
	abstract = strings.TrimSpace(abstract)
	if abstract != "" {
		b.WriteString("Abstract:\n")
		b.WriteString(abstract)
		b.WriteString("\n\n")
	}
	if len(contributions) > 0 {
		b.WriteString("Key Contributions:\n")
		for _, contribution := range contributions {
			b.WriteString("- ")
			b.WriteString(strings.TrimSpace(contribution))
			b.WriteRune('\n')
		}
		b.WriteRune('\n')
	}
	snippet := clipText(content, limit)
	snippet = strings.TrimSpace(snippet)
	if snippet != "" {
		b.WriteString("Paper Excerpt:\n")
		b.WriteString(snippet)
	}
	return strings.TrimSpace(b.String())
}

func buildSuggestionPrompt(title, context string) string {
	if title == "" {
		title = "the paper"
	}
	return fmt.Sprintf(
		"You are mentoring a researcher applying S. Keshav's \"How to Read a Paper\" methodology.\n"+
			"Craft 4-6 distinct, atomic zettelkasten notes that cover the problem framing, key ideas, methods, results, risks, surprises, or follow-up questions.\n"+
			"Each note must include: title (<=10 words), body (2-3 sentences grounded in the text), reason (why this note matters per the reading passes), and kind (problem|method|result|risk|open-question|follow-up).\n"+
			"Return ONLY JSON that matches: {\"notes\":[{\"title\":\"\",\"body\":\"\",\"reason\":\"\",\"kind\":\"\"}]} and avoid duplicate ideas.\n\n"+
			"Paper title: %s\n\nContext:\n%s", title, context,
	)
}

func parseSuggestedNotes(raw string) ([]SuggestedNote, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty suggestion response")
	}

	tryArrays := []string{raw}
	if start := strings.Index(raw, "["); start >= 0 {
		if end := strings.LastIndex(raw, "]"); end > start {
			tryArrays = append(tryArrays, raw[start:end+1])
		}
	}

	for _, candidate := range tryArrays {
		var arr []SuggestedNote
		if err := json.Unmarshal([]byte(candidate), &arr); err == nil && len(arr) > 0 {
			return sanitizeSuggestedNotes(arr), nil
		}
		var wrapper struct {
			Notes []SuggestedNote `json:"notes"`
		}
		if err := json.Unmarshal([]byte(candidate), &wrapper); err == nil && len(wrapper.Notes) > 0 {
			return sanitizeSuggestedNotes(wrapper.Notes), nil
		}
	}
	return nil, fmt.Errorf("unable to parse suggestion payload")
}

func sanitizeSuggestedNotes(notes []SuggestedNote) []SuggestedNote {
	result := make([]SuggestedNote, 0, len(notes))
	for _, note := range notes {
		n := SuggestedNote{
			Title:  strings.TrimSpace(note.Title),
			Body:   strings.TrimSpace(note.Body),
			Reason: strings.TrimSpace(note.Reason),
			Kind:   strings.TrimSpace(note.Kind),
		}
		if n.Title == "" || n.Body == "" {
			continue
		}
		result = append(result, n)
	}
	return result
}

func extractQuestionContext(content, question string, limit int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	keywords := questionKeywords(question)
	if len(keywords) == 0 {
		return clipText(content, limit)
	}

	sentences := roughSentenceSplit(content)
	var matches []string
	totalLen := 0

	for _, sentence := range sentences {
		lower := strings.ToLower(sentence)
		for keyword := range keywords {
			if strings.Contains(lower, keyword) {
				matches = append(matches, sentence)
				totalLen += len(sentence)
				break
			}
		}
		if totalLen >= limit {
			break
		}
	}

	if len(matches) == 0 {
		return clipText(content, limit)
	}

	snippet := strings.Join(matches, " ")
	return clipText(snippet, limit)
}

func questionKeywords(question string) map[string]struct{} {
	question = strings.ToLower(question)
	question = whitespaceRe.ReplaceAllString(question, " ")
	tokens := strings.FieldsFunc(question, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	stopwords := map[string]struct{}{
		"what": {}, "why": {}, "how": {}, "is": {}, "the": {}, "a": {}, "an": {}, "of": {},
		"does": {}, "do": {}, "paper": {}, "method": {}, "result": {}, "in": {}, "on": {},
		"for": {}, "are": {}, "be": {}, "use": {}, "using": {},
	}
	keywords := map[string]struct{}{}
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if len(token) < 3 {
			continue
		}
		if _, skip := stopwords[token]; skip {
			continue
		}
		keywords[token] = struct{}{}
	}
	return keywords
}

func roughSentenceSplit(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var sentences []string
	var current strings.Builder
	for _, r := range text {
		current.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			sentence := strings.TrimSpace(current.String())
			if sentence != "" {
				sentences = append(sentences, sentence)
			}
			current.Reset()
		}
	}
	if tail := strings.TrimSpace(current.String()); tail != "" {
		sentences = append(sentences, tail)
	}
	return sentences
}
