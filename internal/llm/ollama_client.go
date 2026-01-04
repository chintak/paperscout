package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ollamaClient struct {
	host   string
	model  string
	client *http.Client
}

func (c *ollamaClient) Name() string {
	return fmt.Sprintf("Ollama (%s)", c.model)
}

func (c *ollamaClient) Summarize(ctx context.Context, title, content string) (string, error) {
	context := clipText(content, maxSummaryChars)
	if context == "" {
		return "", fmt.Errorf("paper text empty; cannot summarize")
	}
	prompt := buildSummaryPrompt(title, context)
	return c.generate(ctx, prompt)
}

func (c *ollamaClient) Answer(ctx context.Context, title, question, content string) (string, error) {
	if strings.TrimSpace(question) == "" {
		return "", fmt.Errorf("question cannot be empty")
	}
	context := extractQuestionContext(content, question, maxAnswerChars)
	if context == "" {
		return "", fmt.Errorf("paper text empty; cannot answer question")
	}
	prompt := buildAnswerPrompt(title, context, question)
	return c.generate(ctx, prompt)
}

func (c *ollamaClient) SuggestNotes(ctx context.Context, title, abstract string, contributions []string, content string) ([]SuggestedNote, error) {
	context := buildSuggestionContext(abstract, contributions, content, maxSuggestionChars)
	if context == "" {
		return nil, fmt.Errorf("paper text empty; cannot suggest notes")
	}
	prompt := buildSuggestionPrompt(title, context)
	raw, err := c.generate(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return parseSuggestedNotes(raw)
}

func (c *ollamaClient) ReadingBrief(ctx context.Context, title, content string) (ReadingBrief, error) {
	context := clipText(content, maxBriefChars)
	if context == "" {
		return ReadingBrief{}, fmt.Errorf("paper text empty; cannot build brief")
	}
	prompt := buildBriefPrompt(title, context)
	raw, err := c.generate(ctx, prompt)
	if err != nil {
		return ReadingBrief{}, err
	}
	return parseReadingBrief(raw)
}

func (c *ollamaClient) BriefSection(ctx context.Context, kind BriefSectionKind, title, content string) ([]string, error) {
	context := clipBriefSectionContext(kind, content)
	if context == "" {
		return nil, fmt.Errorf("paper text empty; cannot build %s section", kind)
	}
	prompt := buildBriefSectionPrompt(kind, title, context)
	raw, err := c.generate(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return parseBriefSection(raw)
}

func (c *ollamaClient) generate(ctx context.Context, prompt string) (string, error) {
	payload := map[string]any{
		"model":  c.model,
		"prompt": prompt,
		"stream": false,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+"/api/generate", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("ollama API error: %s (%s)", resp.Status, string(body))
	}

	var parsed struct {
		Response string `json:"response"`
		Done     bool   `json:"done"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.Response == "" {
		return "", fmt.Errorf("ollama returned an empty response")
	}
	return strings.TrimSpace(parsed.Response), nil
}
